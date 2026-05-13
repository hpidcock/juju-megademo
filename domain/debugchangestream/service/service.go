// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package service implements the business logic for the
// debugchangestream domain.
package service

import (
	"context"
	"time"

	"github.com/juju/juju/core/logger"
	debugchangestreamerrors "github.com/juju/juju/domain/debugchangestream/errors"
	"github.com/juju/juju/internal/errors"
)

// State defines the persistence operations required by Service.
type State interface {
	// CurrentTxnID returns the current value of change_log_txn_seq.
	CurrentTxnID(ctx context.Context) (int64, error)

	// DebugState returns the current state and step_target from
	// debug_change_stream.
	DebugState(
		ctx context.Context,
	) (state string, stepTarget int64, err error)

	// SetPaused writes state='paused' to debug_change_stream.
	SetPaused(ctx context.Context) error

	// SetStep writes state='step' and the given step_target to
	// debug_change_stream.
	SetStep(ctx context.Context, stepTarget int64) error

	// SetRunning writes state='running' to debug_change_stream.
	SetRunning(ctx context.Context) error

	// AllNodesReachedTxn returns true when every row in
	// change_log_witness has upper_bound >= txnID.
	AllNodesReachedTxn(ctx context.Context, txnID int64) (bool, error)

	// EventCountInRange returns the number of change_log rows whose
	// txn_id is in the inclusive range [minTxn, maxTxn].
	EventCountInRange(
		ctx context.Context, minTxn, maxTxn int64,
	) (int, error)
}

// StepResult describes what was consumed during a single sub-step.
type StepResult struct {
	// TxnMin is the lowest txn_id dispatched.
	TxnMin int64
	// TxnMax is the highest txn_id dispatched (= step_target).
	TxnMax int64
	// EventCount is the number of change_log rows in the
	// [TxnMin, TxnMax] txn_id range. This represents how many
	// events became visible to the system during the step.
	EventCount int
}

// Service provides pause, step, and resume operations on a single
// database's changestream debug state.
type Service struct {
	st     State
	logger logger.Logger
}

// NewService constructs a Service.
func NewService(st State, logger logger.Logger) *Service {
	return &Service{
		st:     st,
		logger: logger,
	}
}

// Pause transitions the changestream from running to paused.
// Returns ErrAlreadyPaused if already paused or stepping.
func (s *Service) Pause(ctx context.Context) error {
	state, _, err := s.st.DebugState(ctx)
	if err != nil {
		return errors.Errorf("reading debug state: %w", err)
	}
	if state == "paused" || state == "step" {
		return debugchangestreamerrors.ErrAlreadyPaused
	}
	if err := s.st.SetPaused(ctx); err != nil {
		return errors.Errorf("setting paused: %w", err)
	}
	s.logger.Debugf(ctx, "changestream paused")
	return nil
}

// Step advances the paused changestream by count transactions.
// Returns ErrNotPaused if the stream is not currently paused.
// Blocks with polling until all HA nodes have consumed each step.
func (s *Service) Step(
	ctx context.Context, count int,
) ([]StepResult, error) {
	state, stepTarget, err := s.st.DebugState(ctx)
	if err != nil {
		return nil, errors.Errorf("reading debug state: %w", err)
	}
	if state != "paused" {
		return nil, debugchangestreamerrors.ErrNotPaused
	}

	// lastWatermark tracks where the stream has been stepped to.
	// Use the persisted step_target as the starting watermark so
	// that repeated calls to Step correctly advance from where the
	// previous call left off.
	lastWatermark := stepTarget
	results := make([]StepResult, 0, count)

	for i := 0; i < count; i++ {
		currentTxn, err := s.st.CurrentTxnID(ctx)
		if err != nil {
			return results, errors.Errorf(
				"reading txn seq: %w", err,
			)
		}

		// Stream is already at the head of the log.
		if currentTxn == lastWatermark {
			results = append(results, StepResult{
				TxnMin:     lastWatermark,
				TxnMax:     lastWatermark,
				EventCount: 0,
			})
			continue
		}

		if err := s.st.SetStep(ctx, currentTxn); err != nil {
			return results, errors.Errorf(
				"setting step target: %w", err,
			)
		}
		s.logger.Debugf(
			ctx,
			"changestream step: target=%d", currentTxn,
		)

		// Poll until all nodes have consumed up to currentTxn.
		if err := s.pollUntilReached(ctx, currentTxn); err != nil {
			return results, err
		}

		txnMin := lastWatermark + 1
		eventCount, err := s.st.EventCountInRange(
			ctx, txnMin, currentTxn,
		)
		if err != nil {
			return results, errors.Errorf(
				"counting events in range: %w", err,
			)
		}

		results = append(results, StepResult{
			TxnMin:     txnMin,
			TxnMax:     currentTxn,
			EventCount: eventCount,
		})
		lastWatermark = currentTxn
	}

	return results, nil
}

// pollUntilReached blocks until all nodes report upper_bound >= txnID
// or the context is cancelled.
func (s *Service) pollUntilReached(
	ctx context.Context, txnID int64,
) error {
	for {
		reached, err := s.st.AllNodesReachedTxn(ctx, txnID)
		if err != nil {
			return errors.Errorf(
				"polling node watermarks: %w", err,
			)
		}
		if reached {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Resume transitions the changestream back to running.
// Returns ErrNotPaused if the stream is not currently paused.
func (s *Service) Resume(ctx context.Context) error {
	state, _, err := s.st.DebugState(ctx)
	if err != nil {
		return errors.Errorf("reading debug state: %w", err)
	}
	if state == "running" {
		return debugchangestreamerrors.ErrNotPaused
	}
	if err := s.st.SetRunning(ctx); err != nil {
		return errors.Errorf("setting running: %w", err)
	}
	s.logger.Debugf(ctx, "changestream resumed")
	return nil
}

// Status returns the current debug state string.
func (s *Service) Status(ctx context.Context) (string, error) {
	state, _, err := s.st.DebugState(ctx)
	if err != nil {
		return "", errors.Errorf("reading debug state: %w", err)
	}
	return state, nil
}
