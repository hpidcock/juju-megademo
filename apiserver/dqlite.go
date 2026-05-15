// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apiserver

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/juju/errors"

	"github.com/juju/juju/apiserver/authentication"
	"github.com/juju/juju/apiserver/httpcontext"
	"github.com/juju/juju/apiserver/websocket"
	"github.com/juju/juju/core/database"
	"github.com/juju/juju/core/model"
	internallogger "github.com/juju/juju/internal/logger"
)

const (
	defaultRowLimit = 100
	maxRowLimit     = 1000
	queryTimeout    = 10 * time.Second
	serverVersion   = "v1"
)

var dqliteLogger = internallogger.GetLogger("juju.apiserver.dqlite")

// DBGetter returns a transaction runner for a dqlite database namespace.
type DBGetter func(namespace string) (database.TxnRunner, error)

// ClusterIntrospector describes the cluster topology of a dqlite node.
type ClusterIntrospector interface {
	DescribeCluster(context.Context) ([]clusterNodeInfo, error)
}

type clusterNodeInfo struct {
	ID      uint64
	Address string
	Role    string
}

type dqliteRequest struct {
	Version   string `json:"version"`
	RequestID string `json:"request_id"`
	Type      string `json:"type"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	SQL       string `json:"sql,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type dqliteResponse struct {
	Version   string      `json:"version"`
	RequestID string      `json:"request_id"`
	Type      string      `json:"type"`
	Error     string      `json:"error,omitempty"`
	Result    interface{} `json:"result,omitempty"`
}

type dqliteDatabase struct {
	Name      string `json:"name"`
	UUID      string `json:"uuid"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
}

type dqliteObject struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type dqliteNode struct {
	ID      uint64 `json:"id"`
	Address string `json:"address"`
	Role    string `json:"role"`
}

type dqliteDDLResult struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
}

type dqliteQueryResult struct {
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	RowCount  int        `json:"row_count"`
	Truncated bool       `json:"truncated"`
}

type dqliteHandler struct {
	ctxt                httpContext
	authenticator       authentication.HTTPAuthenticator
	authorizer          authentication.Authorizer
	dbGetter            DBGetter
	controllerModelUUID model.UUID
}

func newDqliteHandler(
	ctxt httpContext,
	authenticator authentication.HTTPAuthenticator,
	authorizer authentication.Authorizer,
	dbGetter DBGetter,
	controllerModelUUID model.UUID,
) *dqliteHandler {
	return &dqliteHandler{
		ctxt:                ctxt,
		authenticator:       authenticator,
		authorizer:          authorizer,
		dbGetter:            dbGetter,
		controllerModelUUID: controllerModelUUID,
	}
}

func (h *dqliteHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// The /dqlite endpoint is controller-scoped (no model UUID in the
	// URL). Set the controller model UUID in the request context so
	// the authenticator can find it.
	ctx := httpcontext.SetContextModelUUID(req.Context(), h.controllerModelUUID)
	req = req.WithContext(ctx)

	handler := func(conn *websocket.Conn) {
		defer conn.Close()

		authInfo, err := h.authenticator.Authenticate(req)
		if err != nil {
			_ = conn.SendInitialErrorV0(errors.Annotate(err, "authentication failed"))
			return
		}
		if err := h.authorizer.Authorize(req.Context(), authInfo); err != nil {
			_ = conn.SendInitialErrorV0(errors.Annotate(err, "authorization failed"))
			return
		}

		if err := conn.SendInitialErrorV0(nil); err != nil {
			dqliteLogger.Errorf(req.Context(), "dqlite handler failed to send initial ok: %v", err)
			return
		}

		if err := h.versionHandshake(conn); err != nil {
			_ = conn.WriteJSON(dqliteResponse{
				Version:   serverVersion,
				Error:     err.Error(),
			})
			return
		}

		for {
			var msg dqliteRequest
			if err := conn.ReadJSON(&msg); err != nil {
				dqliteLogger.Tracef(req.Context(), "dqlite handler stopped (client disconnected): %v", err)
				return
			}
			if msg.Version != serverVersion {
				resp := dqliteResponse{
					Version:   serverVersion,
					RequestID: msg.RequestID,
					Type:      msg.Type,
					Error:     fmt.Sprintf("unsupported version %q, expected %q", msg.Version, serverVersion),
				}
				conn.WriteJSON(resp)
				return
			}

			ctx, cancel := context.WithTimeout(req.Context(), queryTimeout)
			resp := h.dispatch(ctx, msg)
			cancel()

			if err := conn.WriteJSON(resp); err != nil {
				dqliteLogger.Errorf(req.Context(), "dqlite handler write error: %v", err)
				return
			}
		}
	}
	websocket.Serve(w, req, handler)
}

func (h *dqliteHandler) versionHandshake(conn *websocket.Conn) error {
	var msg dqliteRequest
	if err := conn.ReadJSON(&msg); err != nil {
		return errors.Annotate(err, "failed to read handshake")
	}
	if msg.Version != serverVersion {
		return errors.Errorf("unsupported version %q, expected %q", msg.Version, serverVersion)
	}
	resp := dqliteResponse{
		Version: serverVersion,
	}
	return conn.WriteJSON(resp)
}

func (h *dqliteHandler) dispatch(ctx context.Context, req dqliteRequest) dqliteResponse {
	resp := dqliteResponse{
		Version:   serverVersion,
		RequestID: req.RequestID,
		Type:      req.Type,
	}
	var result interface{}
	var err error

	switch req.Type {
	case "databases":
		result, err = h.dispatchDatabases(ctx)
	case "objects":
		result, err = h.dispatchObjects(ctx, req.Namespace, req.Kind)
	case "ddl":
		result, err = h.dispatchDDL(ctx, req.Namespace, req.Name)
	case "query":
		result, err = h.dispatchQuery(ctx, req.Namespace, req.SQL, req.Limit)
	case "cluster":
		result, err = h.dispatchCluster(ctx)
	default:
		err = errors.Errorf("unknown request type %q", req.Type)
	}

	if err != nil {
		resp.Error = err.Error()
		return resp
	}
	resp.Result = result
	return resp
}

func (h *dqliteHandler) dispatchDatabases(ctx context.Context) (interface{}, error) {
	db, err := h.dbGetter("controller")
	if err != nil {
		return nil, errors.Annotate(err, "getting controller database")
	}

	var databases []dqliteDatabase

	if err := db.StdTxn(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT uuid, name FROM model")
		if err != nil {
			return errors.Annotate(err, "querying models")
		}
		defer rows.Close()

		for rows.Next() {
			var uuid, name string
			if err := rows.Scan(&uuid, &name); err != nil {
				return errors.Annotate(err, "scanning model row")
			}
			databases = append(databases, dqliteDatabase{
				Name:      name,
				UUID:      uuid,
				Namespace: uuid,
				Type:      "model",
			})
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}

	controllerEntry := dqliteDatabase{
		Name:      "controller",
		UUID:      "",
		Namespace: "controller",
		Type:      "controller",
	}
	databases = append([]dqliteDatabase{controllerEntry}, databases...)
	return databases, nil
}

func (h *dqliteHandler) dispatchObjects(ctx context.Context, namespace, kind string) (interface{}, error) {
	if kind == "" {
		kind = "table"
	}
	if namespace == "" {
		return nil, errors.New("namespace is required")
	}

	db, err := h.dbGetter(namespace)
	if err != nil {
		return nil, errors.Annotatef(err, "getting database for namespace %q", namespace)
	}

	var objects []dqliteObject
	if err := db.StdTxn(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = ?", kind)
		if err != nil {
			return errors.Annotate(err, "querying objects")
		}
		defer rows.Close()

		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return errors.Annotate(err, "scanning object row")
			}
			objects = append(objects, dqliteObject{
				Name: name,
				Kind: kind,
			})
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	return objects, nil
}

func (h *dqliteHandler) dispatchDDL(ctx context.Context, namespace, name string) (interface{}, error) {
	if namespace == "" {
		return nil, errors.New("namespace is required")
	}
	if name == "" {
		return nil, errors.New("name is required")
	}

	db, err := h.dbGetter(namespace)
	if err != nil {
		return nil, errors.Annotatef(err, "getting database for namespace %q", namespace)
	}

	var result dqliteDDLResult
	if err := db.StdTxn(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT sql FROM sqlite_master WHERE name = ?", name)
		if err != nil {
			return errors.Annotate(err, "querying DDL")
		}
		defer rows.Close()

		if !rows.Next() {
			return errors.NotFoundf("object %q", name)
		}
		var sqlStr sql.NullString
		if err := rows.Scan(&sqlStr); err != nil {
			return errors.Annotate(err, "scanning DDL row")
		}
		result = dqliteDDLResult{
			Name: name,
		}
		if sqlStr.Valid {
			result.SQL = sqlStr.String
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (h *dqliteHandler) dispatchQuery(ctx context.Context, namespace, sqlStr string, limit int) (interface{}, error) {
	if namespace == "" {
		return nil, errors.New("namespace is required")
	}
	if sqlStr == "" {
		return nil, errors.New("sql is required")
	}

	if err := isReadOnlySQL(sqlStr); err != nil {
		return nil, err
	}

	db, err := h.dbGetter(namespace)
	if err != nil {
		return nil, errors.Annotatef(err, "getting database for namespace %q", namespace)
	}

	effectiveLimit := limit
	if effectiveLimit <= 0 {
		effectiveLimit = defaultRowLimit
	}
	if effectiveLimit > maxRowLimit {
		effectiveLimit = maxRowLimit
	}
	querySQL := sqlStr
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sqlStr)), "PRAGMA") &&
		!strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sqlStr)), "EXPLAIN") {
		querySQL = applyRowLimit(sqlStr, effectiveLimit+1)
	}

	var result dqliteQueryResult
	if err := db.StdTxn(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
			return errors.Annotate(err, "setting read-only mode")
		}
		defer tx.ExecContext(ctx, "PRAGMA query_only = OFF") //nolint:errcheck
		rows, err := tx.QueryContext(ctx, querySQL)
		if err != nil {
			return errors.Annotate(err, "executing query")
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			return errors.Annotate(err, "getting columns")
		}
		result.Columns = columns

		colCount := len(columns)
		rowCount := 0
		for rows.Next() {
			vals := make([]interface{}, colCount)
			valPtrs := make([]interface{}, colCount)
			for i := range vals {
				valPtrs[i] = &vals[i]
			}
			if err := rows.Scan(valPtrs...); err != nil {
				return errors.Annotate(err, "scanning query row")
			}
			rowCount++
			if rowCount > effectiveLimit {
				result.Truncated = true
				continue
			}
			strRow := make([]string, colCount)
			for i, v := range vals {
				strRow[i] = formatValue(v)
			}
			result.Rows = append(result.Rows, strRow)
		}
		result.RowCount = len(result.Rows)
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (h *dqliteHandler) dispatchCluster(ctx context.Context) (interface{}, error) {
	db, err := h.dbGetter("controller")
	if err != nil {
		return nil, errors.Annotate(err, "getting controller database")
	}

	if ci, ok := db.(ClusterIntrospector); ok {
		nodes, err := ci.DescribeCluster(ctx)
		if err != nil {
			return nil, errors.Annotate(err, "describing cluster")
		}
		result := make([]dqliteNode, len(nodes))
		for i, n := range nodes {
			result[i] = dqliteNode{
				ID:      n.ID,
				Address: n.Address,
				Role:    n.Role,
			}
		}
		return result, nil
	}
	return []dqliteNode{}, nil
}

var readOnlyWhitelist = map[string]bool{
	"SELECT":                true,
	"WITH":                  true,
	"EXPLAIN":               true,
	"PRAGMA_TABLE_INFO":     true,
	"PRAGMA_FOREIGN_KEY_LIST": true,
	"PRAGMA_INDEX_LIST":     true,
	"PRAGMA_INDEX_INFO":     true,
	"PRAGMA_TABLE_XINFO":    true,
}

func isReadOnlySQL(sqlStr string) error {
	trimmed := strings.TrimSpace(sqlStr)
	if trimmed == "" {
		return errors.New("empty SQL statement")
	}

	keyword := extractFirstKeyword(trimmed)
	if !readOnlyWhitelist[keyword] {
		return errors.Errorf("read-only query violation: %q is not allowed", keyword)
	}

	if isMultiStatement(trimmed) {
		return errors.New("multi-statement queries are not allowed")
	}
	return nil
}

func extractFirstKeyword(s string) string {
	s = strings.TrimSpace(s)
	var kw strings.Builder
	inString := false
	inDQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			if inString && i+1 < len(s) && s[i+1] == '\'' {
				i++
				continue
			}
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == '"' {
			inDQuote = !inDQuote
			continue
		}
		if inDQuote {
			continue
		}
		if c == ' ' || c == ';' || c == '\t' || c == '\n' || c == '\r' || c == '(' {
			break
		}
		kw.WriteByte(c)
	}
	upperKW := strings.ToUpper(kw.String())
	if upperKW == "PRAGMA" {
		rest := s[kw.Len():]
		if len(rest) > 0 && rest[0] == '(' {
			rest = rest[1:]
		}
		rest = strings.TrimLeft(rest, " \t")
		var pragmaName strings.Builder
		for i := 0; i < len(rest); i++ {
			c := rest[i]
			if c == ' ' || c == '(' || c == '\t' || c == '\n' || c == '\r' {
				break
			}
			pragmaName.WriteByte(c)
		}
		if pragmaName.Len() > 0 {
			return upperKW + "_" + strings.ToUpper(pragmaName.String())
		}
	}
	return upperKW
}

func isMultiStatement(s string) bool {
	nonEmptyParts := 0
	var part strings.Builder
	inString := false
	inDQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			if inString && i+1 < len(s) && s[i+1] == '\'' {
				part.WriteByte(c)
				part.WriteByte(s[i+1])
				i++
				continue
			}
			inString = !inString
			part.WriteByte(c)
			continue
		}
		if inString {
			part.WriteByte(c)
			continue
		}
		if c == '"' {
			inDQuote = !inDQuote
			part.WriteByte(c)
			continue
		}
		if inDQuote {
			part.WriteByte(c)
			continue
		}
		if c == ';' {
			if strings.TrimSpace(part.String()) != "" {
				nonEmptyParts++
			}
			part.Reset()
			if nonEmptyParts >= 2 {
				return true
			}
			continue
		}
		part.WriteByte(c)
	}
	if strings.TrimSpace(part.String()) != "" {
		nonEmptyParts++
	}
	return nonEmptyParts > 1
}

func applyRowLimit(sqlStr string, effectiveLimit int) string {
	upper := strings.ToUpper(sqlStr)
	limitIdx, _ := findLimitClause(upper)
	if limitIdx >= 0 {
		return sqlStr[:limitIdx] + "LIMIT " + strconv.Itoa(effectiveLimit)
	}
	return sqlStr + " LIMIT " + strconv.Itoa(effectiveLimit)
}

func findLimitClause(upperSQL string) (int, int) {
	inString := false
	inDQuote := false
	for i := 0; i < len(upperSQL); i++ {
		c := upperSQL[i]
		if c == '\'' {
			if inString && i+1 < len(upperSQL) && upperSQL[i+1] == '\'' {
				i++
				continue
			}
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == '"' {
			inDQuote = !inDQuote
			continue
		}
		if inDQuote {
			continue
		}
		if i+5 < len(upperSQL) && upperSQL[i:i+5] == "LIMIT" {
			next := byte(' ')
			if i+5 < len(upperSQL) {
				next = upperSQL[i+5]
			}
			if i+5 == len(upperSQL) || isWhitespace(next) || next == '(' {
				j := i + 5
				if next == '(' {
					j++
				}
				for j < len(upperSQL) && isWhitespace(upperSQL[j]) {
					j++
				}
				for j < len(upperSQL) && upperSQL[j] >= '0' && upperSQL[j] <= '9' {
					j++
				}
				for j < len(upperSQL) && isWhitespace(upperSQL[j]) {
					j++
				}
				if j+6 <= len(upperSQL) && upperSQL[j:j+6] == "OFFSET" {
					j += 6
					for j < len(upperSQL) && isWhitespace(upperSQL[j]) {
						j++
					}
					for j < len(upperSQL) && upperSQL[j] >= '0' && upperSQL[j] <= '9' {
						j++
					}
				}
				return i, j - i
			}
		}
	}
	return -1, 0
}

func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func formatValue(v any) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case []byte:
		s := hex.EncodeToString(val)
		if len(s) > 256 {
			return "0x" + s[:256] + "..."
		}
		return "0x" + s
	case time.Time:
		return val.Format(time.RFC3339Nano)
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
