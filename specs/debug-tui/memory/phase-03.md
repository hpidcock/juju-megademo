# Phase 03 — Memory File

## Interface Changes

### `DebugChangeStreamAPI` (cmd/juju/debug/api.go)
```go
type DebugChangeStreamAPI interface {
    Status(ctx context.Context) ([]StreamStatus, error)
    Pause(ctx context.Context, modelUUID string) error
    Resume(ctx context.Context, modelUUID string) error
    Close() error
}
```
Only `Status` is called in this phase. `Pause` and `Resume` are defined for the interface but wired in phase 04.

### `ModelListAPI` (cmd/juju/debug/api.go)
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

The concrete `modelListAPIClient` wraps `modelmanager.Client.ListModelSummaries()`
with `all=true` to retrieve all models visible to the user. It converts
`base.UserModelSummary` to `ModelInfo`, skipping entries with errors.

### Removed
- `DebugScope` type — no longer needed; no CLI scope flags.

## Per-Model State

The `changestreamModel` tracks per-model state in a `map[string]*modelState` keyed by model UUID:

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
```

`currentModel` (UUID) identifies which model is displayed. Switching models updates the transaction list, the context bar, and the log stream.

## Model List Retrieval

The model list is fetched from the real `ModelManager` facade via
`modelmanager.Client.ListModelSummaries(ctx, user, all=true)`. This is
the same API that `juju models` uses.

Flow:
1. `command.go` creates `modelmanager.Client` via `c.NewModelManagerAPIClient(ctx)`.
2. `command.go` gets the current user via `c.CurrentAccountDetails()`.
3. `newModelListAPIClient(modelManagerClient, user)` wraps the client.
4. On `changestreamModel.Init()`, a `fetchModelsCmd()` is issued to
   populate the models map from the API.
5. On `m` key, `fetchModelsForPickerCmd()` re-fetches and opens the
   picker overlay with the fresh list.
6. `mergeModels()` adds any new models to the map without overwriting
   existing state (pause/resume status is preserved).

## Model-Switcher Overlay

When `m` is pressed, the changestream model fetches a fresh model list
from `ModelListAPI`, then opens a picker overlay. The overlay is rendered
in `changestreamModel.viewPicker()` and drawn on top of the normal layout
by the top-level `debugModel.View()`. Navigation uses `↑`/`↓`, selection
uses `Enter`, cancel uses `Esc`. The overlay shows each model's name and
changestream status (RUNNING / PAUSED / STEP).

On selection, `changestreamModel` emits a `switchModelMsg{modelUUID, modelName}`. The top-level `debugModel.Update()` handles this by:
1. Updating `modelName` and `modelUUID` on the context bar.
2. Setting `changestream.currentModel` to the new UUID and resetting its cursor.
3. Restarting the log stream for the new model.
4. Resetting the trace pane.
5. Calling `switchModelFunc` to persist the model switch in the Juju client
   store (equivalent to `juju switch <model>`).

The `switchModelFunc` is created in `debugCommand.Run()`:
```go
qualifyingStore := modelcmd.QualifyingClientStore{ClientStore: store}
switchModel := func(modelName string) error {
    return qualifyingStore.SetCurrentModel(controllerName, modelName)
}
```
This qualifies the model name with the logged-in user (e.g. `mymodel` → `admin/mymodel`) and then calls `jujuclient.ClientStore.SetCurrentModel()`. After this call, the current model is persisted to disk, so any subsequent `juju` command targets the switched model.

## Quit Semantics

On `q`, `debugModel.Update()` calls `resumeAllPaused()` which iterates `changestream.pausedModelUUIDs()` and calls `debugAPI.Resume(ctx, uuid)` for each. This ensures the system is never left in a paused state.

## Polling Interval

- Changestream tick (transaction refresh): 500ms
- Status poll (DebugChangeStreamAPI.Status): 500ms, same cadence as the transaction tick

## Files Created

- `cmd/juju/debug/mock_api.go` — mock implementations of `DebugChangeStreamAPI` and `ModelListAPI` (for testing without a live controller)
- `api/client/debugchangestream/client.go` — thin API client stub for the `DebugChangeStream` facade

## Files Removed

- `cmd/juju/debug/mock_data.go` — removed; models are now fetched from the real `ModelManager` API

## Files Modified

- `cmd/juju/debug/api.go` — added `StreamStatus`, `DebugChangeStreamAPI`, `ModelInfo`, `ModelListAPI`, `modelListAPIClient` (wraps `modelmanager.Client`); removed `DebugScope`
- `cmd/juju/debug/changestream.go` — replaced single-model state with per-model `modelState` map, added picker overlay logic, `P` pauses all, `r` resumes current, `pausedModelUUIDs()`, `fetchModelsCmd()`/`fetchModelsForPickerCmd()`, `mergeModels()`
- `cmd/juju/debug/messages.go` — added `switchModelMsg`, `statusTickMsg`, `listModelsMsg` (with `open` bool); renamed `changestreamTickMsg`
- `cmd/juju/debug/model.go` — added `debugAPI`, `modelLister`, and `switchModelFunc` fields; `switchModelMsg` handler calls `switchModelFunc` to persist the model switch in the client store; `restartLogStream()`, `resumeAllPaused()`; picker overlay rendering in `View()`
- `cmd/juju/debug/command.go` — resolved model UUID via `ModelUUIDs()`, creates `modelmanager.Client` via `NewModelManagerAPIClient()`, gets user via `CurrentAccountDetails()`, creates `switchModelFunc` using `QualifyingClientStore.SetCurrentModel()`, passes `debugAPI`, `modelLister`, and `switchModelFunc` to `newDebugModel()`
- `cmd/juju/debug/help.go` — updated keybindings (P, m descriptions)
- `cmd/juju/debug/doc.go` — updated multi-model documentation
- `specs/debug-tui.md` — removed `--model`/`--all`/`--controller` flags, updated keybinding table, multi-model section, API interfaces, phase table
- `specs/debug-tui/phase-03-model-switching.md` — rewritten for model-picker approach

## Deviations from Original Phase-03 Spec

1. **No CLI scope flags**: The original spec called for `--model`, `--all`, `--controller`. Per updated requirements, all scope handling is done via the `m` key inside the TUI.
2. **No `DebugScope` type**: Removed since there are no flags to encode into a scope.
3. **No tab bar**: The original spec used tabs for multi-model display. The updated approach uses a picker overlay; only one model is visible at a time.
4. **`DebugChangeStreamAPI.Status` takes no scope argument**: Returns status for all models from a single call, since the API always returns all.
5. **`model_switcher.go` not needed**: The picker rendering and key handling are integrated directly into `changestreamModel` (in `viewPicker()` and `updatePicker()`), avoiding a separate sub-model that would require complex message routing.
6. **Real model list from ModelManager API**: Models are fetched from `modelmanager.Client.ListModelSummaries()` (same API as `juju models`), not from mock data. The `mockModelListAPI` remains for testing without a live controller.
