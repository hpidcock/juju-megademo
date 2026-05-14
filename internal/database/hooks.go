// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package database

import (
	"context"

	"github.com/juju/errors"

	coretrace "github.com/juju/juju/core/trace"
	"github.com/juju/juju/internal/database/hookdriver"
)

// ChangeLogTxnHooks returns Hooks that maintain the change_log tracing
// and sequencing tables around each write transaction.
//
// Setup sets is_in_txn = 1 and writes the active OTel trace/span IDs
// into change_log_trace_ctx immediately before the first write statement.
//
// Finalise advances change_log_txn_seq, back-fills change_log.txn_id
// for rows written during the transaction, and resets
// change_log_trace_ctx before COMMIT.
func ChangeLogTxnHooks() hookdriver.Hooks {
	return hookdriver.Hooks{
		Setup:    changeLogSetup,
		Finalise: changeLogFinalise,
	}
}

func changeLogSetup(
	ctx context.Context,
	exec hookdriver.ExecFunc,
) error {
	traceID, spanID, _, ok := coretrace.ScopeFromContext(ctx)
	if !ok {
		traceID = ""
		spanID = ""
	}
	err := exec(
		ctx,
		`UPDATE change_log_trace_ctx
SET is_in_txn = 1, trace_id = ?, span_id = ?`,
		traceID, spanID,
	)
	if err != nil && !IsExtendedErrorCode(err) {
		return errors.Trace(err)
	}
	return nil
}

func changeLogFinalise(
	ctx context.Context,
	exec hookdriver.ExecFunc,
) error {
	seqErr := exec(ctx, `UPDATE change_log_txn_seq SET id = id + 1`)
	if seqErr != nil && !IsExtendedErrorCode(seqErr) {
		return errors.Trace(seqErr)
	}
	// Only back-fill txn_id when the sequence table exists; the
	// subquery would fail identically if we proceeded without it.
	if seqErr == nil {
		if err := exec(
			ctx,
			`UPDATE change_log
SET txn_id = (SELECT id FROM change_log_txn_seq)
WHERE txn_id = 0`,
		); err != nil {
			return errors.Trace(err)
		}
	}
	err := exec(
		ctx,
		`UPDATE change_log_trace_ctx
SET is_in_txn = 0, trace_id = '', span_id = ''`,
	)
	if err != nil && !IsExtendedErrorCode(err) {
		return errors.Trace(err)
	}
	return nil
}
