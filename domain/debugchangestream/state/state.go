// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package state implements the persistence layer for the
// debugchangestream domain.
package state

import (
	"context"

	"github.com/canonical/sqlair"

	coredatabase "github.com/juju/juju/core/database"
	"github.com/juju/juju/domain"
	"github.com/juju/juju/internal/errors"
)

// State provides database access for debug changestream operations.
type State struct {
	*domain.StateBase
}

// NewState returns a new State using the provided TxnRunnerFactory.
func NewState(factory coredatabase.TxnRunnerFactory) *State {
	return &State{
		StateBase: domain.NewStateBase(factory),
	}
}

// CurrentTxnID returns the current value of change_log_txn_seq.
func (st *State) CurrentTxnID(ctx context.Context) (int64, error) {
	db, err := st.DB(ctx)
	if err != nil {
		return 0, errors.Capture(err)
	}

	stmt, err := st.Prepare(
		`SELECT &dbTxnSeq.id FROM change_log_txn_seq LIMIT 1;`,
		dbTxnSeq{},
	)
	if err != nil {
		return 0, errors.Errorf(
			"preparing current txn query: %w", err,
		)
	}

	var seq dbTxnSeq
	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(tx.Query(ctx, stmt).Get(&seq))
	})
	return seq.ID, errors.Capture(err)
}

// DebugState returns the current state and step_target from
// debug_change_stream.
func (st *State) DebugState(
	ctx context.Context,
) (string, int64, error) {
	db, err := st.DB(ctx)
	if err != nil {
		return "", 0, errors.Capture(err)
	}

	stmt, err := st.Prepare(
		`SELECT &dbDebugState.* FROM debug_change_stream LIMIT 1;`,
		dbDebugState{},
	)
	if err != nil {
		return "", 0, errors.Errorf(
			"preparing debug state query: %w", err,
		)
	}

	var ds dbDebugState
	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(tx.Query(ctx, stmt).Get(&ds))
	})
	return ds.State, ds.StepTarget, errors.Capture(err)
}

// SetPaused writes state='paused' to debug_change_stream.
func (st *State) SetPaused(ctx context.Context) error {
	db, err := st.DB(ctx)
	if err != nil {
		return errors.Capture(err)
	}

	stmt, err := st.Prepare(`
UPDATE debug_change_stream
SET state      = 'paused',
    updated_at = STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc');`)
	if err != nil {
		return errors.Errorf(
			"preparing set paused statement: %w", err,
		)
	}

	return errors.Capture(db.Txn(
		ctx,
		func(ctx context.Context, tx *sqlair.TX) error {
			return errors.Capture(tx.Query(ctx, stmt).Run())
		},
	))
}

// SetStep writes state='step' and the given step_target to
// debug_change_stream.
func (st *State) SetStep(
	ctx context.Context, stepTarget int64,
) error {
	db, err := st.DB(ctx)
	if err != nil {
		return errors.Capture(err)
	}

	stmt, err := st.Prepare(`
UPDATE debug_change_stream
SET state       = 'step',
    step_target = $M.step_target,
    updated_at  = STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc');`,
		sqlair.M{},
	)
	if err != nil {
		return errors.Errorf(
			"preparing set step statement: %w", err,
		)
	}

	return errors.Capture(db.Txn(
		ctx,
		func(ctx context.Context, tx *sqlair.TX) error {
			return errors.Capture(
				tx.Query(
					ctx, stmt,
					sqlair.M{"step_target": stepTarget},
				).Run(),
			)
		},
	))
}

// SetRunning writes state='running' to debug_change_stream.
func (st *State) SetRunning(ctx context.Context) error {
	db, err := st.DB(ctx)
	if err != nil {
		return errors.Capture(err)
	}

	stmt, err := st.Prepare(`
UPDATE debug_change_stream
SET state      = 'running',
    updated_at = STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc');`)
	if err != nil {
		return errors.Errorf(
			"preparing set running statement: %w", err,
		)
	}

	return errors.Capture(db.Txn(
		ctx,
		func(ctx context.Context, tx *sqlair.TX) error {
			return errors.Capture(tx.Query(ctx, stmt).Run())
		},
	))
}

// AllNodesReachedTxn returns true when every row in
// change_log_witness has upper_bound >= txnID.
func (st *State) AllNodesReachedTxn(
	ctx context.Context, txnID int64,
) (bool, error) {
	db, err := st.DB(ctx)
	if err != nil {
		return false, errors.Capture(err)
	}

	// Count rows where upper_bound has NOT yet reached txnID.
	// Result is zero when all nodes are up to date.
	stmt, err := st.Prepare(`
SELECT COUNT(*) AS &M.count
FROM   change_log_witness
WHERE  upper_bound < $M.upper_bound;`,
		sqlair.M{},
	)
	if err != nil {
		return false, errors.Errorf(
			"preparing all-nodes-reached query: %w", err,
		)
	}

	result := sqlair.M{}
	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(
			tx.Query(
				ctx, stmt,
				sqlair.M{"upper_bound": txnID},
			).Get(&result),
		)
	})
	if err != nil {
		return false, errors.Capture(err)
	}

	count, ok := result["count"].(int64)
	if !ok {
		return false, errors.New(
			"unexpected type for count result",
		)
	}
	return count == 0, nil
}

// EventCountInRange returns the number of change_log rows whose
// txn_id is in the inclusive range [minTxn, maxTxn].
func (st *State) EventCountInRange(
	ctx context.Context, minTxn, maxTxn int64,
) (int, error) {
	db, err := st.DB(ctx)
	if err != nil {
		return 0, errors.Capture(err)
	}

	stmt, err := st.Prepare(`
SELECT COUNT(*) AS &M.count
FROM   change_log
WHERE  txn_id >= $M.min_txn
AND    txn_id <= $M.max_txn;`,
		sqlair.M{},
	)
	if err != nil {
		return 0, errors.Errorf(
			"preparing event count query: %w", err,
		)
	}

	result := sqlair.M{}
	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(
			tx.Query(
				ctx, stmt,
				sqlair.M{"min_txn": minTxn, "max_txn": maxTxn},
			).Get(&result),
		)
	})
	if err != nil {
		return 0, errors.Capture(err)
	}

	count, ok := result["count"].(int64)
	if !ok {
		return 0, errors.New(
			"unexpected type for count result",
		)
	}
	return int(count), nil
}

// LatestTraceInTxnRange returns the trace_id and span_id from the
// change_log row with the highest txn_id in the inclusive range
// [minTxn, maxTxn].
func (st *State) LatestTraceInTxnRange(
	ctx context.Context, minTxn, maxTxn int64,
) (string, string, error) {
	db, err := st.DB(ctx)
	if err != nil {
		return "", "", errors.Capture(err)
	}

	stmt, err := st.Prepare(`
SELECT &dbTraceInfo.* FROM change_log
WHERE  txn_id >= $M.min_txn AND txn_id <= $M.max_txn
ORDER BY txn_id DESC LIMIT 1;`,
		dbTraceInfo{},
		sqlair.M{},
	)
	if err != nil {
		return "", "", errors.Errorf(
			"preparing latest trace query: %w", err,
		)
	}

	var info dbTraceInfo
	err = db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		err := tx.Query(
			ctx, stmt,
			sqlair.M{"min_txn": minTxn, "max_txn": maxTxn},
		).Get(&info)
		if errors.Is(err, sqlair.ErrNoRows) {
			return nil
		}
		return errors.Capture(err)
	})
	if err != nil {
		return "", "", errors.Capture(err)
	}
	return info.TraceID, info.SpanID, nil
}
