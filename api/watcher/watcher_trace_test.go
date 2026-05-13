// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package watcher_test

import (
	"context"
	"testing"

	"github.com/juju/tc"
	"go.uber.org/mock/gomock"

	apimocks "github.com/juju/juju/api/base/mocks"
	"github.com/juju/juju/api/watcher"
	coretrace "github.com/juju/juju/core/trace"
	"github.com/juju/juju/rpc/params"
)

type traceSuite struct{}

// TestTraceSuite registers the trace tests with the test runner.
func TestTraceSuite(t *testing.T) {
	tc.Run(t, &traceSuite{})
}

// TestStringsWatcherChangeContextNoTrace verifies that ChangeContext returns
// the parent context unchanged when no trace IDs have been received yet.
func (s *traceSuite) TestStringsWatcherChangeContextNoTrace(c *tc.C) {
	ctrl := gomock.NewController(c)
	defer ctrl.Finish()

	caller := apimocks.NewMockAPICaller(ctrl)
	watcherID, _ := setupWatcher[*params.StringsWatchResult](c, caller, "StringsWatcher")

	initial := params.StringsWatchResult{
		StringsWatcherId: watcherID,
		Changes:          []string{"a"},
	}
	w := watcher.NewStringsWatcher(caller, initial)
	defer w.Kill()

	select {
	case <-w.Changes():
	case <-c.Context().Done():
		c.Fatalf("timed out waiting for initial change")
	}

	parent := context.Background()
	got := w.ChangeContext(parent)
	c.Check(got, tc.Equals, parent)
}

// TestStringsWatcherChangeContextWithTrace verifies that ChangeContext returns
// a context enriched with the trace IDs from the most recently received result.
func (s *traceSuite) TestStringsWatcherChangeContextWithTrace(c *tc.C) {
	ctrl := gomock.NewController(c)
	defer ctrl.Finish()

	caller := apimocks.NewMockAPICaller(ctrl)
	watcherID, eventCh := setupWatcher[*params.StringsWatchResult](c, caller, "StringsWatcher")

	initial := params.StringsWatchResult{
		StringsWatcherId: watcherID,
		Changes:          []string{"a"},
	}
	w := watcher.NewStringsWatcher(caller, initial)
	defer w.Kill()

	select {
	case <-w.Changes():
	case <-c.Context().Done():
		c.Fatalf("timed out waiting for initial change")
	}

	go func() {
		eventCh <- &params.StringsWatchResult{
			StringsWatcherId: watcherID,
			Changes:          []string{"b"},
			TraceID:          "trace-abc",
			SpanID:           "span-def",
		}
	}()

	select {
	case <-w.Changes():
	case <-c.Context().Done():
		c.Fatalf("timed out waiting for traced change")
	}

	parent := context.Background()
	got := w.ChangeContext(parent)
	traceID, spanID, _, ok := coretrace.ScopeFromContext(got)
	c.Assert(ok, tc.IsTrue)
	c.Check(traceID, tc.Equals, "trace-abc")
	c.Check(spanID, tc.Equals, "span-def")
}
