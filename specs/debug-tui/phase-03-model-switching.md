# Phase 03 — Model Switching

## Goal

Support `--all` and `--controller` flags so the TUI can display and
switch between multiple changestreams (one per database). Each database
gets its own tab in the Changestream pane header.

## Dependencies

- **Phase 00** — the working TUI skeleton must exist.

## Memory Files to Read

Before writing any code, read:

- `specs/debug-tui/memory/phase-00.md` — file paths, model structure,
  deviations.

## Research Required

Before writing any code, read the following:

- `cmd/juju/commands/main.go` — understand flag registration patterns
  for existing commands that use `--all` or `--controller`.
- `cmd/modelcmd/base.go` — understand `ControllerCommandBase`,
  `ModelCommandBase`, and model resolution.
- `cmd/juju/debug/changestream.go` — the existing `changestreamModel`
  from phase 00.
- `api/client/debugchangestream/` (if it exists from debug-changestream
  spec task 08) — the `DebugChangeStream` API client for the `Status`
  method.

## Scope

### 1. Define `DebugChangeStreamAPI` interface in `cmd/juju/debug/api.go`

```go
type StreamStatus struct {
    Name   string
    State  string
    TxnID  int64
}

type DebugChangeStreamAPI interface {
    Status(ctx context.Context, scope DebugScope) ([]StreamStatus, error)
    Close() error
}
```

In this phase, only the `Status` method is called. `Pause`, `Step`, and
`Resume` are added in phase 04.

### 2. Implement concrete `DebugChangeStreamAPI` client

If the `api/client/debugchangestream/` package already exists (from
debug-changestream task 08), wrap it. Otherwise, create a thin client
that calls the `DebugChangeStream` facade's `Status` method.

### 3. Add scope flags to `debugCommand` in `cmd/juju/debug/command.go`

```go
type debugCommand struct {
    modelcmd.ControllerCommandBase
    model      string
    all        bool
    controller bool
}
```

- `SetFlags()` registers:
  - `--model`, `-m` (string, default: current model)
  - `--all` (bool)
  - `--controller` (bool)
- `Init()` validates:
  - `--all` and `--controller` are mutually exclusive
  - `--model` is incompatible with `--all` and `--controller`
  - If none are specified, target the current model

### 4. Define `DebugScope` type in `cmd/juju/debug/api.go`

```go
type DebugScope struct {
    All        bool
    Controller bool
    ModelUUID  string
}
```

The `debugCommand.Init()` resolves the model UUID from the `--model`
flag (or the current model) and populates `DebugScope`.

### 5. Update `changestreamModel` in `cmd/juju/debug/changestream.go`

Add multi-database state:

```go
type tabState struct {
    Name         string
    Status       string
    TxnID        int64
    Transactions []transactionEntry
}

type changestreamModel struct {
    width          int
    height         int
    tabs           []tabState
    activeTab      int
    cursor         int
    currentTxnIdx  int
    api            DebugChangeStreamAPI
    pollInterval   time.Duration
}
```

- `Init()` returns a `tea.Tick(pollInterval, statusTickMsg)` command for
  periodic status polling.
- On `statusTickMsg`, call `api.Status(ctx, scope)` and update each
  tab's `Status` and `TxnID`. Return the next tick command to continue
  polling.
- On `Tab` key, increment `activeTab` (wrap around).
- On `Shift+Tab`, decrement `activeTab` (wrap around).
- On `↑`/`↓`, navigate the transaction list for the active tab.
- On `Enter`, update `currentTxnIdx` and emit `selectTxnMsg`.
- `View()`:
  - Header bar: left "Changestream", centre shortcuts, right tab bar.
  - Tab bar: rendered only when `len(tabs) > 1`. Each tab shows its
    name and status indicator. The active tab is highlighted.
  - Transaction list: shows entries for `tabs[activeTab]`.
  - When a single tab is active and `--all`/`--controller` are not set,
    hide the tab bar.

### 6. Create `cmd/juju/debug/mock_data.go`

For this phase, populate each tab's `Transactions` slice with mock data
(different transactions per tab). This allows testing tab switching
without a live controller.

### 7. Update `debugCommand.Run()` in `cmd/juju/debug/command.go`

- Create the `DebugChangeStreamAPI` client.
- Pass it into `newDebugModel()` along with the `DebugScope`.
- Close the client on program exit.

### 8. Update `debugModel` in `cmd/juju/debug/model.go`

- When a `selectTxnMsg` is received, route it to both `traceModel` (to
  update the trace pane) and `logModel` (future: to highlight related
  log lines, though this is not required in this phase).

## Memory File

On completion, write `specs/debug-tui/memory/phase-03.md` containing:

- The `DebugChangeStreamAPI` interface methods implemented.
- The `DebugScope` type definition.
- The flag validation rules.
- The tab rendering approach (how tabs are styled, how the active tab
  is highlighted).
- The polling interval chosen.
- All file paths modified.
- Any deviations from this phase spec and the reason.

## Acceptance Criteria

- `juju debug` (no flags) shows a single-tab Changestream pane with no
  tab bar.
- `juju debug --all` shows a tab bar with one tab per database
  (controller + models). `Tab`/`Shift+Tab` switch the active tab; the
  transaction list and status update accordingly.
- `juju debug --controller` shows a single tab for the controller.
- `--all` and `--controller` together produce an `Init()` error.
- `--model` combined with `--all` or `--controller` produces an
  `Init()` error.
- When connected to a live controller, the status indicator for each
  tab reflects the real changestream state (RUNNING/PAUSED/STEP),
  updated via polling.
