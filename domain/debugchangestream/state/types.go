// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

// dbTxnSeq maps the change_log_txn_seq table.
type dbTxnSeq struct {
	ID int64 `db:"id"`
}

// dbDebugState maps the debug_change_stream table columns used
// by read and write operations.
type dbDebugState struct {
	State      string `db:"state"`
	StepTarget int64  `db:"step_target"`
}
