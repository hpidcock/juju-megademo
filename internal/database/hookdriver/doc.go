// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package hookdriver provides a database/sql/driver shim that intercepts
// write SQL statements within a transaction and invokes Setup and Finalise
// hooks. Setup is called once before the first write statement is executed.
// Finalise is called before Commit when at least one write occurred.
//
// This eliminates the need for a separate read-only probe transaction to
// detect writes; statement classification is done by inspecting the SQL
// keyword at the start of each statement.
package hookdriver
