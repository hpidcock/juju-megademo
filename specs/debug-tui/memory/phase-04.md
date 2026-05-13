# Phase 04 — Memory File

## DebugChangeStreamAPI Interface

```go
type StepResult struct {
    TxnMin     int64
    TxnMax     int64
    EventCount int
    TraceID    string
    SpanID     string
}

type DebugChangeStreamAPI interface {
    Status(ctx context.Context) ([]StreamStatus, error)
    Pause(ctx context.Context, modelUUID string) error
    Step(ctx context.Context, modelUUID string, count int) ([]StepResult, error)
    Resume(ctx context.Context, modelUUID string) error
    Close() error
}
```

Concrete client `debugChangeStreamAPIClient` in `cmd/juju/debug/api.go` wraps
`debugchangestream.Client`. It maps `DebugChangeStreamTarget{ModelUUID: uuid}`
for Pause/Step/Resume calls, and converts `DebugChangeStreamStatusResult`
and `DebugChangeStreamStepResult` wire types to the TUI's `StreamStatus`
and `StepResult` types. Results with errors are filtered out.

## StepResult Mapping to transactionEntry

Each `StepResult` maps to a `transactionEntry`:
- `TxnID` = `StepResult.TxnMax` (the highest txn_id consumed in the step)
- `EventCount` = `StepResult.EventCount`
- `TraceID` = `StepResult.TraceID`
- `SpanID` = `StepResult.SpanID`

When `TxnMin == TxnMax`, a single entry is created. When they differ, only
one entry with `TxnMax` is shown (the range is collapsed). Entries with
`TxnMax == 0 && EventCount == 0` (stream already at head) are skipped.

New entries are prepended (newest first) and capped at 10. After a step,
`cursor` is set to 0 and a `selectTxnMsg` is emitted for the first entry.

## Error Display Strategy

Errors from facade calls are displayed in the Changestream pane header,
immediately below the shortcuts/status line, in red. The error string is
prefixed with the action name (e.g. `"Step failed: ..."`,
`"Pause failed: ..."`, `"Resume failed: ..."`).

- On success, `headerErr` is cleared.
- On failure, state is not modified.
- The error persists until the next successful action or until the model
  is switched (which clears `headerErr`).

## Step Count Input (S key)

The `S` key activates a `bubbles/textinput` inline in the header area,
prompting "Step N: ". While active, all key events go to the text input.
`Enter` parses the integer and issues `stepCmd(uuid, count)`. `Esc`
cancels. Invalid input shows "Invalid step count" in `headerErr`.

## Graceful Shutdown Logic

On quit (`q` key), `resumeAllPaused()` iterates
`changestream.pausedModelUUIDs()` which returns UUIDs for models where
`Paused == true` OR `Status == "STEP"`. For each, `debugAPI.Resume(ctx,
uuid)` is called. Errors are silently ignored (best-effort). The log
stream cancel is also called.

## Status Polling

`statusTickMsg` triggers `statusCmd()` which calls
`api.Status(context.Background())`. The result (`statusResultMsg`) updates
each model's `Status` and `TxnID` fields. The controller database name
"controller" is mapped to the controller model UUID via the
`controllerModel` field set during `mergeModels()`.

The transaction list is NOT replaced on status polls — it is populated
exclusively by step results.

## Changestream Tick Removed

The `changestreamTickMsg` and associated `tickChangestream`/`scheduleChangestreamTick`
functions have been removed. Previously these generated mock transaction data
every 500ms. Now the only periodic tick is the `statusTickMsg` → `statusCmd()` →
`statusResultMsg` cycle.

## Message Types Added

- `stepResultMsg{results []StepResult, err error}`
- `pauseResultMsg{err error}`
- `resumeResultMsg{err error}`
- `statusResultMsg{statuses []StreamStatus, err error}`
- `selectTxnMsg` changed from `{txnIndex int}` to `{txn transactionEntry}`

Removed: `changestreamTickMsg`

## Files Created

None.

## Files Modified

- `cmd/juju/debug/api.go` — Added `StepResult` type, `Step` method to
  `DebugChangeStreamAPI` interface, `debugChangeStreamAPIClient` concrete
  implementation with `newDebugChangeStreamAPIClient` constructor; added
  `api/base`, `api/client/debugchangestream`, `rpc/params`, `strings` imports
- `cmd/juju/debug/messages.go` — Added `stepResultMsg`, `pauseResultMsg`,
  `resumeResultMsg`, `statusResultMsg`; changed `selectTxnMsg` from
  `{txnIndex int}` to `{txn transactionEntry}`; removed `changestreamTickMsg`
- `cmd/juju/debug/changestream.go` — Replaced mock transaction generation
  with real facade calls; added `controllerModel` field, `stepInputMode`
  field, `stepInput` textinput; added async cmd methods (`pauseCmd`,
  `pauseAllCmd`, `resumeCmd`, `stepCmd`, `statusCmd`); added
  `updateStepInput`; added `prependTransaction`; removed
  `generateMockTransactions`, `randomHex`, `randomMockTraceID`,
  `mockTraceIDs`, `tickChangestream`, `scheduleChangestreamTick`,
  `modelByUUID`; `Init()` no longer schedules changestream tick; empty
  initial transaction list
- `cmd/juju/debug/model.go` — Added `stepInputMode` guard in key routing;
  `selectTxnMsg` handler now uses `selectMsg.txn` directly instead of
  looking up by index; `switchModelMsg` clears `headerErr`; graceful
  shutdown resumes STEP models too
- `cmd/juju/debug/command.go` — Uses `newDebugChangeStreamAPIClient(apiRoot)`
  instead of `newMockDebugChangeStreamAPI()`
- `cmd/juju/debug/mock_api.go` — Added `Step` method to
  `mockDebugChangeStreamAPI`
- `cmd/juju/debug/doc.go` — Removed phase-04 TODOs; updated changestream
  refresh cycle documentation
- `rpc/params/debugchangestream.go` — Added `TraceID` and `SpanID` fields
  to `DebugChangeStreamDBResult` (omitempty); added
  `DebugChangeStreamDBStatus` and `DebugChangeStreamStatusResult` types
- `api/client/debugchangestream/client.go` — Added `Status` method
- `apiserver/facades/controller/debugchangestream/facade.go` — Added
  `Status` and `CurrentTxnID` to `DebugChangeStreamService` interface;
  added `Status` method to API; updated `Step` to populate `TraceID`
  and `SpanID` from the last domain `StepResult`
- `apiserver/facades/controller/debugchangestream/facade_test.go` — Added
  `Status`, `CurrentTxnID` to `stubChangeStreamSvc`; added
  `statusRes`, `statusErr`, `txnIDRes`, `txnIDErr` fields
- `domain/debugchangestream/service/service.go` — Added `TraceID` and
  `SpanID` to `StepResult`; added `LatestTraceInTxnRange` to State
  interface; `Step` populates trace fields; added `CurrentTxnID` method
- `domain/debugchangestream/service/state_mock_test.go` — Added
  `LatestTraceInTxnRange` mock method
- `domain/debugchangestream/state/state.go` — Added
  `LatestTraceInTxnRange` method
- `domain/debugchangestream/state/types.go` — Added `dbTraceInfo` type;
  removed doc comments from existing types

## Deviations from Phase-04 Spec

1. **Status facade method added**: The spec didn't mention adding `Status`
   to the facade, but the facade didn't have one. Added `Status` method to
   the facade, wire types, and API client to support real status polling.

2. **TraceID/SpanID in domain StepResult**: Extended the domain
   `StepResult` with `TraceID` and `SpanID` fields, and added
   `LatestTraceInTxnRange` to the state layer. This populates trace
   context end-to-end rather than leaving it as empty strings.

3. **selectTxnMsg carries transactionEntry**: Changed from
   `{txnIndex int}` to `{txn transactionEntry}` so the top-level model
   doesn't need to look up the entry by index (which could be stale after
   state changes). This is a simplification that makes the data flow
   cleaner.

4. **Controller model mapping in Status**: The Status API returns
   `Name: "controller"` for the controller database, but the TUI's models
   map is keyed by model UUID. Added a `controllerModel` field to track
   the controller model's UUID (set during `mergeModels`) and map
   "controller" → controller model UUID in the status handler.

5. **pauseResultMsg/resumeResultMsg don't update local state immediately**:
   The spec says to update status optimistically on p/r keys. Instead, the
   implementation calls the API and updates state on success in the result
   message handler. This avoids state inconsistency if the API call fails.

6. **P key uses async pauseAllCmd**: Instead of iterating and synchronously
   updating local state, `P` issues async `pauseCmd` calls for each model.
   Status updates come from the periodic status poll.
