# Debug TUI: `juju debug`

## Status

Draft — awaiting review.

## Summary

Add a new `juju debug` command that launches an interactive terminal UI (TUI)
for inspecting and controlling the Juju changestream. The TUI presents three
panes — changestream status and controls, log tail, and trace inspection — and
allows a developer to pause, step through, and resume the changestream
interactively using keyboard shortcuts.

The command replaces the need to run `juju debug-pause`, `juju debug-step`,
and `juju debug-resume` in separate terminals while also providing a unified
view of logs and traces alongside changestream state.

## Motivation

The `juju debug-pause`, `debug-step`, and `debug-resume` commands (defined in
`specs/debug-changestream.md`) provide fine-grained control over the
changestream, but they require the developer to:

1. Run commands in separate terminals to control the stream.
2. Run `juju debug-log` in another terminal to see worker reactions.
3. Cross-reference trace IDs manually in an external trace backend.

A TUI that combines all three into a single interactive session reduces
cognitive overhead and makes the debug workflow significantly faster.

## Command Registration

`juju debug` is registered as a **flat subcommand** on the root `juju`
SuperCommand, consistent with the existing `debug-log`, `debug-hooks`, and
`debug-code` commands.

```go
// In cmd/juju/commands/main.go, registerCommands()
r.Register(newDebugCommand())
```

The command embeds `modelcmd.ControllerCommandBase` (not
`ModelCommandBase`) because it always requires a controller connection.

### Flags

| Flag | Type | Default | Meaning |
|------|------|---------|---------|
| `--model`, `-m` | string | current model | Target model's changestream. Ignored when `--all` is set. |
| `--all` | bool | false | Show all model changestreams + controller changestream. |
| `--controller` | bool | false | Show only the controller changestream. |

When no scope flags are given, the current model is targeted (same semantics
as `debug-pause`/`debug-step`/`debug-resume`).

### Access control

Only controller superusers may run this command, matching the existing
`debug-pause`/`debug-step`/`debug-resume` restriction.

## TUI Layout

The terminal is divided into three panes:

```
┌──────────────────────────────────────────────────────────┐
│ Changestream Pane   [s]tep [r]esume [q]uit  [model tabs] │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Transaction List                                    │ │
│  │ ● 42  3 events  trace: abc…                         │ │
│  │ 41  1 event   trace: def…                           │ │
│  │ 40  5 events  trace: ghi…                           │ │
│  └─────────────────────────────────────────────────────┘ │
├──────────────────────────────────────────────────────────┤
│ Log Pane                                                 │
│ 10:42:01 INFO juju.worker.uniter … handling configure    │
│ 10:42:01 DEBUG juju.changestream … dispatching term 42   │
│ 10:42:00 INFO juju.worker.caas … starting                │
├──────────────────────────────────────────────────────────┤
│ Trace Pane                                               │
│ txn 42 — trace abc123 — span def456                      │
│   → juju:API.AddRelation    1.2ms                        │
│   → juju:changestream.write 0.3ms                        │
│   → juju:worker.uniter      0.8ms                        │
└──────────────────────────────────────────────────────────┘
```

### Changestream Pane (top)

This pane is the primary interaction surface. It consists of a header
bar and a full-width transaction list:

**Header bar**
- Left: pane title ("Changestream Pane").
- Centre: available shortcuts (`[s]tep [r]esume [q]uit`).
- Right: model/controller tab bar when `--all` is active.

**Transaction List**
- Full-width scrollable list of recent transactions, newest at top.
- Each row shows: `txn_id`, event count, and truncated trace ID.
- A `●` dot marks the current (most recently stepped) transaction.
- The selected/highlighted transaction populates the Trace Pane below.
- When stepping, new transactions appear at the top as they are
  consumed and the dot moves to the newest entry.

When `--all` is active, the tab bar in the header allows switching
between databases. The transaction list updates to show entries for the
currently selected tab (keybindings: `1`, `2`, `3`, … or `Shift+Tab` /
`Tab`).

### Log Pane (middle)

Streams log entries using the same filtering and coloring logic as
`juju debug-log`. Keybindings allow changing the log level and module
filter without leaving the TUI.

### Trace Pane (bottom)

Displays the OTel trace and span hierarchy for the transaction currently
selected in the Transaction List. When no transaction is selected (or the
selected transaction has no trace context), the pane shows a placeholder
message.

The trace view shows:
- Trace ID and span ID from the `change_log` row.
- If the trace backend is reachable (future work), child spans fetched
  via the OTel API. Initially, only the trace ID and span ID stored in
  `change_log` are displayed.

## Keybindings

| Key | Context | Action |
|-----|---------|--------|
| `s` | Changestream | Step forward 1 transaction (calls `debug-step` facade) |
| `S` | Changestream | Step forward N transactions (prompts for N) |
| `p` | Changestream | Pause the changestream (calls `debug-pause` facade) |
| `r` | Changestream | Resume the changestream (calls `debug-resume` facade) |
| `Tab` | Changestream (multi-model) | Switch to next model/controller tab |
| `Shift+Tab` | Changestream (multi-model) | Switch to previous tab |
| `↑` / `↓` | Transaction List | Navigate transaction list |
| `Enter` | Transaction List | Select transaction for Trace Pane |
| `l` | Log Pane | Cycle log level (TRACE → DEBUG → INFO → WARNING → ERROR) |
| `/` | Log Pane | Filter by module (text input) |
| `Esc` | Any filter input | Cancel/clear filter input |
| `q` | Global | Quit the TUI |
| `?` | Global | Show help overlay with all keybindings |

All changestream actions (`s`, `S`, `p`, `r`) target the currently
selected model/controller tab. When a single model is targeted (no `--all`),
those keys target that model directly.

## TUI Architecture

### Framework: charmbracelet/bubbletea

The TUI is built using the
[bubbletea](https://github.com/charmbracelet/bubbletea) Elm-architecture
framework with supporting packages:

| Package | Purpose |
|---------|---------|
| `github.com/charmbracelet/bubbletea` | Core TUI framework (Model/Update/View) |
| `github.com/charmbracelet/lipgloss` | Terminal styling, layout, borders |
| `github.com/charmbracelet/bubbles` | Pre-built components (viewport, list, spinner, text input) |

These are new dependencies and will be added to `go.mod`.

### Model composition

The top-level `debugModel` composes three sub-models:

```go
type debugModel struct {
    width          int
    height         int
    activePane     pane

    changestream   changestreamModel
    log            logModel
    trace          traceModel

    api            DebugChangeStreamAPI
    logAPI         LogAPI
    quitting       bool
}
```

Each sub-model implements the bubbletea `Model` interface independently. The
top-level model delegates `Init()`, `Update()`, and `View()` calls to the
active pane, while non-active panes receive only size-change messages.

### Sub-models

**`changestreamModel`**
- Maintains per-database state: name, status, txn_id, step_target.
- Polls the `DebugChangeStream` facade on a timed interval (e.g. 500ms)
  to refresh status. This mirrors the polling that the stream worker
  itself performs.
- On `s`/`p`/`r` keypress, calls the corresponding facade method and
  immediately refreshes state.
- Manages the scrollable transaction list. Each step response from the
  facade includes the event count and txn_id range, which is prepended
  to the list.
- Tracks the current transaction (most recently stepped) and marks it
  with a `●` indicator in the transaction list.

**`logModel`**
- Wraps `bubbles/viewport` for scrollable log output.
- Connects to the controller log API (same as `debug-log`) and streams
  log entries.
- Supports filtering by level and module string, updated via keybindings.
- Uses `juju/ansiterm` for severity coloring (same palette as `debug-log`).

**`traceModel`**
- Displays trace/span info for the selected transaction.
- Updated when the user selects a transaction in the changestream pane.
- Initially read-only display of `trace_id` and `span_id` from the
  `change_log` row. Future work may integrate with an OTel collector API.

### Message types

```go
type statusTickMsg time.Time
type logMsg params.LogRecord
type stepResultMsg StepResult
type paneResizeMsg struct {
    width  int
    height int
}
```

## API Client

The TUI communicates with two facades:

1. **`DebugChangeStream` facade** (from `specs/debug-changestream.md`):
   - `Pause(scope)` — blocks until paused
   - `Step(scope, count)` — returns `[]StepResult`
   - `Resume(scope)` — blocks until resumed
   - `Status(scope)` — returns current state + txn_id per database

2. **`Logger` facade** (existing, used by `debug-log`):
   - `WatchLoggerAPI()` — streaming log entries

A new internal API interface is defined in the command package for
testability:

```go
type DebugChangeStreamAPI interface {
    Pause(ctx context.Context, scope DebugScope) error
    Step(ctx context.Context, scope DebugScope, count int) ([]StepResult, error)
    Resume(ctx context.Context, scope DebugScope) error
    Status(ctx context.Context, scope DebugScope) ([]StreamStatus, error)
}

type LogAPI interface {
    StreamLogs(ctx context.Context, filters LogFilters) (<-chan params.LogRecord, error)
}
```

Concrete implementations call the Juju API server via the standard
`base.APICaller`.

## Multi-Model Layout

When `--all` is passed, the Changestream Pane shows a tab bar across the top:

```
 [controller] [model:foo] [model:bar]*
```

The asterisk (`*`) marks the active tab. Each tab corresponds to one
database's changestream. Switching tabs with `Tab`/`Shift+Tab` updates:
- The transaction list
- The target for `s`/`p`/`r` actions

The `Status` facade call returns an array of `StreamStatus` (one per
database), so a single call refreshes all tabs.

## Graceful Shutdown

On `q` or terminal disconnect, the TUI:

1. Calls `Resume` for any changestreams that are in `paused` or `step`
   state (for the databases that this TUI session paused).
2. Closes the log stream.
3. Restores the terminal to its original state (bubbletea handles this
   via `tea.ExitAltScreen`).

This ensures the system is never left in a paused state if the developer
closes the TUI.

## Testing

This spec intentionally does not include any test requirements. Agents
implementing these changes should **not** write unit tests, integration
tests, or any other automated tests. The implementation tasks are limited
to the production code only.

## Dependencies

New `go.mod` entries:

| Module | Version |
|--------|---------|
| `github.com/charmbracelet/bubbletea` | v1.x |
| `github.com/charmbracelet/lipgloss` | v1.x |
| `github.com/charmbracelet/bubbles` | v0.x |

These are the only new external dependencies. All other functionality
(build tags, terminal detection, ANSI styling) uses existing juju
libraries.

## Implementation Plan

The implementation is structured as incremental phases, each described
in its own file under `specs/debug-tui/`. See
[specs/debug-tui/README.md](debug-tui/README.md) for the phase index,
dependency graph, and execution order.

| Phase | File | Description |
|-------|------|-------------|
| 0 | [phase-00-working-tui.md](debug-tui/phase-00-working-tui.md) | Working command with bubbletea, mock data |
| 1 | [phase-01-debug-logs.md](debug-tui/phase-01-debug-logs.md) | Live log streaming from Logger facade |
| 2 | [phase-02-grafana-tempo.md](debug-tui/phase-02-grafana-tempo.md) | Real spans from Grafana Tempo |
| 3 | [phase-03-model-switching.md](debug-tui/phase-03-model-switching.md) | `--all` / `--controller` tab switching |
| 4 | [phase-04-database-access.md](debug-tui/phase-04-database-access.md) | Real transaction list via facade |
| 5 | [phase-05-workers-popup.md](debug-tui/phase-05-workers-popup.md) | Worker popup window |
