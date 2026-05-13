// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package params

// DebugChangeStreamTarget selects which database(s) to target.
type DebugChangeStreamTarget struct {
	// ModelUUID targets a specific model. Mutually exclusive with
	// Controller and All.
	ModelUUID string `json:"model-uuid,omitempty"`
	// Controller targets only the controller database.
	Controller bool `json:"controller,omitempty"`
	// All targets all model databases and the controller database.
	All bool `json:"all,omitempty"`
}

// DebugChangeStreamArgs holds arguments for Pause and Resume.
type DebugChangeStreamArgs struct {
	Target DebugChangeStreamTarget `json:"target"`
}

// DebugChangeStreamStepArgs holds arguments for Step.
type DebugChangeStreamStepArgs struct {
	Target DebugChangeStreamTarget `json:"target"`
	Count  int                     `json:"count"`
}

// DebugChangeStreamDBResult holds the result for one database.
type DebugChangeStreamDBResult struct {
	Name       string `json:"name"`
	TxnMin     int64  `json:"txn-min"`
	TxnMax     int64  `json:"txn-max"`
	EventCount int    `json:"event-count"`
	TraceID    string `json:"trace-id,omitempty"`
	SpanID     string `json:"span-id,omitempty"`
	Error      *Error `json:"error,omitempty"`
}

// DebugChangeStreamPauseResult holds the results for a Pause call.
type DebugChangeStreamPauseResult struct {
	Results []DebugChangeStreamDBResult `json:"results"`
}

// DebugChangeStreamStepResult holds the results for a Step call.
type DebugChangeStreamStepResult struct {
	Results []DebugChangeStreamDBResult `json:"results"`
}

// DebugChangeStreamDBStatus holds the status for one database.
type DebugChangeStreamDBStatus struct {
	Name  string `json:"name"`
	State string `json:"state"`
	TxnID int64  `json:"txn-id"`
	Error *Error `json:"error,omitempty"`
}

// DebugChangeStreamStatusResult holds the results for a Status call.
type DebugChangeStreamStatusResult struct {
	Results []DebugChangeStreamDBStatus `json:"results"`
}
