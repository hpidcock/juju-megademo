// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package debug implements the `juju debug` TUI command -- an interactive
// terminal UI for inspecting and controlling the Juju changestream.
//
// The debug TUI presents a context bar and three panes: a changestream pane
// showing transaction history with pause/step/resume controls, a log pane
// streaming controller log entries, and a trace pane displaying OpenTelemetry
// span details for a selected transaction.
//
// # Architecture
//
// The TUI is built on the bubbletea Elm-architecture framework. The top-level
// debugModel composes three sub-models -- changestreamModel, logModel, and
// traceModel -- each implementing the bubbletea Model interface independently.
//
// Key routing: the top-level Update dispatches key events to the relevant
// sub-model. Changestream actions (p, P, s, S, r) go to changestreamModel.
// Log actions (l, /) go to logModel. The m key opens the model-picker
// overlay. The ? key toggles the help overlay.
//
// Sub-models communicate through message types defined in messages.go. When
// the user selects a transaction in the changestream pane (Enter key),
// changestreamModel emits a selectTxnMsg. The top-level Update handles this
// message by calling traceModel.setTransaction to update the trace pane.
//
// When the user switches models (m key, then Enter in the picker),
// changestreamModel emits a switchModelMsg. The top-level Update handles
// this by updating the context bar, restarting the log stream for the new
// model, and resetting the trace pane.
//
// # Multi-model support
//
// The TUI tracks per-model state in a map keyed by model UUID. Each model
// has its own pause/resume state, transaction list, and cursor position.
// The currently displayed model is identified by currentModel (a UUID).
//
// The m key opens a model-picker overlay that lists all models on the
// controller. The user navigates with ↑/↓ and selects with Enter. The
// picker is rendered as an overlay on top of the normal TUI layout.
//
// Pause/resume semantics:
//   - p pauses only the current model
//   - P pauses all models
//   - r resumes only the current model
//   - s steps only the current model
//   - On quit, all paused models are automatically resumed
//
// # Log streaming
//
// The logModel streams live log entries from the controller via the LogAPI
// interface, which wraps common.StreamDebugLog. On startup, a
// startLogStreamCmd opens a WebSocket to /log with the current level filter.
// Each received common.LogMessage is forwarded as a logMsg. When the channel
// closes, a logStreamDoneMsg sets the disconnected state.
//
// Changing the log level (l key) cancels the current stream, increments
// streamVersion to discard stale messages, and starts a new stream with
// the updated level. Module filtering (/ key) is client-side substring
// matching on the Module field and does not restart the stream.
//
// When the model is switched, the log stream is restarted for the new model.
//
// # Changestream refresh cycle
//
// The changestreamModel polls the DebugChangeStreamAPI.Status method on a
// timed interval (statusTickMsg, every 500ms) to refresh the state of all
// models. The transaction list starts empty and is populated exclusively by
// step results; it is not replaced on status polls.
//
// # Phased implementation
//
// The current implementation uses mock data in some areas. Each mock is
// annotated with a TODO comment referencing the phase that replaces it:
//
//   - trace spans                -> phase-02 (Grafana Tempo)
//
// See specs/debug-tui/README.md for the full phase index and dependency graph.
//
// # Layout
//
//	The terminal is divided vertically:
//
//	Context bar  (1 row): controller name, model name, [m]odel [q]uit
//	Changestream (~40%): transaction list with pause/step/resume
//	Log          (~35%): scrollable log output with severity colors
//	Trace        (~25%): span details for the selected transaction
//
// # State machine
//
//	The changestream pane has two states per model:
//
//	 +----------+   p   +--------+
//	 | running  | ----> | paused |
//	 +----------+       +--------+
//	   ^  ^               |  ^
//	   |  |   r           |  |
//	   |  +---------------+  |
//	   +---------------------+
//	              s (step)
//
//	Running: transaction list refreshes every tick. No bullet dot shown.
//	Paused:  transaction list is frozen. Bullet dot marks paused position.
//	         s advances the dot; r returns to running.
package debug
