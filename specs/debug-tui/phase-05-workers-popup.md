# Phase 05 — Workers Related to a Transaction: Popup Window

## Goal

Show which workers reacted to a given transaction in a popup/overlay
window. The user selects a transaction, presses `w`, and an overlay
lists all workers that consumed changes from that transaction along
with their subscription namespaces and completion status.

## Dependencies

- **Phase 04** — real transaction data and facade calls must be
  available so the worker information has a meaningful context.

## Memory Files to Read

Before writing any code, read:

- `specs/debug-tui/memory/phase-03.md` — tab structure and
  `DebugChangeStreamAPI` interface.
- `specs/debug-tui/memory/phase-04.md` — full `DebugChangeStreamAPI`
  interface, `StepResult`, transaction list structure.

## Research Required

Before writing any code, read the following:

- `internal/changestream/eventmultiplexer/multiplexer.go` — understand
  how the `EventMultiplexer` tracks subscriptions per namespace and
  dispatches changes to workers. Identify what data is available to
  report "which workers reacted to txn X".
- `specs/debug-changestream.md` — the specification section on the
  `DebugChangeStream` facade, to determine whether a `WorkersForTxn`
  method already exists or must be proposed.
- `cmd/juju/debug/changestream.go` — the current `changestreamModel`
  with keybinding routing.
- `cmd/juju/debug/model.go` — the top-level `debugModel` with overlay
  rendering logic (from phase 00's help overlay).

## Scope

### 1. Extend `DebugChangeStreamAPI` interface in `cmd/juju/debug/api.go`

Add a new method:

```go
type WorkerInfo struct {
    Name       string
    Namespaces []string
    Completed  bool
}

type DebugChangeStreamAPI interface {
    // ... existing methods from phase 04 ...

    WorkersForTxn(ctx context.Context, scope DebugScope, txnID int64) ([]WorkerInfo, error)
}
```

### 2. Implement `WorkersForTxn` in the API client

If the facade does not yet expose this method (it is not part of the
original debug-changestream spec), implement the client method to call
it anyway. The facade side will return an
`errors.NotSupported` or equivalent when the method is not available on
the target controller version. The TUI handles this gracefully.

If the method is available, parse the response into `[]WorkerInfo`.

### 3. Create `cmd/juju/debug/workers.go`

```go
type workersPopupModel struct {
    width     int
    height    int
    visible   bool
    fetching  bool
    workers   []WorkerInfo
    txnID     int64
    errMsg    string
}
```

- `Init()` returns nil.
- `Update()`:
  - On `showWorkersMsg{txnID}`, set `visible = true`, `fetching = true`,
    `txnID = txnID`, and return a `fetchWorkersCmd` that calls
    `api.WorkersForTxn`.
  - On `workersResultMsg`, set `fetching = false`, populate `workers`
    or `errMsg`.
  - On `Esc` or `q` (when visible), set `visible = false` and clear
    state.
- `View()`:
  - If not visible, return empty string.
  - If fetching, render a bordered centered overlay with a spinner and
    text `"Fetching workers for txn <id>…"`.
  - On success, render a bordered centered overlay listing each worker:
    - `✔ <name>  <ns1>,<ns2>  completed` (green check, namespaces
      comma-separated)
    - `⏳ <name>  <ns1>  in progress` (yellow hourglass)
  - On error (including not-supported), render the overlay with the
    error message:
    - `"Worker information not available for this controller version"`
    - or the specific error string.
  - The overlay is sized relative to the terminal (e.g. 60% width, 50%
    height), centered, with a title: `"Workers for txn <id>"`.
  - Footer line: `"[Esc] close"`.

### 4. Update `changestreamModel` in `cmd/juju/debug/changestream.go`

- On `w` key: if a transaction is selected at the cursor, emit a
  `showWorkersMsg{txnID: selectedTxnID}`. If no transaction is selected,
  do nothing.

### 5. Update `debugModel` in `cmd/juju/debug/model.go`

- Add `workers workersPopupModel` field.
- In `Update()`, route `showWorkersMsg` and `workersResultMsg` to the
  `workersPopupModel`.
- In `View()`, after rendering the 3-pane layout, if
  `workers.visible == true`, overlay the popup on top using lipgloss
  `Place()` with centering.
- While the popup is visible, only `Esc` and `q` key events are routed
  to the popup; all other keys are consumed (ignored) so the user does
  not accidentally trigger changestream actions behind the overlay.

### 6. Add `showWorkersMsg` and `workersResultMsg` to `cmd/juju/debug/messages.go`

```go
type showWorkersMsg struct {
    txnID int64
}

type workersResultMsg struct {
    workers []WorkerInfo
    err     error
}
```

## Memory File

On completion, write `specs/debug-tui/memory/phase-05.md` containing:

- The `WorkersForTxn` method signature and wire type.
- The popup sizing and positioning strategy.
- The not-supported fallback UX.
- How key routing works while the popup is visible.
- All file paths modified.
- Any deviations from this phase spec and the reason.

## Acceptance Criteria

- Pressing `w` with a transaction selected opens the workers popup.
- The popup shows a spinner while fetching, then lists workers with
  their namespaces and completion status.
- If the controller does not support `WorkersForTxn`, the popup shows a
  clear version-compatibility message.
- `Esc` or `q` closes the popup and returns to the normal 3-pane view.
- While the popup is visible, other keybindings (`s`, `p`, `r`, etc.)
  are inert.
- The popup is centered and sized proportionally to the terminal.
