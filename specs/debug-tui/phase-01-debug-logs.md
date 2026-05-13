# Phase 01 — Show Debug Logs

## Goal

Replace the static mock log pane with a live stream from the controller
`Logger` facade, reusing the same streaming and filtering mechanisms as
`juju debug-log`.

## Dependencies

- **Phase 00** — the working TUI skeleton must exist.

## Memory Files to Read

Before writing any code, read:

- `specs/debug-tui/memory/phase-00.md` — file paths created, model
  structure, and any deviations.

## Research Required

Before writing any code, read the following:

- `cmd/juju/commands/debuglog.go` — understand how `debug-log` connects
  to the Logger facade, the `LogFilterSpec`, and how log entries are
  streamed and formatted.
- `api/client/logs.go` or equivalent — the existing log API client used
  by `debug-log`.
- `api/logger/logger.go` — the Logger facade client interface.
- `core/logger` — severity level definitions and color mapping.
- `cmd/juju/debug/log.go` — the existing mock `logModel` from phase 00.

## Scope

### 1. Define `LogAPI` interface in `cmd/juju/debug/api.go`

```go
type LogAPI interface {
    StreamLogs(ctx context.Context, filters LogFilters) (<-chan params.LogRecord, error)
    Close() error
}
```

`LogFilters` wraps the same filter options that `debug-log` uses (level,
module, include/exclude patterns).

### 2. Implement concrete `LogAPI` client in `cmd/juju/debug/api.go`

The concrete client wraps the existing `api/logger` client. It calls
`WatchLoggerAPI` or equivalent, reads from the result channel, and
forwards `params.LogRecord` values on the returned channel.

The client is created in `debugCommand.Run()` and injected into
`debugModel` before the bubbletea program starts.

### 3. Update `logModel` in `cmd/juju/debug/log.go`

Replace the static mock lines with live streaming:

- Add a `logCh <-chan params.LogRecord` field to `logModel`.
- `Init()` returns a `tea.Batch` of commands: one that blocks on
  `logCh` and wraps each received record as a `logMsg`, plus the
  viewport's `Init()`.
- On `logMsg`, format the record using the same severity coloring as
  `debug-log` (`ansiterm` palette), append to the viewport content,
  and scroll to the bottom (tail behavior).
- On `l` key, cycle `levelIndex` through severity levels (TRACE →
  DEBUG → INFO → WARNING → ERROR). Update the `LogFilters` level and
  display the current level in the pane border title (e.g. "Log
  [INFO]").
- On `/` key, enter text-input mode using `bubbles/textinput`. The
  text input value becomes the module filter substring. While in
  text-input mode, all key events go to the text input. `Enter` applies
  the filter; `Esc` cancels and clears the input. The filter value is
  shown in the border title (e.g. "Log [INFO] filter: uniter").
- When the `logCh` channel is closed, show a status line at the top
  of the log pane: "Log stream disconnected."
- When the `LogAPI` constructor returns an error (e.g. unreachable
  controller), the log pane shows the error message instead of hanging.

### 4. Update `debugCommand.Run()` in `cmd/juju/debug/command.go`

- Create the `LogAPI` client before starting the bubbletea program.
- If the client creation fails, print the error to stderr and exit
  (do not start the TUI with a broken log stream).
- Pass the `LogAPI` client (or its channel) into `newDebugModel()`.
- On program exit, call `logAPI.Close()` to tear down the log stream.

### 5. Remove mock log data

Delete the hardcoded mock log lines from `logModel`. The initial state
of the viewport is empty; lines populate as the stream delivers them.

## Memory File

On completion, write `specs/debug-tui/memory/phase-01.md` containing:

- The `LogAPI` interface methods and their wire counterparts.
- The severity level cycle order.
- The filter text-input UX decisions (where the input renders, what
  border title format is used).
- All file paths modified.
- Any deviations from this phase spec and the reason.

## Acceptance Criteria

- Running `juju debug` with a live controller shows real-time log
  entries in the Log pane.
- `l` cycles the log level; filtered-out entries are not displayed.
- `/` activates module filter input; `Enter` applies, `Esc` cancels.
- The pane border title reflects the active level and filter.
- Closing the TUI does not leak goroutines (log stream is closed).
- If the controller is unreachable, the log pane shows an error message
  instead of hanging.
