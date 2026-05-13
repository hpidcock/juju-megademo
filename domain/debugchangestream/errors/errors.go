// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package errors defines sentinel errors for the debugchangestream
// domain.
package errors

import "github.com/juju/juju/internal/errors"

var (
	// ErrNotPaused is returned when a step or resume is requested
	// but the changestream is not currently paused.
	ErrNotPaused = errors.New("changestream is not paused")

	// ErrAlreadyPaused is returned when a pause is requested but
	// the changestream is already paused or in step mode.
	ErrAlreadyPaused = errors.New("changestream is already paused")
)
