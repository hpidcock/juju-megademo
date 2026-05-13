# Task 07 — Domain Layer: `domain/debugchangestream`

## Goal

Create the `domain/debugchangestream` domain package that encapsulates
all database interactions for the `debug_change_stream` table. This
domain is consumed by the API facade in task 08.

## Dependencies

- **Task 01** — `debug_change_stream` and `change_log_txn_seq` tables
  must exist.
- **Task 05** — the stream worker must be able to react to state changes
  written by this domain.

Must complete before task 08.

## Memory Files to Read

Before writing any code, read the following memory files from
completed dependency tasks:

- `specs/debug-changestream/memory/task-01.md` — exact
  `debug_change_stream` and `change_log_txn_seq` table schemas,
  column names, and types. Use these to write correct SQL in the
  state layer without guessing column names.
- `specs/debug-changestream/memory/task-05.md` — the exact SQL
  constants and CAS update used by the stream worker to transition
  states. Ensures the domain layer uses compatible SQL and state
  string values (`'running'`, `'paused'`, `'step'`).

## Research Required

Before writing any code, read the following to understand Juju domain
conventions:

- Read an existing simple domain package in full, e.g.
  `domain/controllernode/` — its `doc.go`, `errors/errors.go`,
  `service/service.go`, and `state/state.go`. Note the layering: the
  state layer holds SQL; the service layer holds business logic; errors
  are defined separately.
- Read `domain/changestream/state/state.go` — for the `Watermark` struct
  and `locateLowestWatermark` pattern; this is reused in task 08 but
  understanding it here is useful context.
- Note the sqlair vs stdlib SQL pattern used in state files.

## Scope

Create the following files:

### `domain/debugchangestream/doc.go`

Package doc summarising that this package provides pause, step, and
resume control over a single Juju database's changestream for debugging
purposes.

### `domain/debugchangestream/errors/errors.go`

```go
var (
    // ErrNotPaused is returned when a step or resume is requested
    // but the changestream is not currently paused.
    ErrNotPaused = errors.New("changestream is not paused")

    // ErrAlreadyPaused is returned when a pause is requested but
    // the changestream is already paused or in step mode.
    ErrAlreadyPaused = errors.New("changestream is already paused")
)
```

### `domain/debugchangestream/service/service.go`

```go
// State defines the persistence operations required by Service.
type State interface {
    // CurrentTxnID returns the current value of change_log_txn_seq.
    CurrentTxnID(ctx context.Context) (int64, error)

    // DebugState returns the current state and step_target from
    // debug_change_stream.
    DebugState(ctx context.Context) (state string, stepTarget int64, err error)

    // SetPaused writes state='paused' to debug_change_stream.
    SetPaused(ctx context.Context) error

    // SetStep writes state='step' and the given step_target to
    // debug_change_stream.
    SetStep(ctx context.Context, stepTarget int64) error

    // SetRunning writes state='running' to debug_change_stream.
    SetRunning(ctx context.Context) error

    // AllNodesReachedTxn returns true when every row in
    // change_log_witness has upper_bound >= txnID. This is used to
    // confirm all HA nodes have consumed the step.
    AllNodesReachedTxn(ctx context.Context, txnID int64) (bool, error)

    // EventCountInRange returns the number of change_log rows whose
    // txn_id is in the inclusive range [minTxn, maxTxn].
    EventCountInRange(
        ctx context.Context, minTxn, maxTxn int64,
    ) (int, error)
}

// StepResult is returned by Step to describe what was consumed.
type StepResult struct {
    // TxnMin is the lowest txn_id dispatched.
    TxnMin int64
    // TxnMax is the highest txn_id dispatched (= step_target).
    TxnMax int64
    // EventCount is the number of change_log rows in the
    // [TxnMin, TxnMax] txn_id range, as reported by a
    // COUNT query on change_log. This represents how many events
    // became visible to the system during the step.
    EventCount int
}

// Service provides pause, step, and resume operations on a single
// database's changestream debug state.
type Service struct {
    st     State
    logger logger.Logger
}

// NewService constructs a Service.
func NewService(st State, logger logger.Logger) *Service

// Pause transitions the changestream from running to paused.
// Returns ErrAlreadyPaused if already paused or stepping.
func (s *Service) Pause(ctx context.Context) error

// Step advances the paused changestream by count transactions.
// Returns ErrNotPaused if the stream is not currently paused.
// Blocks (with polling) until all HA nodes have consumed the step.
func (s *Service) Step(
    ctx context.Context, count int,
) ([]StepResult, error)

// Resume transitions the changestream back to running.
// Returns ErrNotPaused if the stream is not currently paused.
func (s *Service) Resume(ctx context.Context) error

// Status returns the current debug state.
func (s *Service) Status(ctx context.Context) (string, error)
```

**`Step` implementation notes:**

`Step` performs `count` sequential sub-steps. For each sub-step:

1. Read current `change_log_txn_seq.id` → `currentTxn`.
2. If `currentTxn` equals the last known watermark (stream is at head),
   record a `StepResult` with `EventCount = 0` and continue.
3. Otherwise call `SetStep(ctx, currentTxn + 1)` to set
   `step_target = currentTxn + 1`.
4. Poll `AllNodesReachedTxn(ctx, currentTxn + 1)` with a short interval
   (e.g. 100 ms) until it returns `true` or the context is cancelled.
5. Query `change_log` to count how many rows fall within the consumed
   txn_id range (i.e. `SELECT COUNT(*) FROM change_log WHERE txn_id
   BETWEEN ? AND ?`). This is the `EventCount` for the `StepResult`.
6. Record the `StepResult`.

The polling loop must respect `ctx` cancellation.

### `domain/debugchangestream/state/state.go`

Implements the `State` interface above. Uses `sqlair` or raw `*sql.Tx`
(follow the pattern used in the domain found during research).

Key queries:

```sql
-- CurrentTxnID
SELECT id FROM change_log_txn_seq LIMIT 1;

-- DebugState
SELECT state, step_target FROM debug_change_stream LIMIT 1;

-- SetPaused
UPDATE debug_change_stream
    SET state = 'paused', updated_at = DATETIME('now', 'utc');

-- SetStep
UPDATE debug_change_stream
    SET state = 'step',
        step_target = ?,
        updated_at = DATETIME('now', 'utc');

-- SetRunning
UPDATE debug_change_stream
    SET state = 'running', updated_at = DATETIME('now', 'utc');

-- AllNodesReachedTxn
SELECT COUNT(*) = 0
    FROM change_log_witness
    WHERE upper_bound < ?;

-- EventCountInRange
SELECT COUNT(*) FROM change_log
    WHERE txn_id BETWEEN ? AND ?;
```

### Tests

- `domain/debugchangestream/service/service_test.go` — unit tests using
  a mock `State`.
- `domain/debugchangestream/state/state_test.go` — integration tests
  using `testing.DqliteSuite` against a real database with the Task 01
  schema.

## Sub-Agent Testing

To prevent context ballooning, delegate all test writing and test
execution to a sub-agent. The sub-agent's write scope is limited to
test files only — it must not modify production code.

Spawn two sub-agents in parallel with disjoint write scopes:

**Sub-agent A — service unit tests**
- Write scope: `domain/debugchangestream/service/service_test.go`.
- Task: write unit tests using a mock `State`, covering `Pause`
  (success and `ErrAlreadyPaused`), `Step` (success and
  `ErrNotPaused`), `Resume` (success and `ErrNotPaused`).
- Run: `go test ./domain/debugchangestream/service/...`.

**Sub-agent B — state integration tests**
- Write scope: `domain/debugchangestream/state/state_test.go`.
- Task: write integration tests using `testing.DqliteSuite` against
  a real database with the task-01 schema.
- Run: `go test ./domain/debugchangestream/state/...`.

Do not proceed to the Memory File step until both sub-agents report
a passing test suite.

## Memory File

On completion, write `specs/debug-changestream/memory/task-07.md`
containing:

- The full `State` interface as implemented (method signatures).
- The full `Service` method signatures.
- The exact SQL strings used in the state layer.
- All file paths created.
- The sqlair vs stdlib choice made and why.
- Any deviations from this task spec and the reason.

## DI Registration

As part of this task, register the new domain service in the dependency
injection layer:

1. Add a `DebugChangeStream() *debugchangestream.Service` method to the
   `ControllerDomainServices` interface.
2. Provide the concrete implementation in the controller domain services
   factory.
3. Wire the factory so that a new `debugchangestream.Service` is
   constructed with the correct `State` and `Logger` for the controller
   database.

The same pattern is required for model domain services so that the API
facade can access per-model debug services via
`DomainServicesForModel`.

## Acceptance Criteria

- The package follows the standard Juju domain layout.
- `doc.go` exists.
- The service is accessible via `ControllerDomainServices` and per-model
  domain services.
- All `State` interface methods are implemented in the state layer.
- All `Service` methods are implemented and tested.
- `Pause` returns `ErrAlreadyPaused` when called twice.
- `Step` returns `ErrNotPaused` when the stream is not paused.
- `Resume` returns `ErrNotPaused` when the stream is not paused.
- `go test ./domain/debugchangestream/...` passes.
