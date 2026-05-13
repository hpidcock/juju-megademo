// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package debugchangestream provides pause, step, and resume control
// over a single Juju database's changestream for debugging purposes.
// The domain exposes operations that allow an operator to halt the
// changestream, advance it one transaction batch at a time, and then
// resume normal operation.
package debugchangestream
