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
    StreamLogs(ctx context.Context, params common.DebugLogParams) (<-chan common.LogMessage, error)
    Close() error
}
```

The interface uses `common.LogMessage` (not `params.LogRecord`) because that
is the actual client-side type returned by `common.StreamDebugLog`. Each
`LogMessage` carries `Severity` (string), `Module`, `Entity`, `Timestamp`,
`Message`, `ModelUUID`, and `Labels` — everything the log pane needs for
formatting and filtering.

`LogFilters` is not a separate type; the interface accepts
`common.DebugLogParams` directly, which is the same filter structure used by
`debug-log` (level, include/exclude module/entity/labels, backlog, replay,
etc.).

### 2. Implement concrete `LogAPI` client in `cmd/juju/debug/api.go`

The concrete `logAPIClient` wraps an `api.Connection` and delegates to
`common.StreamDebugLog`:

```go
type logAPIClient struct {
    conn api.Connection
}

func (c *logAPIClient) StreamLogs(ctx context.Context, params common.DebugLogParams) (<-chan common.LogMessage, error) {
    return common.StreamDebugLog(ctx, c.conn, params)
}

func (c *logAPIClient) Close() error {
    return c.conn.Close()
}
```

`Close()` closes the underlying `api.Connection`, consistent with how
`debug-log` works. The client is created in `debugCommand.Run()` and injected
into `debugModel` before the bubbletea program starts.

### 3. Update `logModel` in `cmd/juju/debug/log.go`

Replace the static mock lines with live streaming:

- Add a `logAPI LogAPI` field to `logModel`.
- Add a `records []common.LogMessage` field to store received records.
- Add a `filterInput textinput.Model` field for module filter text input.
- Add a `filtering bool` field indicating the text input is active.
- Add a `moduleFilter string` field for the active module filter substring.
- Add a `streamVersion int` field to discard stale messages from old streams.
- Add a `disconnected bool` field set when the log channel closes.
- Add an `initErr string` field for displaying connection errors.
- `Init()` returns a `tea.Batch` of commands: one that calls `startLogStream`
  to establish the stream and return a `waitForLogMsg` command, plus the
  viewport's `Init()`.

**Stream lifecycle (level changes trigger stream restart):**

- `startLogStream` builds `common.DebugLogParams` from the current
  `levelIndex` (mapped to `loggo.Level`), calls `logAPI.StreamLogs`, and
  returns a `waitForLogMsg` command that blocks on the channel.
- On `l` key, increment `levelIndex`, cancel the old stream context by
  incrementing `streamVersion`, clear `records`, and issue a new
  `startLogStream` command. Stale `logMsg` values (whose version does not
  match) are silently discarded.
- On `logMsg`, append the `common.LogMessage` to `records`, format the
  record, append to the viewport content, and scroll to the bottom (tail
  behavior).
- On `logStreamDoneMsg`, set `disconnected = true` and show a status line at
  the top of the log pane: "Log stream disconnected."

**Module filter (client-side substring matching):**

- Module filtering is done entirely client-side by substring matching on
  the `Module` field. It does **not** restart the stream.
- On `/` key, enter text-input mode by setting `filtering = true` and
  focusing the `filterInput`. While in text-input mode, all key events go
  to the text input.
- `Enter` applies the filter: set `moduleFilter` to the input value, set
  `filtering = false`, and re-render the viewport content.
- `Esc` cancels: clear the input, set `filtering = false`, restore the
  previous filter.
- The filter value is shown in the border title (e.g. `Log [INFO] filter:
  uniter`).
- When module filter is active, only records whose `Module` contains the
  `moduleFilter` substring (case-insensitive) are displayed.

**Error handling:**

- When `logAPI.StreamLogs` returns an error, `initErr` is set and the log
  pane shows the error message instead of hanging.

**Border title format:**

- No filter active: `Log [INFO]`
- Filter active: `Log [INFO] filter: uniter`

### 4. Update `debugCommand.Run()` in `cmd/juju/debug/command.go`

- Create the `LogAPI` client before starting the bubbletea program:
  - Call `c.NewAPIRoot(ctx)` to get an `api.Connection`.
  - Wrap it in `logAPIClient`.
- If the client creation fails, print the error to stderr and exit (do not
  start the TUI with a broken log stream).
- Pass the `LogAPI` client into `newDebugModel()`.
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
