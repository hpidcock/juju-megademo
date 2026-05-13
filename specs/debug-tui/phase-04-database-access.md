# Phase 04 — Database Access for Transaction List

## Goal

Replace the mock transaction list with real data fetched from the
`DebugChangeStream` facade. Wire the `s`, `S`, `p`, and `r` keybindings
to real facade calls that pause, step through, and resume the
changestream.

## Dependencies

- **Phase 01** — the log pane must be streaming so worker reactions to
  stepped transactions are visible.
- **Phase 02** — the trace pane must be ready so real trace IDs from
  the transaction list are resolved.
- **Phase 03** — multi-model tab state must exist so facade calls target
  the correct database.

## Memory Files to Read

Before writing any code, read:

- `specs/debug-tui/memory/phase-00.md` — base model structure.
- `specs/debug-tui/memory/phase-01.md` — LogAPI interface and UX.
- `specs/debug-tui/memory/phase-02.md` — TempoAPI interface and cache.
- `specs/debug-tui/memory/phase-03.md` — DebugChangeStreamAPI
  interface, scope, tab structure, polling.

## Research Required

Before writing any code, read the following:

- `specs/debug-changestream.md` — the full technical design for the
  `DebugChangeStream` facade, including `StepResult` and `StreamStatus`
  wire types.
- `rpc/params/debugchangestream.go` or equivalent — the wire types for
  the facade (from debug-changestream task 08).
- `api/client/debugchangestream/` — the API client package.
- `cmd/juju/debug/changestream.go` — the current `changestreamModel`
  with mock transaction data.
- `cmd/juju/debug/api.go` — the current `DebugChangeStreamAPI`
  interface (only `Status` from phase 03).

## Scope

### 1. Extend `DebugChangeStreamAPI` interface in `cmd/juju/debug/api.go`

Add the remaining facade methods:

```go
type StepResult struct {
    TxnMin     int64
    TxnMax     int64
    EventCount int
    TraceID    string
    SpanID     string
}

type DebugChangeStreamAPI interface {
    Pause(ctx context.Context, scope DebugScope) error
    Step(ctx context.Context, scope DebugScope, count int) ([]StepResult, error)
    Resume(ctx context.Context, scope DebugScope) error
    Status(ctx context.Context, scope DebugScope) ([]StreamStatus, error)
    Close() error
}
```

### 2. Implement the extended `DebugChangeStreamAPI` client

Add `Pause`, `Step`, and `Resume` methods to the concrete client in
`api/client/debugchangestream/`. Each method calls the corresponding
facade RPC and returns the result.

### 3. Update `changestreamModel` in `cmd/juju/debug/changestream.go`

Replace mock transaction data with facade-driven data:

- On `s` key:
  1. Call `api.Step(ctx, scope, 1)`.
  2. On success, convert each `StepResult` to a `transactionEntry` and
     prepend to the active tab's `Transactions`.
  3. Set `currentTxnIdx = 0` so the `●` dot marks the newest entry.
  4. Scroll the list to show the new entry.
  5. Emit `selectTxnMsg` so the Trace pane fetches the real trace.
  6. On failure, display the error in the Changestream pane header
     (e.g. `"Step failed: <error>"`) and do not modify state.

- On `S` key:
  1. Activate a text input (using `bubbles/textinput`) prompting for a
     count. While active, all keys go to the text input.
  2. On `Enter`, parse the integer. If invalid, show error in the
     header and return to normal mode.
  3. Call `api.Step(ctx, scope, count)`.
  4. Process results the same as `s` above.

- On `p` key:
  1. Call `api.Pause(ctx, scope)`.
  2. On success, update the active tab's `Status` to `"PAUSED"`.
  3. On failure, display the error in the header.

- On `r` key:
  1. Call `api.Resume(ctx, scope)`.
  2. On success, update the active tab's `Status` to `"RUNNING"`.
  3. On failure, display the error in the header.

- On `statusTickMsg` (periodic polling from phase 03):
  1. Call `api.Status(ctx, scope)`.
  2. Update each tab's `Status` and `TxnID` from the response.
  3. Do **not** replace the transaction list — only the status fields.
     The transaction list is populated exclusively by step results.

### 4. Remove mock transaction data

Delete `cmd/juju/debug/mock_data.go` and any hardcoded transaction
entries. The initial state of each tab's transaction list is empty; it
populates as the user steps through the changestream.

### 5. Update `traceModel` in `cmd/juju/debug/trace.go`

When a `selectTxnMsg` arrives with a `StepResult` containing a non-empty
`TraceID`, the trace pane should attempt to fetch spans from the
`TempoAPI` (from phase 02). The `TraceID` and `SpanID` from the
`StepResult` are used for display and for the Tempo query.

### 6. Update `debugModel` graceful shutdown in `cmd/juju/debug/model.go`

On quit, before exiting:
- For each tab where `Status` is `"PAUSED"` or `"STEP"`, call
  `api.Resume(ctx, scope)`.
- This ensures the system is never left in a paused state when the TUI
  exits.
- If the resume call fails, log the error to stderr (best-effort).

## Memory File

On completion, write `specs/debug-tui/memory/phase-04.md` containing:

- The full `DebugChangeStreamAPI` interface as implemented.
- The `StepResult` type and its mapping to `transactionEntry`.
- The error display strategy (where, how long, when cleared).
- The graceful shutdown logic (which tabs are auto-resumed, error
  handling).
- All file paths modified.
- Any deviations from this phase spec and the reason.

## Acceptance Criteria

- `s` steps forward 1 transaction; the new entry appears at the top of
  the transaction list with the `●` dot.
- `S` prompts for a count; stepping N transactions shows N new entries.
- `p` pauses the changestream; the status indicator changes to PAUSED.
- `r` resumes the changestream; the status indicator changes to RUNNING.
- If a facade call fails, the error appears in the Changestream pane
  header and state is unchanged.
- Quitting the TUI while paused automatically resumes the changestream.
- The transaction list starts empty and is populated only by step
  actions.
- Trace pane displays real trace IDs from step results; if Tempo is
  configured, spans are fetched and rendered.
