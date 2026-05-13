// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package database_test

import (
	"context"
	"database/sql"
	stdtesting "testing"

	"github.com/juju/tc"

	coretrace "github.com/juju/juju/core/trace"
	"github.com/juju/juju/domain/schema"
	schematesting "github.com/juju/juju/domain/schema/testing"
	"github.com/juju/juju/internal/database"
	dbtesting "github.com/juju/juju/internal/database/testing"
)

// modelConfigNamespaceID is the namespace_id for model_config in the model
// schema. It equals reservedCustomNamespaceIDOffset (10000) + iota(0).
const modelConfigNamespaceID = 10000

type hooksSuite struct {
	dbtesting.DqliteSuite
}

func TestHooksSuite(t *stdtesting.T) {
	tc.Run(t, &hooksSuite{})
}

func (s *hooksSuite) SetUpTest(c *tc.C) {
	s.DqliteSuite.SetUpTest(c)
	s.ApplyDDL(c, &schematesting.SchemaApplier{
		Schema:  schema.ModelDDL(),
		Verbose: s.Verbose,
	})
}

// runWithHooks executes fn inside a plain write transaction wrapped by the
// ChangeLogTxnHooks Setup and Finalise calls. This simulates the behaviour of
// RetryingTxnRunner.StdTxn on the write path without requiring DQLite's
// SQLITE_READONLY upgrade mechanism.
func (s *hooksSuite) runWithHooks(
	ctx context.Context,
	c *tc.C,
	fn func(context.Context, *sql.Tx) error,
) {
	hooks := database.ChangeLogTxnHooks()
	tx, err := s.DB().BeginTx(ctx, nil)
	c.Assert(err, tc.ErrorIsNil)
	if err := hooks.StdSetup(ctx, tx); err != nil {
		_ = tx.Rollback()
		c.Fatalf("hooks.Setup: %v", err)
	}
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback()
		c.Fatalf("tx fn: %v", err)
	}
	if err := hooks.StdFinalise(ctx, tx); err != nil {
		_ = tx.Rollback()
		c.Fatalf("hooks.Finalise: %v", err)
	}
	c.Assert(tx.Commit(), tc.ErrorIsNil)
}

func (s *hooksSuite) TestWriteTxnStampsChangeLogTxnID(c *tc.C) {
	s.runWithHooks(c.Context(), c, func(
		ctx context.Context, tx *sql.Tx,
	) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO change_log (edit_type_id, namespace_id, changed)
VALUES (1, ?, 'stamp-uuid-1')`,
			modelConfigNamespaceID,
		)
		return err
	})

	var txnID int
	err := s.DB().QueryRowContext(
		c.Context(),
		`SELECT txn_id FROM change_log WHERE changed = 'stamp-uuid-1'`,
	).Scan(&txnID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(txnID != 0, tc.IsTrue)
}

func (s *hooksSuite) TestWriteTxnAllRowsShareTxnID(c *tc.C) {
	s.runWithHooks(c.Context(), c, func(
		ctx context.Context, tx *sql.Tx,
	) error {
		for _, u := range []string{"shared-a", "shared-b", "shared-c"} {
			_, err := tx.ExecContext(
				ctx,
				`INSERT INTO change_log (edit_type_id, namespace_id, changed)
VALUES (1, ?, ?)`,
				modelConfigNamespaceID, u,
			)
			if err != nil {
				return err
			}
		}
		return nil
	})

	rows, err := s.DB().QueryContext(
		c.Context(),
		`SELECT txn_id FROM change_log
WHERE changed IN ('shared-a', 'shared-b', 'shared-c')
ORDER BY id`,
	)
	c.Assert(err, tc.ErrorIsNil)
	defer func() { _ = rows.Close() }()

	var txnIDs []int
	for rows.Next() {
		var id int
		c.Assert(rows.Scan(&id), tc.ErrorIsNil)
		txnIDs = append(txnIDs, id)
	}
	c.Assert(rows.Err(), tc.ErrorIsNil)
	c.Assert(len(txnIDs), tc.Equals, 3)
	c.Check(txnIDs[0] != 0, tc.IsTrue)
	c.Check(txnIDs[1], tc.Equals, txnIDs[0])
	c.Check(txnIDs[2], tc.Equals, txnIDs[0])
}

func (s *hooksSuite) TestSequentialTxnsDifferentTxnIDs(c *tc.C) {
	s.runWithHooks(c.Context(), c, func(
		ctx context.Context, tx *sql.Tx,
	) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO change_log (edit_type_id, namespace_id, changed)
VALUES (1, ?, 'seq-first')`,
			modelConfigNamespaceID,
		)
		return err
	})

	s.runWithHooks(c.Context(), c, func(
		ctx context.Context, tx *sql.Tx,
	) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO change_log (edit_type_id, namespace_id, changed)
VALUES (1, ?, 'seq-second')`,
			modelConfigNamespaceID,
		)
		return err
	})

	var firstID, secondID int
	err := s.DB().QueryRowContext(
		c.Context(),
		`SELECT txn_id FROM change_log WHERE changed = 'seq-first'`,
	).Scan(&firstID)
	c.Assert(err, tc.ErrorIsNil)

	err = s.DB().QueryRowContext(
		c.Context(),
		`SELECT txn_id FROM change_log WHERE changed = 'seq-second'`,
	).Scan(&secondID)
	c.Assert(err, tc.ErrorIsNil)

	c.Check(secondID > firstID, tc.IsTrue)
}

func (s *hooksSuite) TestTraceScopePopulated(c *tc.C) {
	ctx := coretrace.WithTraceScope(
		c.Context(), "aaaa-trace-id", "bbbb-span-id", 1,
	)
	s.runWithHooks(ctx, c, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO change_log (edit_type_id, namespace_id, changed)
VALUES (1, ?, 'traced-uuid')`,
			modelConfigNamespaceID,
		)
		return err
	})

	var traceID, spanID string
	err := s.DB().QueryRowContext(
		c.Context(),
		`SELECT trace_id, span_id FROM change_log WHERE changed = 'traced-uuid'`,
	).Scan(&traceID, &spanID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(traceID, tc.Equals, "aaaa-trace-id")
	c.Check(spanID, tc.Equals, "bbbb-span-id")
}

func (s *hooksSuite) TestNoTraceScopeEmptyStrings(c *tc.C) {
	s.runWithHooks(c.Context(), c, func(
		ctx context.Context, tx *sql.Tx,
	) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO change_log (edit_type_id, namespace_id, changed)
VALUES (1, ?, 'no-trace-uuid')`,
			modelConfigNamespaceID,
		)
		return err
	})

	var traceID, spanID string
	err := s.DB().QueryRowContext(
		c.Context(),
		`SELECT trace_id, span_id FROM change_log WHERE changed = 'no-trace-uuid'`,
	).Scan(&traceID, &spanID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(traceID, tc.Equals, "")
	c.Check(spanID, tc.Equals, "")
}

func (s *hooksSuite) TestChangeLogTraceCtxResetAfterCommit(c *tc.C) {
	ctx := coretrace.WithTraceScope(
		c.Context(), "trace-reset", "span-reset", 1,
	)
	s.runWithHooks(ctx, c, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO change_log (edit_type_id, namespace_id, changed)
VALUES (1, ?, 'reset-uuid')`,
			modelConfigNamespaceID,
		)
		return err
	})

	var isInTxn int
	var traceID, spanID string
	err := s.DB().QueryRowContext(
		c.Context(),
		`SELECT is_in_txn, trace_id, span_id
FROM change_log_trace_ctx WHERE id = 1`,
	).Scan(&isInTxn, &traceID, &spanID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(isInTxn, tc.Equals, 0)
	c.Check(traceID, tc.Equals, "")
	c.Check(spanID, tc.Equals, "")
}

// TestReadOnlyTxnDoesNotModifyTraceCtx verifies that a transaction which only
// reads does not invoke the hooks. The trace ctx sentinel is_in_txn must
// remain 0 before and after the read.
func (s *hooksSuite) TestReadOnlyTxnDoesNotModifyTraceCtx(c *tc.C) {
	var isInTxnBefore int
	err := s.DB().QueryRowContext(
		c.Context(),
		`SELECT is_in_txn FROM change_log_trace_ctx WHERE id = 1`,
	).Scan(&isInTxnBefore)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(isInTxnBefore, tc.Equals, 0)

	// Perform a read-only query with no hook involvement.
	tx, err := s.DB().BeginTx(c.Context(), &sql.TxOptions{ReadOnly: true})
	c.Assert(err, tc.ErrorIsNil)
	var count int
	err = tx.QueryRowContext(
		c.Context(), `SELECT COUNT(*) FROM change_log`,
	).Scan(&count)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(tx.Commit(), tc.ErrorIsNil)

	var isInTxnAfter int
	err = s.DB().QueryRowContext(
		c.Context(),
		`SELECT is_in_txn FROM change_log_trace_ctx WHERE id = 1`,
	).Scan(&isInTxnAfter)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(isInTxnAfter, tc.Equals, 0)
}
