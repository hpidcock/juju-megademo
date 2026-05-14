// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package hookdriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync"
)

// ExecFunc executes a SQL statement on the current transaction.
type ExecFunc func(ctx context.Context, query string, args ...interface{}) error

// Hooks are called around write transactions by the shim driver.
type Hooks struct {
	// Setup is called before the first write statement in a transaction.
	// If Setup returns an error, the write is not executed and the error
	// is returned to the caller.
	Setup func(ctx context.Context, exec ExecFunc) error

	// Finalise is called before Commit when at least one write statement
	// was executed. If Finalise returns an error the transaction is
	// rolled back.
	Finalise func(ctx context.Context, exec ExecFunc) error
}

// WrapDB creates a new *sql.DB backed by a shim connector that intercepts
// write statements and invokes hooks. It extracts the driver from db and
// opens a new connection pool using the given dsn. The original db is not
// closed; the caller is responsible for closing it.
func WrapDB(db *sql.DB, dsn string, hooks Hooks) *sql.DB {
	drv := db.Driver()
	var connector driver.Connector
	if dc, ok := drv.(driver.DriverContext); ok {
		if c, err := dc.OpenConnector(dsn); err == nil {
			connector = c
		}
	}
	if connector == nil {
		connector = &fallbackConnector{drv: drv, dsn: dsn}
	}
	return sql.OpenDB(&shimConnector{inner: connector, hooks: hooks})
}

// fallbackConnector wraps a driver.Driver as a driver.Connector.
type fallbackConnector struct {
	drv driver.Driver
	dsn string
}

func (c *fallbackConnector) Connect(_ context.Context) (driver.Conn, error) {
	return c.drv.Open(c.dsn)
}

func (c *fallbackConnector) Driver() driver.Driver { return c.drv }

// shimConnector wraps a Connector, injecting shimConn on each Connect.
type shimConnector struct {
	inner driver.Connector
	hooks Hooks
}

func (c *shimConnector) Connect(ctx context.Context) (driver.Conn, error) {
	inner, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &shimConn{inner: inner, hooks: c.hooks}, nil
}

func (c *shimConnector) Driver() driver.Driver { return c.inner.Driver() }

// shimConn wraps a driver.Conn and tracks per-transaction write state.
type shimConn struct {
	inner driver.Conn
	hooks Hooks

	mu           sync.Mutex
	writeStarted bool
	txCtx        context.Context
}

// execOnInner executes SQL directly on the inner conn, bypassing the
// shim. This is used for hook SQL to avoid re-entrant setup detection.
func (c *shimConn) execOnInner(
	ctx context.Context,
	query string,
	args ...interface{},
) error {
	if ec, ok := c.inner.(driver.ExecerContext); ok {
		named, err := toNamedValues(args)
		if err != nil {
			return err
		}
		_, err = ec.ExecContext(ctx, query, named)
		return err
	}
	stmt, err := c.inner.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()
	vals, err := toValues(args)
	if err != nil {
		return err
	}
	_, err = stmt.Exec(vals)
	return err
}

// maybeSetup calls the Setup hook if this is the first write statement
// in the current transaction.
func (c *shimConn) maybeSetup(ctx context.Context, query string) error {
	if c.hooks.Setup == nil || !isWriteStatement(query) {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeStarted {
		return nil
	}
	c.writeStarted = true
	txCtx := c.txCtx
	if txCtx == nil {
		txCtx = ctx
	}
	return c.hooks.Setup(
		txCtx,
		func(ctx context.Context, q string, a ...interface{}) error {
			return c.execOnInner(ctx, q, a...)
		},
	)
}

// Prepare implements driver.Conn.
func (c *shimConn) Prepare(query string) (driver.Stmt, error) {
	inner, err := c.inner.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &shimStmt{inner: inner, conn: c, query: query}, nil
}

// Close implements driver.Conn.
func (c *shimConn) Close() error { return c.inner.Close() }

// Begin implements driver.Conn.
func (c *shimConn) Begin() (driver.Tx, error) {
	tx, err := c.inner.Begin()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.writeStarted = false
	c.txCtx = context.Background()
	c.mu.Unlock()
	return &shimTx{inner: tx, conn: c, ctx: context.Background()}, nil
}

// BeginTx implements driver.ConnBeginTx.
func (c *shimConn) BeginTx(
	ctx context.Context,
	opts driver.TxOptions,
) (driver.Tx, error) {
	var (
		tx  driver.Tx
		err error
	)
	if cb, ok := c.inner.(driver.ConnBeginTx); ok {
		tx, err = cb.BeginTx(ctx, opts)
	} else {
		tx, err = c.inner.Begin()
	}
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.writeStarted = false
	c.txCtx = ctx
	c.mu.Unlock()
	return &shimTx{inner: tx, conn: c, ctx: ctx}, nil
}

// ExecContext implements driver.ExecerContext.
func (c *shimConn) ExecContext(
	ctx context.Context,
	query string,
	args []driver.NamedValue,
) (driver.Result, error) {
	if err := c.maybeSetup(ctx, query); err != nil {
		return nil, err
	}
	if ec, ok := c.inner.(driver.ExecerContext); ok {
		return ec.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

// QueryContext implements driver.QueryerContext.
func (c *shimConn) QueryContext(
	ctx context.Context,
	query string,
	args []driver.NamedValue,
) (driver.Rows, error) {
	if err := c.maybeSetup(ctx, query); err != nil {
		return nil, err
	}
	if qc, ok := c.inner.(driver.QueryerContext); ok {
		return qc.QueryContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

// shimTx wraps a driver.Tx, calling Finalise before Commit.
type shimTx struct {
	inner driver.Tx
	conn  *shimConn
	ctx   context.Context
}

// Commit implements driver.Tx.
func (t *shimTx) Commit() error {
	t.conn.mu.Lock()
	writeStarted := t.conn.writeStarted
	t.conn.writeStarted = false
	t.conn.mu.Unlock()
	if writeStarted && t.conn.hooks.Finalise != nil {
		execFn := func(
			ctx context.Context,
			query string,
			args ...interface{},
		) error {
			return t.conn.execOnInner(ctx, query, args...)
		}
		if err := t.conn.hooks.Finalise(t.ctx, execFn); err != nil {
			_ = t.inner.Rollback()
			return err
		}
	}
	return t.inner.Commit()
}

// Rollback implements driver.Tx.
func (t *shimTx) Rollback() error {
	t.conn.mu.Lock()
	t.conn.writeStarted = false
	t.conn.mu.Unlock()
	return t.inner.Rollback()
}

// shimStmt wraps a driver.Stmt, detecting writes before execution.
type shimStmt struct {
	inner driver.Stmt
	conn  *shimConn
	query string
}

// Close implements driver.Stmt.
func (s *shimStmt) Close() error { return s.inner.Close() }

// NumInput implements driver.Stmt.
func (s *shimStmt) NumInput() int { return s.inner.NumInput() }

// ExecContext implements driver.StmtExecContext.
func (s *shimStmt) ExecContext(
	ctx context.Context,
	args []driver.NamedValue,
) (driver.Result, error) {
	if err := s.conn.maybeSetup(ctx, s.query); err != nil {
		return nil, err
	}
	if ec, ok := s.inner.(driver.StmtExecContext); ok {
		return ec.ExecContext(ctx, args)
	}
	vals, err := namedToValues(args)
	if err != nil {
		return nil, err
	}
	return s.inner.Exec(vals)
}

// QueryContext implements driver.StmtQueryContext.
func (s *shimStmt) QueryContext(
	ctx context.Context,
	args []driver.NamedValue,
) (driver.Rows, error) {
	if err := s.conn.maybeSetup(ctx, s.query); err != nil {
		return nil, err
	}
	if qc, ok := s.inner.(driver.StmtQueryContext); ok {
		return qc.QueryContext(ctx, args)
	}
	vals, err := namedToValues(args)
	if err != nil {
		return nil, err
	}
	return s.inner.Query(vals)
}

// Exec implements driver.Stmt.
func (s *shimStmt) Exec(args []driver.Value) (driver.Result, error) {
	ctx := s.conn.txCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.conn.maybeSetup(ctx, s.query); err != nil {
		return nil, err
	}
	return s.inner.Exec(args)
}

// Query implements driver.Stmt.
func (s *shimStmt) Query(args []driver.Value) (driver.Rows, error) {
	ctx := s.conn.txCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.conn.maybeSetup(ctx, s.query); err != nil {
		return nil, err
	}
	return s.inner.Query(args)
}

// writeKeywords are SQL keywords that indicate a write operation.
var writeKeywords = []string{
	"INSERT",
	"UPDATE",
	"DELETE",
	"REPLACE",
	"CREATE",
	"DROP",
	"ALTER",
}

// isWriteStatement reports whether query begins with a SQL write keyword.
func isWriteStatement(query string) bool {
	query = strings.TrimSpace(query)
	// Skip leading line comments.
	for strings.HasPrefix(query, "--") {
		if i := strings.IndexByte(query, '\n'); i >= 0 {
			query = strings.TrimSpace(query[i+1:])
		} else {
			return false
		}
	}
	upper := strings.ToUpper(query)
	for _, kw := range writeKeywords {
		if !strings.HasPrefix(upper, kw) {
			continue
		}
		rest := upper[len(kw):]
		if rest == "" ||
			rest[0] == ' ' || rest[0] == '\t' ||
			rest[0] == '\n' || rest[0] == '\r' ||
			rest[0] == '(' {
			return true
		}
	}
	return false
}

func toNamedValues(args []interface{}) ([]driver.NamedValue, error) {
	named := make([]driver.NamedValue, len(args))
	for i, a := range args {
		v, err := driver.DefaultParameterConverter.ConvertValue(a)
		if err != nil {
			return nil, err
		}
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return named, nil
}

func namedToValues(named []driver.NamedValue) ([]driver.Value, error) {
	vals := make([]driver.Value, len(named))
	for i, n := range named {
		vals[i] = n.Value
	}
	return vals, nil
}

func toValues(args []interface{}) ([]driver.Value, error) {
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		v, err := driver.DefaultParameterConverter.ConvertValue(a)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}
