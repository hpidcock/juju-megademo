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
	// Name is "controller" or the model UUID.
	Name string `json:"name"`
	// TxnMin is the lowest txn_id consumed (0 if already at head).
	TxnMin int64 `json:"txn-min"`
	// TxnMax is the highest txn_id consumed.
	TxnMax int64 `json:"txn-max"`
	// EventCount is the number of change events that became visible.
	EventCount int    `json:"event-count"`
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
