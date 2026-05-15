// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apiserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/canonical/sqlair"
	gorillaws "github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
	"github.com/juju/tc"

	"github.com/juju/juju/apiserver/authentication"
	"github.com/juju/juju/core/database"
)

var websocketDialer = &gorillaws.Dialer{
	HandshakeTimeout: 5 * time.Second,
}

type mockAuthenticator struct{}

func (m *mockAuthenticator) Authenticate(_ *http.Request) (authentication.AuthInfo, error) {
	return authentication.AuthInfo{}, nil
}

type mockAuthorizer struct{}

func (m *mockAuthorizer) Authorize(_ context.Context, _ authentication.AuthInfo) error {
	return nil
}

type rejectAuthorizer struct{}

func (r *rejectAuthorizer) Authorize(_ context.Context, _ authentication.AuthInfo) error {
	return authentication.ErrorEntityMissingPermission
}

type testTxnRunner struct {
	db *sql.DB
}

func (r *testTxnRunner) Txn(_ context.Context, _ func(context.Context, *sqlair.TX) error) error {
	panic("Txn not implemented in tests")
}

func (r *testTxnRunner) StdTxn(_ context.Context, fn func(context.Context, *sql.Tx) error) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			tx.Rollback()
			closed = true
		}
	}()
	if err := fn(context.Background(), tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	closed = true
	return nil
}

func (r *testTxnRunner) Dying() <-chan struct{} {
	ch := make(chan struct{})
	return ch
}

func newTestDB() *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		panic(err)
	}
	return db
}

func setupTestDqliteHandler(t *testing.T, dbGetter DBGetter, authorizer authentication.Authorizer) (*dqliteHandler, *httptest.Server) {
	t.Helper()
	handler := newDqliteHandler(
		httpContext{},
		&mockAuthenticator{},
		authorizer,
		dbGetter,
		"deadbeef-1234-5678-9abc-def012345678",
	)
	mux := http.NewServeMux()
	mux.Handle("/dqlite", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return handler, srv
}

func dialWebsocket(t *testing.T, srv *httptest.Server) *gorillaws.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/dqlite"
	conn, _, err := websocketDialer.Dial(url, nil)
	tc.Assert(t, err, tc.ErrorIsNil)
	t.Cleanup(func() { conn.Close() })
	return conn
}

func doHandshake(t *testing.T, conn *gorillaws.Conn) {
	t.Helper()
	// Consume the initial SendInitialErrorV0(nil) frame sent by the
	// handler after successful authentication.
	readInitialErrorV0(t, conn)

	msg := dqliteRequest{Version: "v1"}
	writeJSON(t, conn, msg)

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Version, tc.Equals, serverVersion)
	tc.Assert(t, resp.Error, tc.Equals, "")
}

// readInitialErrorV0 reads the params.ErrorResult frame sent by the
// handler's SendInitialErrorV0 call after authentication.
func readInitialErrorV0(t *testing.T, conn *gorillaws.Conn) {
	t.Helper()
	_, body, err := conn.ReadMessage()
	tc.Assert(t, err, tc.ErrorIsNil)

	// Strip the trailing newline from the V0 protocol convention.
	body = body[:len(body)-1]
	var errResult struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	tc.Assert(t, json.Unmarshal(body, &errResult), tc.ErrorIsNil)
	tc.Assert(t, errResult.Error, tc.IsNil)
}

func writeJSON(t *testing.T, conn *gorillaws.Conn, v interface{}) {
	t.Helper()
	err := conn.WriteJSON(v)
	tc.Assert(t, err, tc.ErrorIsNil)
}

func readJSON(t *testing.T, conn *gorillaws.Conn, v interface{}) {
	t.Helper()
	err := conn.ReadJSON(v)
	tc.Assert(t, err, tc.ErrorIsNil)
}

func setupControllerDB() *sql.DB {
	db := newTestDB()
	db.Exec("CREATE TABLE model (uuid TEXT PRIMARY KEY, name TEXT NOT NULL)")
	db.Exec("INSERT INTO model (uuid, name) VALUES ('deadbeef-1234-5678-9abc-def012345678', 'test-model')")
	return db
}

func setupModelDB() *sql.DB {
	db := newTestDB()
	db.Exec(`
		CREATE TABLE change_log (
			id INTEGER PRIMARY KEY,
			edit_type_id INTEGER NOT NULL,
			ns_id INTEGER
		)
	`)
	db.Exec("CREATE VIEW v_test AS SELECT 1 AS col")
	db.Exec("INSERT INTO change_log (edit_type_id, ns_id) VALUES (1, 100)")
	db.Exec("INSERT INTO change_log (edit_type_id, ns_id) VALUES (2, 200)")
	return db
}

func TestVersionHandshake(t *testing.T) {
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: newTestDB()}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	readInitialErrorV0(t, conn)
	msg := dqliteRequest{Version: "v1"}
	writeJSON(t, conn, msg)

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Version, tc.Equals, serverVersion)
	tc.Assert(t, resp.Error, tc.Equals, "")
}

func TestUnknownVersion(t *testing.T) {
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: newTestDB()}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	readInitialErrorV0(t, conn)
	msg := dqliteRequest{Version: "v2"}
	writeJSON(t, conn, msg)

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Version, tc.Equals, serverVersion)
	tc.Check(t, strings.Contains(resp.Error, "unsupported version"), tc.IsTrue)

	tc.Assert(t, gorillaws.IsUnexpectedCloseError(readCloseError(conn)), tc.IsTrue)
}

func readCloseError(conn *gorillaws.Conn) error {
	_, _, err := conn.NextReader()
	return err
}

func TestDatabases(t *testing.T) {
	controllerDB := setupControllerDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: controllerDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-1",
		Type:      "databases",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Version, tc.Equals, serverVersion)
	tc.Assert(t, resp.RequestID, tc.Equals, "req-1")
	tc.Assert(t, resp.Error, tc.Equals, "")

	var databases []dqliteDatabase
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &databases)
	tc.Assert(t, len(databases), tc.Equals, 2)

	tc.Check(t, databases[0].Name, tc.Equals, "controller")
	tc.Check(t, databases[0].Type, tc.Equals, "controller")
	tc.Check(t, databases[0].Namespace, tc.Equals, "controller")

	tc.Check(t, databases[1].Name, tc.Equals, "test-model")
	tc.Check(t, databases[1].Type, tc.Equals, "model")
	tc.Check(t, databases[1].UUID, tc.Equals, "deadbeef-1234-5678-9abc-def012345678")
}

func TestObjects(t *testing.T) {
	modelDB := setupModelDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: modelDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-2",
		Type:      "objects",
		Namespace: "test-model",
		Kind:      "table",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var objects []dqliteObject
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &objects)

	var names []string
	for _, o := range objects {
		tc.Check(t, o.Kind, tc.Equals, "table")
		names = append(names, o.Name)
	}
	tc.Check(t, names, tc.DeepEquals, []string{"change_log"})
}

func TestObjectsView(t *testing.T) {
	modelDB := setupModelDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: modelDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-view",
		Type:      "objects",
		Namespace: "test-model",
		Kind:      "view",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var objects []dqliteObject
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &objects)
	tc.Assert(t, len(objects), tc.Equals, 1)
	tc.Check(t, objects[0].Name, tc.Equals, "v_test")
	tc.Check(t, objects[0].Kind, tc.Equals, "view")
}

func TestDDL(t *testing.T) {
	modelDB := setupModelDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: modelDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-3",
		Type:      "ddl",
		Namespace: "test-model",
		Name:      "change_log",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var ddl dqliteDDLResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &ddl)

	tc.Check(t, ddl.Name, tc.Equals, "change_log")
	tc.Check(t, strings.Contains(ddl.SQL, "CREATE TABLE"), tc.IsTrue)
	tc.Check(t, strings.Contains(ddl.SQL, "change_log"), tc.IsTrue)
}

func TestQuery(t *testing.T) {
	modelDB := setupModelDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: modelDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-4",
		Type:      "query",
		Namespace: "test-model",
		SQL:       "SELECT id, edit_type_id, ns_id FROM change_log",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var qResult dqliteQueryResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &qResult)

	tc.Check(t, qResult.Columns, tc.DeepEquals, []string{"id", "edit_type_id", "ns_id"})
	tc.Assert(t, len(qResult.Rows), tc.Equals, 2)
	tc.Check(t, qResult.Rows[0], tc.DeepEquals, []string{"1", "1", "100"})
	tc.Check(t, qResult.Rows[1], tc.DeepEquals, []string{"2", "2", "200"})
	tc.Check(t, qResult.RowCount, tc.Equals, 2)
	tc.Check(t, qResult.Truncated, tc.IsFalse)
}

func TestMutationRejected(t *testing.T) {
	modelDB := setupModelDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: modelDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	tests := []string{
		"INSERT INTO change_log (edit_type_id) VALUES (99)",
		"UPDATE change_log SET ns_id = 999",
		"DELETE FROM change_log",
		"CREATE TABLE foo (id INTEGER)",
		"DROP TABLE change_log",
		"ALTER TABLE change_log RENAME TO bar",
		"BEGIN",
		"COMMIT",
		"ROLLBACK",
	}

	for _, sql := range tests {
		t.Run(sql, func(t *testing.T) {
			writeJSON(t, conn, dqliteRequest{
				Version:   "v1",
				RequestID: "req-mut",
				Type:      "query",
				Namespace: "test-model",
				SQL:       sql,
			})
			var resp dqliteResponse
			readJSON(t, conn, &resp)
			tc.Check(t, strings.Contains(resp.Error, "not allowed"), tc.IsTrue)
		})
	}
}

func TestMultiStatementRejected(t *testing.T) {
	modelDB := setupModelDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: modelDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-multi",
		Type:      "query",
		Namespace: "test-model",
		SQL:       "SELECT 1; SELECT 2",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Check(t, strings.Contains(resp.Error, "multi-statement"), tc.IsTrue)
}

func TestSemicolonInStringLiteral(t *testing.T) {
	modelDB := setupModelDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: modelDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-semi",
		Type:      "query",
		Namespace: "test-model",
		SQL:       "SELECT 'hello;world' AS greeting",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var qResult dqliteQueryResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &qResult)
	tc.Check(t, qResult.Rows[0], tc.DeepEquals, []string{"hello;world"})
}

func TestNullFormatting(t *testing.T) {
	db := newTestDB()
	db.Exec("CREATE TABLE t (val)")
	db.Exec("INSERT INTO t (val) VALUES (NULL)")

	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: db}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-null",
		Type:      "query",
		Namespace: "test",
		SQL:       "SELECT val FROM t",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var qResult dqliteQueryResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &qResult)
	tc.Check(t, qResult.Rows[0][0], tc.Equals, "NULL")
}

func TestBytesFormatting(t *testing.T) {
	db := newTestDB()
	db.Exec("CREATE TABLE t (data BLOB)")
	db.Exec("INSERT INTO t (data) VALUES (X'DEADBEEF')")

	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: db}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-bytes",
		Type:      "query",
		Namespace: "test",
		SQL:       "SELECT data FROM t",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var qResult dqliteQueryResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &qResult)
	tc.Check(t, qResult.Rows[0][0], tc.Equals, "0xdeadbeef")
}

func TestLongBytesTruncation(t *testing.T) {
	db := newTestDB()
	db.Exec("CREATE TABLE t (data BLOB)")
	longData := make([]byte, 300)
	for i := range longData {
		longData[i] = byte(i % 256)
	}
	db.Exec("INSERT INTO t (data) VALUES (?)", longData)

	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: db}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-bytes-long",
		Type:      "query",
		Namespace: "test",
		SQL:       "SELECT data FROM t",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var qResult dqliteQueryResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &qResult)
	tc.Check(t, strings.Contains(qResult.Rows[0][0], "..."), tc.IsTrue)
}

func TestRowLimit(t *testing.T) {
	modelDB := setupModelDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: modelDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-limit",
		Type:      "query",
		Namespace: "test-model",
		SQL:       "SELECT id, edit_type_id, ns_id FROM change_log",
		Limit:     1,
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var qResult dqliteQueryResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &qResult)
	tc.Check(t, qResult.RowCount, tc.Equals, 1)
	tc.Check(t, qResult.Truncated, tc.IsTrue)
}

func TestQueryWithExistingLimit(t *testing.T) {
	modelDB := setupModelDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: modelDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-existing-limit",
		Type:      "query",
		Namespace: "test-model",
		SQL:       "SELECT id FROM change_log LIMIT 10",
		Limit:     1,
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var qResult dqliteQueryResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &qResult)
	tc.Check(t, qResult.RowCount, tc.Equals, 1)
}

func TestAuthFailure(t *testing.T) {
	db := newTestDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: db}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &rejectAuthorizer{})

	conn := dialWebsocket(t, srv)
	_, body, err := conn.ReadMessage()
	tc.Assert(t, err, tc.ErrorIsNil)

	body = body[:len(body)-1]
	var errResult struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	tc.Assert(t, json.Unmarshal(body, &errResult), tc.ErrorIsNil)
	tc.Check(t, strings.Contains(errResult.Error.Message, "authorization"), tc.IsTrue)
}

func TestGracefulDisconnect(t *testing.T) {
	controllerDB := setupControllerDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: controllerDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	conn.Close()
}

func TestAcceptedReadOnlyKeywords(t *testing.T) {
	keywords := []string{
		"SELECT 1",
		"WITH cte AS (SELECT 1) SELECT * FROM cte",
		"EXPLAIN SELECT 1",
		"PRAGMA table_info('sqlite_master')",
		"PRAGMA foreign_key_list('change_log')",
		"PRAGMA index_list('change_log')",
		"PRAGMA index_info('idx')",
		"PRAGMA table_xinfo('change_log')",
	}

	modelDB := setupModelDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: modelDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	for _, sql := range keywords {
		t.Run(sql, func(t *testing.T) {
			conn := dialWebsocket(t, srv)
			doHandshake(t, conn)
			defer conn.Close()

			writeJSON(t, conn, dqliteRequest{
				Version:   "v1",
				RequestID: "req-kw",
				Type:      "query",
				Namespace: "test-model",
				SQL:       sql,
			})

			var resp dqliteResponse
			readJSON(t, conn, &resp)
			tc.Check(t, resp.Error, tc.Equals, "")
		})
	}
}

func TestDefaultLimitZeroUsesDefault(t *testing.T) {
	modelDB := setupModelDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: modelDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-default",
		Type:      "query",
		Namespace: "test-model",
		SQL:       "SELECT id FROM change_log",
		Limit:     0,
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")
}

func TestUnknownRequestType(t *testing.T) {
	db := newTestDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: db}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-unknown",
		Type:      "nonexistent",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Check(t, strings.Contains(resp.Error, "unknown request type"), tc.IsTrue)
}

func TestObjectsMissingNamespace(t *testing.T) {
	db := newTestDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: db}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-no-ns",
		Type:      "objects",
		Kind:      "table",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Check(t, strings.Contains(resp.Error, "namespace is required"), tc.IsTrue)
}

func TestDDLMissingName(t *testing.T) {
	db := newTestDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: db}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-no-name",
		Type:      "ddl",
		Namespace: "test",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Check(t, strings.Contains(resp.Error, "name is required"), tc.IsTrue)
}

func TestQueryMissingSQL(t *testing.T) {
	db := newTestDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: db}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-no-sql",
		Type:      "query",
		Namespace: "test",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Check(t, strings.Contains(resp.Error, "sql is required"), tc.IsTrue)
}

func TestClusterWithoutIntrospector(t *testing.T) {
	controllerDB := newTestDB()
	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: controllerDB}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-cluster",
		Type:      "cluster",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var nodes []dqliteNode
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &nodes)
	tc.Check(t, len(nodes), tc.Equals, 0)
}

func TestClusterWithIntrospector(t *testing.T) {
	mockNodes := []clusterNodeInfo{
		{ID: 0x00ab, Address: "10.0.0.1:12345", Role: "voter"},
		{ID: 0x00cd, Address: "10.0.0.2:12345", Role: "stand-by"},
	}

	db := newTestDB()
	introspector := &mockClusterIntrospector{nodes: mockNodes}
	runner := &testClusterTxnRunner{
		testTxnRunner: &testTxnRunner{db: db},
		introspector:  introspector,
	}

	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return runner, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-cluster-ci",
		Type:      "cluster",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var nodes []dqliteNode
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &nodes)
	tc.Assert(t, len(nodes), tc.Equals, 2)
	tc.Check(t, nodes[0].ID, tc.Equals, uint64(0x00ab))
	tc.Check(t, nodes[0].Address, tc.Equals, "10.0.0.1:12345")
	tc.Check(t, nodes[0].Role, tc.Equals, "voter")
	tc.Check(t, nodes[1].ID, tc.Equals, uint64(0x00cd))
	tc.Check(t, nodes[1].Address, tc.Equals, "10.0.0.2:12345")
	tc.Check(t, nodes[1].Role, tc.Equals, "stand-by")
}

func TestTimeFormatting(t *testing.T) {
	db := newTestDB()
	db.Exec("CREATE TABLE t (ts TIMESTAMP)")
	refTime := time.Date(2025, 5, 14, 12, 30, 45, 123456789, time.UTC)
	db.Exec("INSERT INTO t (ts) VALUES (?)", refTime)

	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: db}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-time",
		Type:      "query",
		Namespace: "test",
		SQL:       "SELECT ts FROM t",
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var qResult dqliteQueryResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &qResult)
	tc.Check(t, qResult.Rows[0][0], tc.Equals, "2025-05-14T12:30:45.123456789Z")
}

func TestQueryTimeout(t *testing.T) {
	db := newTestDB()
	db.Exec("CREATE TABLE t (x)")
	for i := 0; i < 1000; i++ {
		db.Exec("INSERT INTO t (x) VALUES (?)", i)
	}

	dbGetter := func(namespace string) (database.TxnRunner, error) {
		return &testTxnRunner{db: db}, nil
	}
	_, srv := setupTestDqliteHandler(t, dbGetter, &mockAuthorizer{})

	conn := dialWebsocket(t, srv)
	doHandshake(t, conn)

	writeJSON(t, conn, dqliteRequest{
		Version:   "v1",
		RequestID: "req-timeout",
		Type:      "query",
		Namespace: "test",
		SQL:       "SELECT * FROM t",
		Limit:     10,
	})

	var resp dqliteResponse
	readJSON(t, conn, &resp)
	tc.Assert(t, resp.Error, tc.Equals, "")

	var qResult dqliteQueryResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &qResult)
	tc.Check(t, len(qResult.Rows), tc.Equals, 10)
	tc.Check(t, qResult.Truncated, tc.IsTrue)
}

type mockClusterIntrospector struct {
	nodes []clusterNodeInfo
}

func (m *mockClusterIntrospector) DescribeCluster(_ context.Context) ([]clusterNodeInfo, error) {
	return m.nodes, nil
}

type testClusterTxnRunner struct {
	*testTxnRunner
	introspector *mockClusterIntrospector
}

func (r *testClusterTxnRunner) DescribeCluster(ctx context.Context) ([]clusterNodeInfo, error) {
	return r.introspector.DescribeCluster(ctx)
}

func (r *testClusterTxnRunner) Dying() <-chan struct{} {
	return r.testTxnRunner.Dying()
}