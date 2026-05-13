// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"testing"

	"github.com/juju/tc"
	gomock "go.uber.org/mock/gomock"

	debugchangestreamerrors "github.com/juju/juju/domain/debugchangestream/errors"
	loggertesting "github.com/juju/juju/internal/logger/testing"
	"github.com/juju/juju/internal/testhelpers"
)

type serviceSuite struct {
	testhelpers.IsolationSuite

	state *MockState
}

func TestServiceSuite(t *testing.T) {
	tc.Run(t, &serviceSuite{})
}

func (s *serviceSuite) TestPauseSuccess(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().DebugState(gomock.Any()).Return("running", int64(0), nil)
	s.state.EXPECT().SetPaused(gomock.Any()).Return(nil)

	svc := NewService(s.state, loggertesting.WrapCheckLog(c))
	err := svc.Pause(c.Context())
	c.Assert(err, tc.ErrorIsNil)
}

func (s *serviceSuite) TestPauseAlreadyPaused(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().DebugState(gomock.Any()).Return("paused", int64(0), nil)

	svc := NewService(s.state, loggertesting.WrapCheckLog(c))
	err := svc.Pause(c.Context())
	c.Assert(err, tc.ErrorIs, debugchangestreamerrors.ErrAlreadyPaused)
}

func (s *serviceSuite) TestPauseStepping(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().DebugState(gomock.Any()).Return("step", int64(0), nil)

	svc := NewService(s.state, loggertesting.WrapCheckLog(c))
	err := svc.Pause(c.Context())
	c.Assert(err, tc.ErrorIs, debugchangestreamerrors.ErrAlreadyPaused)
}

func (s *serviceSuite) TestStepNotPaused(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().DebugState(gomock.Any()).Return("running", int64(0), nil)

	svc := NewService(s.state, loggertesting.WrapCheckLog(c))
	_, err := svc.Step(c.Context(), 1)
	c.Assert(err, tc.ErrorIs, debugchangestreamerrors.ErrNotPaused)
}

// TestStepAtHead checks that when the changestream is already at the
// head of the log (CurrentTxnID equals the persisted step target),
// Step returns a zero-event result without advancing state.
func (s *serviceSuite) TestStepAtHead(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().DebugState(gomock.Any()).Return("paused", int64(5), nil)
	s.state.EXPECT().CurrentTxnID(gomock.Any()).Return(int64(5), nil)

	svc := NewService(s.state, loggertesting.WrapCheckLog(c))
	results, err := svc.Step(c.Context(), 1)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(len(results), tc.Equals, 1)
	c.Check(results[0].TxnMin, tc.Equals, int64(5))
	c.Check(results[0].TxnMax, tc.Equals, int64(5))
	c.Check(results[0].EventCount, tc.Equals, 0)
}

func (s *serviceSuite) TestStepSuccess(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().DebugState(gomock.Any()).Return("paused", int64(0), nil)
	s.state.EXPECT().CurrentTxnID(gomock.Any()).Return(int64(3), nil)
	s.state.EXPECT().SetStep(gomock.Any(), int64(3)).Return(nil)
	s.state.EXPECT().AllNodesReachedTxn(gomock.Any(), int64(3)).Return(true, nil)
	s.state.EXPECT().EventCountInRange(
		gomock.Any(), int64(1), int64(3),
	).Return(2, nil)

	svc := NewService(s.state, loggertesting.WrapCheckLog(c))
	results, err := svc.Step(c.Context(), 1)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(len(results), tc.Equals, 1)
	c.Check(results[0].TxnMin, tc.Equals, int64(1))
	c.Check(results[0].TxnMax, tc.Equals, int64(3))
	c.Check(results[0].EventCount, tc.Equals, 2)
}

// TestStepMultipleCount checks that a count of 2 produces two results:
// the first a real step, the second an at-head no-op when no new
// transactions have arrived.
func (s *serviceSuite) TestStepMultipleCount(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().DebugState(gomock.Any()).Return("paused", int64(0), nil)
	// Both iterations read the same txn ID.
	s.state.EXPECT().CurrentTxnID(gomock.Any()).Return(int64(3), nil).Times(2)
	s.state.EXPECT().SetStep(gomock.Any(), int64(3)).Return(nil)
	s.state.EXPECT().AllNodesReachedTxn(gomock.Any(), int64(3)).Return(true, nil)
	s.state.EXPECT().EventCountInRange(
		gomock.Any(), int64(1), int64(3),
	).Return(2, nil)

	svc := NewService(s.state, loggertesting.WrapCheckLog(c))
	results, err := svc.Step(c.Context(), 2)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(len(results), tc.Equals, 2)
	c.Check(results[0].TxnMin, tc.Equals, int64(1))
	c.Check(results[0].TxnMax, tc.Equals, int64(3))
	c.Check(results[0].EventCount, tc.Equals, 2)
	// Second iteration is at head: watermark already equals CurrentTxnID.
	c.Check(results[1].TxnMin, tc.Equals, int64(3))
	c.Check(results[1].TxnMax, tc.Equals, int64(3))
	c.Check(results[1].EventCount, tc.Equals, 0)
}

func (s *serviceSuite) TestResumeSuccess(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().DebugState(gomock.Any()).Return("paused", int64(0), nil)
	s.state.EXPECT().SetRunning(gomock.Any()).Return(nil)

	svc := NewService(s.state, loggertesting.WrapCheckLog(c))
	err := svc.Resume(c.Context())
	c.Assert(err, tc.ErrorIsNil)
}

func (s *serviceSuite) TestResumeNotPaused(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().DebugState(gomock.Any()).Return("running", int64(0), nil)

	svc := NewService(s.state, loggertesting.WrapCheckLog(c))
	err := svc.Resume(c.Context())
	c.Assert(err, tc.ErrorIs, debugchangestreamerrors.ErrNotPaused)
}

func (s *serviceSuite) TestStatusSuccess(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().DebugState(gomock.Any()).Return("paused", int64(0), nil)

	svc := NewService(s.state, loggertesting.WrapCheckLog(c))
	status, err := svc.Status(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(status, tc.Equals, "paused")
}

func (s *serviceSuite) setupMocks(c *tc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)
	s.state = NewMockState(ctrl)
	return ctrl
}
