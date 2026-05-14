// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package hookdriver

import (
	"context"
	"database/sql"
	"path/filepath"
	stdtesting "testing"

	"github.com/juju/tc"
	_ "github.com/mattn/go-sqlite3"
)

func TestHookDriverSuite(t *stdtesting.T) {
	tc.Run(t, &hookDriverSuite{})
}

type hookDriverSuite struct{}

func (s *hookDriverSuite) TestIsWriteStatement(c *tc.C) {
	writes := []string{
		"INSERT INTO foo VALUES (1)",
		"insert into foo values (1)",
		"UPDATE foo SET x = 1",
		"update foo set x = 1",
		"DELETE FROM foo",
		"delete from foo",
		"REPLACE INTO foo VALUES (1)",
		"replace into foo values (1)",
		"CREATE TABLE foo (id INT)",
		"create table foo (id int)",
		"DROP TABLE foo",
		"drop table foo",
		"ALTER TABLE foo ADD COLUMN x INT",
		"alter table foo add column x int",
		"  INSERT INTO foo VALUES (1)", // leading space
		"-- a comment\nINSERT INTO foo VALUES (1)",
	}
	for _, q := range writes {
		c.Check(
			isWriteStatement(q),
			tc.IsTrue,
			tc.Commentf("expected write: %q", q),
		)
	}

	reads := []string{
		"SELECT 1",
		"select count(*) from foo",
		"WITH cte AS (SELECT 1) SELECT * FROM cte",
		"PRAGMA table_info(foo)",
		"BEGIN",
		"COMMIT",
		"ROLLBACK",
		"",
		"-- just a comment",
	}
	for _, q := range reads {
		c.Check(
			isWriteStatement(q),
			tc.IsFalse,
			tc.Commentf("expected read: %q", q),
		)
	}
}

// openShimDB creates a file-backed raw DB for schema setup, applies ddl,
// then closes the raw DB and re-opens via WrapDB with the given hooks.
func openShimDB(
	c *tc.C,
	ddl string,
	hooks Hooks,
) (shimDB *sql.DB, cleanup func()) {
	path := filepath.Join(c.MkDir(), "test.db")

	raw, err := sql.Open("sqlite3", path)
	c.Assert(err, tc.ErrorIsNil)
	_, err = raw.Exec(ddl)
	c.Assert(err, tc.ErrorIsNil)
	_ = raw.Close()

	raw2, err := sql.Open("sqlite3", path)
	c.Assert(err, tc.ErrorIsNil)

	shimDB = WrapDB(raw2, path, hooks)
	return shimDB, func() {
		_ = shimDB.Close()
		_ = raw2.Close()
	}
}

func (s *hookDriverSuite) TestShimDoesNotCallSetupForReads(c *tc.C) {
	var setupCalled bool
	hooks := Hooks{
		Setup: func(ctx context.Context, exec ExecFunc) error {
			setupCalled = true
			return nil
		},
		Finalise: func(ctx context.Context, exec ExecFunc) error {
			return nil
		},
	}

	shimDB, cleanup := openShimDB(c, "CREATE TABLE t (id INT)", hooks)
	defer cleanup()

	err := shimDB.QueryRowContext(
		c.Context(), "SELECT COUNT(*) FROM t",
	).Scan(new(int))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(setupCalled, tc.IsFalse)
}

// TestShimCallsSetupOnFirstWrite verifies that Setup is called exactly once
// for a transaction containing write statements.
func (s *hookDriverSuite) TestShimCallsSetupOnFirstWrite(c *tc.C) {
	var setupCount, finaliseCount int
	hooks := Hooks{
		Setup: func(ctx context.Context, exec ExecFunc) error {
			setupCount++
			return nil
		},
		Finalise: func(ctx context.Context, exec ExecFunc) error {
			finaliseCount++
			return nil
		},
	}

	shimDB, cleanup := openShimDB(c, "CREATE TABLE t (id INT)", hooks)
	defer cleanup()

	tx, err := shimDB.BeginTx(c.Context(), nil)
	c.Assert(err, tc.ErrorIsNil)
	_, err = tx.ExecContext(c.Context(), "INSERT INTO t VALUES (1)")
	c.Assert(err, tc.ErrorIsNil)
	_, err = tx.ExecContext(c.Context(), "INSERT INTO t VALUES (2)")
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(tx.Commit(), tc.ErrorIsNil)

	c.Check(setupCount, tc.Equals, 1)
	c.Check(finaliseCount, tc.Equals, 1)
}

// TestShimDoesNotCallFinaliseOnRollback verifies that Finalise is not called
// when a write transaction is rolled back.
func (s *hookDriverSuite) TestShimDoesNotCallFinaliseOnRollback(c *tc.C) {
	var finaliseCount int
	hooks := Hooks{
		Setup: func(ctx context.Context, exec ExecFunc) error {
			return nil
		},
		Finalise: func(ctx context.Context, exec ExecFunc) error {
			finaliseCount++
			return nil
		},
	}

	shimDB, cleanup := openShimDB(c, "CREATE TABLE t (id INT)", hooks)
	defer cleanup()

	tx, err := shimDB.BeginTx(c.Context(), nil)
	c.Assert(err, tc.ErrorIsNil)
	_, err = tx.ExecContext(c.Context(), "INSERT INTO t VALUES (1)")
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(tx.Rollback(), tc.ErrorIsNil)

	c.Check(finaliseCount, tc.Equals, 0)
}
