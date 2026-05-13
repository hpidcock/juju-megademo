// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"context"
	"database/sql"
	"testing"

	"github.com/juju/tc"

	schematesting "github.com/juju/juju/domain/schema/testing"
)

type stateSuite struct {
	schematesting.ControllerSuite
	state *State
}

func TestStateSuite(t *testing.T) {
	tc.Run(t, &stateSuite{})
}

func (s *stateSuite) SetUpTest(c *tc.C) {
	s.ControllerSuite.SetUpTest(c)
	s.state = NewState(s.TxnRunnerFactory())
}

func (s *stateSuite) TestCurrentTxnID(c *tc.C) {
	txnID, err := s.state.CurrentTxnID(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(txnID, tc.Equals, int64(0))
}

func (s *stateSuite) TestDebugStateInitial(c *tc.C) {
	state, stepTarget, err := s.state.DebugState(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(state, tc.Equals, "running")
	c.Check(stepTarget, tc.Equals, int64(0))
}

func (s *stateSuite) TestSetPaused(c *tc.C) {
	err := s.state.SetPaused(c.Context())
	c.Assert(err, tc.ErrorIsNil)

	state, stepTarget, err := s.state.DebugState(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(state, tc.Equals, "paused")
	c.Check(stepTarget, tc.Equals, int64(0))
}

func (s *stateSuite) TestSetStep(c *tc.C) {
	err := s.state.SetStep(c.Context(), 42)
	c.Assert(err, tc.ErrorIsNil)

	state, stepTarget, err := s.state.DebugState(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(state, tc.Equals, "step")
	c.Check(stepTarget, tc.Equals, int64(42))
}

func (s *stateSuite) TestSetRunning(c *tc.C) {
	err := s.state.SetPaused(c.Context())
	c.Assert(err, tc.ErrorIsNil)

	err = s.state.SetRunning(c.Context())
	c.Assert(err, tc.ErrorIsNil)

	state, _, err := s.state.DebugState(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(state, tc.Equals, "running")
}

func (s *stateSuite) TestAllNodesReachedTxnEmpty(c *tc.C) {
	reached, err := s.state.AllNodesReachedTxn(c.Context(), 5)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(reached, tc.IsTrue)
}

func (s *stateSuite) TestAllNodesReachedTxnAllReached(c *tc.C) {
	err := s.TxnRunner().StdTxn(
		c.Context(),
		func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(
				ctx,
				`INSERT INTO change_log_witness
(controller_id, lower_bound, upper_bound) VALUES (?, 0, ?)`,
				"ctrl-1", 10,
			)
			return err
		},
	)
	c.Assert(err, tc.ErrorIsNil)

	reached, err := s.state.AllNodesReachedTxn(c.Context(), 5)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(reached, tc.IsTrue)
}

func (s *stateSuite) TestAllNodesReachedTxnNotAllReached(c *tc.C) {
	err := s.TxnRunner().StdTxn(
		c.Context(),
		func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(
				ctx,
				`INSERT INTO change_log_witness
(controller_id, lower_bound, upper_bound) VALUES (?, 0, ?)`,
				"ctrl-1", 3,
			)
			return err
		},
	)
	c.Assert(err, tc.ErrorIsNil)

	reached, err := s.state.AllNodesReachedTxn(c.Context(), 5)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(reached, tc.IsFalse)
}

func (s *stateSuite) TestEventCountInRangeEmpty(c *tc.C) {
	count, err := s.state.EventCountInRange(c.Context(), 1, 10)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(count, tc.Equals, 0)
}

func (s *stateSuite) TestEventCountInRange(c *tc.C) {
	err := s.TxnRunner().StdTxn(
		c.Context(),
		func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(
				ctx,
				`INSERT INTO change_log_namespace
(id, namespace, description) VALUES (1, 'test', 'test namespace')`,
			)
			if err != nil {
				return err
			}
			for _, txnID := range []int{1, 2, 3} {
				_, err := tx.ExecContext(
					ctx,
					`INSERT INTO change_log
(edit_type_id, namespace_id, changed, txn_id) VALUES (1, 1, 'x', ?)`,
					txnID,
				)
				if err != nil {
					return err
				}
			}
			return nil
		},
	)
	c.Assert(err, tc.ErrorIsNil)

	count, err := s.state.EventCountInRange(c.Context(), 1, 2)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(count, tc.Equals, 2)

	count, err = s.state.EventCountInRange(c.Context(), 1, 3)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(count, tc.Equals, 3)
}
