# Phase 03 — Model Switching

## Goal

Allow the user to switch between models inside the TUI using the `m`
key. When `m` is pressed, a model-picker overlay lists all models
available on the controller. Selecting a model switches the changestream
and log panes to that model. Each model retains its own pause/resume
state independently. `P` pauses all models at once; `r` resumes the
currently selected model. On quit, all paused models are resumed.

There are **no CLI flags** for scope — `juju debug` takes no arguments.
Everything is handled via keyboard shortcuts.

## Dependencies

- **Phase 00** — the working TUI skeleton must exist.
- **Phase 01** — live log streaming must work (model switch restarts the
  log stream for the new model).

## Memory Files to Read

Before writing any code, read:

- `specs/debug-tui/memory/phase-00.md` — file paths, model structure,
  deviations.

## Research Required

Before writing any code, read the following:

- `cmd/juju/debug/changestream.go` — the existing `changestreamModel`
  from phase 00.
- `cmd/modelcmd/controller.go` — understand `ControllerCommandBase`
  and how to resolve model UUIDs.
- `jujuclient/clientstore.go` — how to list models for a controller.

## Scope

### 1. Define `DebugChangeStreamAPI` interface in `cmd/juju/debug/api.go`

```go
type StreamStatus struct {
    Name   string
    State  string
    TxnID  int64
}

type DebugChangeStreamAPI interface {
    Status(ctx context.Context) ([]StreamStatus, error)
    Pause(ctx context.Context, modelUUID string) error
    Resume(ctx context.Context, modelUUID string) error
    Close() error
}
```

Only `Status` is called in this phase for polling. `Pause` and `Resume`
are wired in phase 04 but the interface is defined now so the mock can
implement them.

### 2. Define `ModelListAPI` interface in `cmd/juju/debug/api.go`

```go
type ModelListAPI interface {
    ListModels(ctx context.Context) ([]ModelInfo, error)
    Close() error
}

type ModelInfo struct {
    Name         string
    UUID         string
    IsController bool
}
```

This interface provides the list of models shown in the model-picker
overlay. The concrete `modelListAPIClient` wraps
`modelmanager.Client.ListModelSummaries()` (the same API used by
`juju models`) with `all=true` and converts `base.UserModelSummary`
to `ModelInfo`.

### 3. Implement concrete API clients

Create `api/client/debugchangestream/client.go` as a thin client
wrapping the `DebugChangeStream` facade's `Status` method (stub until
the facade is implemented).

Implement `modelListAPIClient` in `cmd/juju/debug/api.go` wrapping
`modelmanager.Client.ListModelSummaries()` with `all=true`. The
client is created in `debugCommand.Run()` via
`c.NewModelManagerAPIClient(ctx)` and `c.CurrentAccountDetails()` for
the user name.

### 4. Remove scope flags from `debugCommand` in `cmd/juju/debug/command.go`

The command takes **no flags** beyond what `ControllerCommandBase`
provides.

`debugCommand.Run()` must:

- Create the `modelmanager.Client` via `c.NewModelManagerAPIClient(ctx)`.
- Get the current user via `c.CurrentAccountDetails()`.
- Create `modelListAPIClient` wrapping the model manager client.
- Create the `DebugChangeStreamAPI` mock client.
- Resolve the current model name and UUID via `c.ModelUUIDs()`.
- Pass both API clients and the current model info into
  `newDebugModel()`.
- On exit, close both clients and resume all paused models.

### 5. Add per-model state tracking to `changestreamModel`

```go
type modelState struct {
    Name         string
    UUID         string
    Status       string
    TxnID        int64
    Transactions []transactionEntry
    Paused       bool
    PausedTxnIdx int
}

type changestreamModel struct {
    width         int
    height        int
    active        bool
    currentModel  string
    models        map[string]*modelState
    cursor        int
    pollInterval  time.Duration
    api           DebugChangeStreamAPI
    pickerOpen    bool
    pickerItems   []ModelInfo
    pickerCursor  int
}
```

- `models` maps model UUID → per-model state.
- `currentModel` is the UUID of the currently displayed model.
- `pickerOpen` / `pickerItems` / `pickerCursor` manage the model-picker
  overlay.

### 6. Add `modelSwitcherModel` overlay in `cmd/juju/debug/model_switcher.go`

When the user presses `m`, a full-screen overlay lists all models with
their current changestream status (RUNNING / PAUSED / STEP). The user
navigates with `↑`/`↓` and selects with `Enter`. `Esc` cancels.

```go
type modelSwitcherModel struct {
    width   int
    height  int
    items   []ModelInfo
    cursor  int
    open    bool
}
```

### 7. Update `changestreamModel.Update()`

- On `m` key: open the model-switcher overlay (populate items from
  `ModelListAPI` or the `models` map keys).
- On `p` key: pause only the current model (`models[currentModel]`).
- On `P` key: pause **all** models.
- On `r` key: resume only the current model.
- On `s` key: step only the current model.
- On `statusTickMsg`: call `api.Status(ctx)` and update each model's
  `Status` and `TxnID` from the returned `[]StreamStatus`. Replace the
  transaction list for each model when the status differs. Continue
  ticking.
- On `Enter` in picker: set `currentModel` to the selected model UUID
  and close the picker.

### 8. Update `changestreamModel.View()`

- Header bar: left "Changestream", centre shortcuts, right the current
  model name and its status.
- No tab bar — the current model is shown as a label.
- Transaction list: shows entries for `models[currentModel]`.
- When the picker is open, render the picker overlay on top.

### 9. Create `cmd/juju/debug/mock_data.go`

For this phase, populate each model's `Transactions` slice with mock
data (different transactions per model). Provide an initial set of
models (controller + 2 models) so the picker can be tested without a
live controller.

### 10. Update `debugModel` in `cmd/juju/debug/model.go`

- Store the `DebugChangeStreamAPI` and `ModelListAPI` references.
- Pass them into `changestreamModel` and `logModel`.
- When a model switch occurs (via a `switchModelMsg`), restart the log
  stream for the new model.
- On quit (`q`), call `Resume` for every model in `models` that is
  paused, then close both API clients.

### 11. Add `switchModelMsg` to `cmd/juju/debug/messages.go`

```go
type switchModelMsg struct {
    modelUUID string
    modelName string
}
```

When the model-switcher selects a model, it emits this message. The
top-level `debugModel.Update()` handles it by:

1. Updating `changestreamModel.currentModel`.
2. Updating `logModel` to restart the stream for the new model.
3. Updating the context bar model name.
4. Calling `switchModelFunc` to persist the model switch in the Juju
   client store (same effect as `juju switch <model>`).

The `switchModelFunc` is created in `debugCommand.Run()` using
`modelcmd.QualifyingClientStore.SetCurrentModel()` to qualify the
model name with the logged-in user before calling
`jujuclient.ClientStore.SetCurrentModel()`.

## Memory File

On completion, write `specs/debug-tui/memory/phase-03.md` containing:

- The `DebugChangeStreamAPI` interface methods.
- The `ModelListAPI` interface methods.
- The per-model state tracking approach.
- The model-switcher overlay rendering approach.
- The polling interval chosen.
- All file paths modified.
- Any deviations from this phase spec and the reason.

## Acceptance Criteria

- `juju debug` (no flags) launches the TUI showing the current model.
- `m` opens a model-picker overlay listing all models. `↑`/`↓` navigate,
  `Enter` selects, `Esc` cancels.
- Selecting a model switches the changestream transaction list and the
  log stream to that model, and also persists the switch in the Juju
  client store (equivalent to `juju switch <model>`).
- `p` pauses only the current model. The `●` dot appears for that model.
- `P` pauses all models.
- `r` resumes only the current model.
- `s` steps only the current model.
- Quitting resumes all paused models.
- When connected to a live controller, the status indicator for each
  model reflects the real changestream state (RUNNING / PAUSED / STEP),
  updated via polling.
