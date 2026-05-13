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
// Log actions (l, /) go to logModel. The m key triggers model switching
// (phase 3). The ? key toggles the help overlay.
//
// Sub-models communicate through message types defined in messages.go. When
// the user selects a transaction in the changestream pane (Enter key),
// changestreamModel emits a selectTxnMsg. The top-level Update handles this
// message by calling traceModel.setTransaction to update the trace pane.
//
// # Changestream refresh cycle
//
// The changestreamModel drives a periodic tick (changestreamTickMsg) every
// 500ms. On each tick, if the changestream is not paused, the model replaces
// its transaction list with a fresh set (capped at 10 entries). This ensures
// the TUI stays up-to-date with the stream of new transactions. When paused,
// ticks are ignored and the transaction list is frozen.
//
// # Mock data and phased implementation
//
// The current implementation uses mock data throughout. Each mock is annotated
// with a TODO comment referencing the phase that replaces it:
//
//   - changestream transactions  -> phase-04 (DebugChangeStream facade)
//   - pause/resume/step actions  -> phase-04 (DebugChangeStream facade)
//   - pause-all (P key)          -> phase-03 (multi-model support)
//   - step-N (S key)             -> phase-04 (DebugChangeStream.Step)
//   - log lines                  -> phase-01 (Logger facade)
//   - module filter (/ key)      -> phase-01 (Logger facade)
//   - trace spans                -> phase-02 (Grafana Tempo)
//   - model switching (m key)    -> phase-03 (model list + switch)
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
//	The changestream pane has two states:
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
