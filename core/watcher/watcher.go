// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package watcher

import (
	"context"

	"github.com/juju/worker/v5"
)

// Watcher defines a worker that emits changes for a given type T.
type Watcher[T any] interface {
	worker.Worker

	// Changes returns a channel of type T, closed when the watcher stops.
	Changes() <-chan T

	// ChangeContext returns a new context derived from parent, enriched
	// with the OTel trace ID and span ID for the last value dispatched
	// on Changes(). If no value has been received yet, or no trace
	// context was captured, parent is returned unchanged.
	ChangeContext(parent context.Context) context.Context
}
