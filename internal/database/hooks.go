// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package database

import (
	"context"
	"database/sql"

	"github.com/canonical/sqlair"
	"github.com/juju/errors"

	coretrace "github.com/juju/juju/core/trace"
	"github.com/juju/juju/internal/database/txn"
)

// sqlair statements used by the sqlair (Txn) hook pair.
var (
	changeLogSetupStmt = sqlair.MustPrepare(
		`UPDATE change_log_trace_ctx
SET is_in_txn = 1, trace_id = $M.trace_id, span_id = $M.span_id`,
		sqlair.M{},
	)
	changeLogSeqIncStmt = sqlair.MustPrepare(
		`UPDATE change_log_txn_seq SET id = id + 1`,
	)
	changeLogBackfillStmt = sqlair.MustPrepare(
		`UPDATE change_log
SET txn_id = (SELECT id FROM change_log_txn_seq)
WHERE txn_id = 0`,
	)
	changeLogResetCtxStmt = sqlair.MustPrepare(
		`UPDATE change_log_trace_ctx
SET is_in_txn = 0, trace_id = '', span_id = ''`,
	)
)

// ChangeLogTxnHooks returns TxnHooks that maintain the change_log
// tracing and sequencing tables around each write transaction.
//
// The sqlair pair (Setup/Finalise) is used by Txn. The stdlib pair
// (StdSetup/StdFinalise) is used by StdTxn. Both pairs execute the
// same logical SQL.
//
// Setup sets is_in_txn = 1 and writes the active OTel trace/span IDs
// into change_log_trace_ctx immediately after BEGIN IMMEDIATE.
//
// Finalise advances change_log_txn_seq, back-fills change_log.txn_id
// for rows written during the transaction, and resets
// change_log_trace_ctx before COMMIT.
func ChangeLogTxnHooks() txn.TxnHooks {
	return txn.TxnHooks{
		Setup:       changeLogSqulairSetup,
		Finalise:    changeLogSqulairFinalise,
		StdSetup:    changeLogSetup,
		StdFinalise: changeLogFinalise,
	}
}

func changeLogSqulairSetup(ctx context.Context, tx *sqlair.TX) error {
	traceID, spanID, _, ok := coretrace.ScopeFromContext(ctx)
	if !ok {
		traceID = ""
		spanID = ""
	}
	return errors.Trace(
		tx.Query(ctx, changeLogSetupStmt, sqlair.M{
			"trace_id": traceID,
			"span_id":  spanID,
		}).Run(),
	)
}

func changeLogSqulairFinalise(ctx context.Context, tx *sqlair.TX) error {
	if err := tx.Query(ctx, changeLogSeqIncStmt).Run(); err != nil {
		return errors.Trace(err)
	}
	if err := tx.Query(ctx, changeLogBackfillStmt).Run(); err != nil {
		return errors.Trace(err)
	}
	return errors.Trace(tx.Query(ctx, changeLogResetCtxStmt).Run())
}

func changeLogSetup(ctx context.Context, tx *sql.Tx) error {
	traceID, spanID, _, ok := coretrace.ScopeFromContext(ctx)
	if !ok {
		traceID = ""
		spanID = ""
	}
	_, err := tx.ExecContext(
		ctx,
		`UPDATE change_log_trace_ctx
SET is_in_txn = 1, trace_id = ?, span_id = ?`,
		traceID, spanID,
	)
	return errors.Trace(err)
}

func changeLogFinalise(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE change_log_txn_seq SET id = id + 1`,
	); err != nil {
		return errors.Trace(err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE change_log
SET txn_id = (SELECT id FROM change_log_txn_seq)
WHERE txn_id = 0`,
	); err != nil {
		return errors.Trace(err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE change_log_trace_ctx
SET is_in_txn = 0, trace_id = '', span_id = ''`,
	); err != nil {
		return errors.Trace(err)
	}
	return nil
}
