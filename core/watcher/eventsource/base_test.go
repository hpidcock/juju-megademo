// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package eventsource

import (
	"context"
	"testing"

	"github.com/juju/tc"
	"go.uber.org/goleak"

	"github.com/juju/juju/core/changestream"
	coretrace "github.com/juju/juju/core/trace"
)

func TestBaseWatcher(t *testing.T) {
	defer goleak.VerifyNone(t)
	tc.Run(t, &baseWatcherSuite{})
}

type baseWatcherSuite struct {
	baseSuite
}

// tracedChangeEvent is a changeEvent with non-empty trace fields.
type tracedChangeEvent struct {
	changeEvent
	traceID string
	spanID  string
}

func (e tracedChangeEvent) TraceID() string { return e.traceID }
func (e tracedChangeEvent) SpanID() string  { return e.spanID }

func (s *baseWatcherSuite) TestChangeContextNoTrace(c *tc.C) {
	w := s.newBaseWatcher(c)
	parent := context.Background()
	got := w.ChangeContext(parent)
	c.Check(got, tc.Equals, parent)
}

func (s *baseWatcherSuite) TestChangeContextUniformTrace(c *tc.C) {
	w := s.newBaseWatcher(c)
	w.setLastTrace([]changestream.ChangeEvent{
		tracedChangeEvent{traceID: "trace-1", spanID: "span-a"},
		tracedChangeEvent{traceID: "trace-1", spanID: "span-b"},
	})
	parent := context.Background()
	got := w.ChangeContext(parent)
	traceID, spanID, _, ok := coretrace.ScopeFromContext(got)
	c.Assert(ok, tc.IsTrue)
	c.Check(traceID, tc.Equals, "trace-1")
	c.Check(spanID, tc.Equals, "span-b")
}

func (s *baseWatcherSuite) TestChangeContextMixedTrace(c *tc.C) {
	w := s.newBaseWatcher(c)
	w.setLastTrace([]changestream.ChangeEvent{
		tracedChangeEvent{traceID: "trace-1", spanID: "span-a"},
		tracedChangeEvent{traceID: "trace-2", spanID: "span-b"},
	})
	parent := context.Background()
	got := w.ChangeContext(parent)
	c.Check(got, tc.Equals, parent)
}

func (s *baseWatcherSuite) TestSetLastTraceEmptyBatch(c *tc.C) {
	w := s.newBaseWatcher(c)
	// Seed a known value first.
	w.mu.Lock()
	w.lastTraceID = "existing"
	w.lastSpanID = "existing-span"
	w.mu.Unlock()

	w.setLastTrace(nil)

	w.mu.Lock()
	traceID, spanID := w.lastTraceID, w.lastSpanID
	w.mu.Unlock()
	c.Check(traceID, tc.Equals, "existing")
	c.Check(spanID, tc.Equals, "existing-span")
}

func (s *baseWatcherSuite) TestSetLastTraceAllEmpty(c *tc.C) {
	w := s.newBaseWatcher(c)
	w.setLastTrace([]changestream.ChangeEvent{
		changeEvent{},
		changeEvent{},
	})
	w.mu.Lock()
	traceID, spanID := w.lastTraceID, w.lastSpanID
	w.mu.Unlock()
	c.Check(traceID, tc.Equals, "")
	c.Check(spanID, tc.Equals, "")
}

func (s *baseWatcherSuite) TestSetLastTraceUniform(c *tc.C) {
	w := s.newBaseWatcher(c)
	w.setLastTrace([]changestream.ChangeEvent{
		tracedChangeEvent{traceID: "trace-1", spanID: "span-a"},
		tracedChangeEvent{traceID: "trace-1", spanID: "span-b"},
		tracedChangeEvent{traceID: "trace-1", spanID: "span-c"},
	})
	w.mu.Lock()
	traceID, spanID := w.lastTraceID, w.lastSpanID
	w.mu.Unlock()
	c.Check(traceID, tc.Equals, "trace-1")
	c.Check(spanID, tc.Equals, "span-c")
}

func (s *baseWatcherSuite) TestSetLastTraceMixed(c *tc.C) {
	w := s.newBaseWatcher(c)
	w.setLastTrace([]changestream.ChangeEvent{
		tracedChangeEvent{traceID: "trace-1", spanID: "span-a"},
		tracedChangeEvent{traceID: "trace-2", spanID: "span-b"},
	})
	w.mu.Lock()
	traceID, spanID := w.lastTraceID, w.lastSpanID
	w.mu.Unlock()
	c.Check(traceID, tc.Equals, "")
	c.Check(spanID, tc.Equals, "")
}
