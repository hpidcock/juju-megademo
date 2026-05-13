// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package eventsource

import (
	"context"
	"sync"

	"github.com/juju/collections/transform"
	"gopkg.in/tomb.v2"

	"github.com/juju/juju/core/changestream"
	"github.com/juju/juju/core/database"
	"github.com/juju/juju/core/logger"
	coretrace "github.com/juju/juju/core/trace"
)

// BaseWatcher encapsulates members common to all EventQueue-based watchers.
// It has no functionality by itself, and is intended to be embedded in
// other more specific watchers.
type BaseWatcher struct {
	tomb tomb.Tomb

	watchableDB changestream.WatchableDB
	logger      logger.Logger

	// mu guards lastTraceID and lastSpanID.
	mu          sync.Mutex
	lastTraceID string
	lastSpanID  string
}

// NewBaseWatcher returns a BaseWatcher constructed from the arguments.
func NewBaseWatcher(watchableDB changestream.WatchableDB, logger logger.Logger) *BaseWatcher {
	return &BaseWatcher{
		watchableDB: watchableDB,
		logger:      logger,
	}
}

// ChangeContext implements watcher.Watcher.
func (w *BaseWatcher) ChangeContext(
	parent context.Context,
) context.Context {
	w.mu.Lock()
	traceID, spanID := w.lastTraceID, w.lastSpanID
	w.mu.Unlock()
	if traceID == "" {
		return parent
	}
	return coretrace.WithTraceScope(parent, traceID, spanID, 0)
}

// setLastTrace caches the trace context from a batch of events.
// If all events share the same non-empty TraceID, that ID and the
// SpanID of the last event in the batch are stored. If TraceIDs
// differ across the batch or all are empty, lastTraceID is cleared.
func (w *BaseWatcher) setLastTrace(
	events []changestream.ChangeEvent,
) {
	if len(events) == 0 {
		return
	}
	var traceID, spanID string
	for _, e := range events {
		t := e.TraceID()
		s := e.SpanID()
		if t == "" {
			continue
		}
		if traceID == "" {
			traceID = t
			spanID = s
		} else if traceID != t {
			// Mixed trace IDs — clear and bail.
			w.mu.Lock()
			w.lastTraceID = ""
			w.lastSpanID = ""
			w.mu.Unlock()
			return
		} else {
			// Same trace ID — update to most recent spanID.
			spanID = s
		}
	}
	w.mu.Lock()
	w.lastTraceID = traceID
	w.lastSpanID = spanID
	w.mu.Unlock()
}

// Kill (worker.Worker) kills the watcher via its tomb.
func (w *BaseWatcher) Kill() {
	w.tomb.Kill(nil)
}

// Wait (worker.Worker) waits for the watcher's tomb to die,
// and returns the error with which it was killed.
func (w *BaseWatcher) Wait() error {
	return w.tomb.Wait()
}

// Mapper is a function that maps a slice of change events to another slice
// of change events. This allows modification or dropping of events if
// necessary. When zero events returned, no change will be emitted.
// The inverse is also possible, allowing fake events to be added to the stream.
type Mapper func(context.Context, []changestream.ChangeEvent) ([]string, error)

// defaultMapper is the default mapper used by the watchers.
// It will always return the same change events, allowing all events to be sent.
func defaultMapper(
	_ context.Context, events []changestream.ChangeEvent,
) ([]string, error) {
	return transform.Slice(events, func(c changestream.ChangeEvent) string {
		return c.Changed()
	}), nil
}

// FilterEvents drops events that do not match the filter.
func FilterEvents(filter func(changestream.ChangeEvent) bool) Mapper {
	return func(
		_ context.Context, events []changestream.ChangeEvent,
	) ([]string, error) {
		var filtered []string
		for _, event := range events {
			if filter(event) {
				filtered = append(filtered, event.Changed())
			}
		}
		return filtered, nil
	}
}

// Query is a function that returns the initial state of a watcher.
type Query[T any] func(context.Context, database.TxnRunner) (T, error)
