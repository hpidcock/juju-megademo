// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

type dbTxnSeq struct {
	ID int64 `db:"id"`
}

type dbDebugState struct {
	State      string `db:"state"`
	StepTarget int64  `db:"step_target"`
}

type dbTraceInfo struct {
	TraceID string `db:"trace_id"`
	SpanID  string `db:"span_id"`
}
