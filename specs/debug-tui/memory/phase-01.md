# Phase 01 — Memory File

## LogAPI Interface

```go
type LogAPI interface {
    StreamLogs(ctx context.Context, params common.DebugLogParams) (<-chan common.LogMessage, error)
    Close() error
}
```

Wire counterpart: `common.StreamDebugLog(ctx, conn, params)` which opens a
WebSocket to `/log` and returns a `<-chan common.LogMessage`. The concrete
`logAPIClient` wraps an `api.Connection`.

`Close()` closes the underlying `api.Connection`, consistent with how
`debug-log` works.

## Severity Level Cycle Order

TRACE → DEBUG → INFO → WARNING → ERROR

Mapped to `loggo.Level` values: 1 → 2 → 3 → 4 → 5.

Level changes cancel the current stream context, increment `streamVersion`,
clear records, and start a new stream with updated `DebugLogParams.Level`.

## Module Filter UX

- `/` key activates `bubbles/textinput` inline in the border title.
- `Enter` applies the filter (sets `moduleFilter`); `Esc` cancels.
- Module filtering is client-side substring matching (case-insensitive) on
  the `Module` field. It does **not** restart the stream.
- Border title format:
  - No filter: `Log [INFO]`
  - Filter active or input focused: `Log [INFO] filter: <textinput>`

## Stream Lifecycle

1. `startLogStreamCmd` creates a `context.WithCancel`, calls
   `logAPI.StreamLogs`, returns `logStreamReadyMsg` with channel + cancel.
2. On `logStreamReadyMsg`, model stores `logCh` and `streamCancel`,
   returns `waitForLogMsg(ch, version)`.
3. `waitForLogMsg` blocks on the channel. On receive: returns `logMsg`.
   On channel close: returns `logStreamDoneMsg`.
4. On `logMsg`, append record, re-render viewport, return next
   `waitForLogMsg`. Stale messages (version mismatch) are discarded.
5. On `l` key: cancel old stream, increment `streamVersion`, clear records,
   start new stream.

## Files Created

- `cmd/juju/debug/api.go` — `LogAPI` interface and `logAPIClient` concrete
  implementation

## Files Modified

- `cmd/juju/debug/messages.go` — Added `logMsg`, `logStreamReadyMsg`,
  `logStreamDoneMsg` message types; removed `logStreamStartMsg`
- `cmd/juju/debug/log.go` — Replaced mock log data with live streaming from
  LogAPI; added level cycling with stream restart; added module filter text
  input; added stream version tracking; added disconnected/error states
- `cmd/juju/debug/model.go` — Updated `newDebugModel` to accept `LogAPI`;
  routed `l`, `/`, `esc` keys to logModel; added stream cancel on quit
- `cmd/juju/debug/command.go` — Creates `api.Connection` via
  `NewAPIRoot`, wraps in `logAPIClient`, passes to `newDebugModel`;
  removed logAPI.Close() (connection closed by stream cancel)
- `cmd/juju/debug/doc.go` — Updated to document log streaming architecture,
  removed phase-01 TODO for log lines
- `specs/debug-tui/phase-01-debug-logs.md` — Updated with design decisions:
  `common.LogMessage` instead of `params.LogRecord`, client-side module
  filter, stream restart model with version tracking, cancellable contexts

## Deviations from Original Spec

1. **Channel type**: Spec said `<-chan params.LogRecord`; implemented as
   `<-chan common.LogMessage` because that is the actual type returned by
   `common.StreamDebugLog`. `common.LogMessage` carries all needed fields
   (Severity, Module, Entity, Timestamp, Message, ModelUUID, Labels).
2. **Module filter**: Spec was ambiguous about server-side vs client-side.
   Implemented as client-side substring matching to avoid restarting the
   stream on every filter change. Only level changes restart the stream.
3. **Stream restart model**: Not detailed in original spec. Implemented
   with `streamVersion` counter to discard stale messages from cancelled
   streams, and `context.CancelFunc` to cancel old stream contexts.
4. **No separate `LogFilters` type**: Used `common.DebugLogParams` directly
   in the `LogAPI` interface instead of a custom wrapper, since that is
   what `common.StreamDebugLog` already accepts.
5. **Error handling**: Stream errors (`StreamLogs` failure) are displayed
   in the log pane rather than aborting the entire TUI, since other panes
   (changestream, trace) may still work.
