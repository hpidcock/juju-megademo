# Task 07 — Domain Layer: `domain/debugchangestream` (completed)

## Files created

| File | Purpose |
|------|---------|
| `domain/debugchangestream/doc.go` | Package documentation |
| `domain/debugchangestream/errors/errors.go` | Sentinel errors |
| `domain/debugchangestream/service/service.go` | Business logic + State interface |
| `domain/debugchangestream/service/package_test.go` | go:generate directive |
| `domain/debugchangestream/service/state_mock_test.go` | Typed mock for State |
| `domain/debugchangestream/service/service_test.go` | Unit tests (10 tests) |
| `domain/debugchangestream/state/types.go` | sqlair type definitions |
| `domain/debugchangestream/state/state.go` | SQL implementation |
| `domain/debugchangestream/state/state_test.go` | Integration tests (10 tests) |

## DI files modified

| File | Change |
|------|--------|
| `internal/services/interface.go` | Added `DebugChangeStream()` to `ControllerDomainServices` and `ModelDomainServices` |
| `domain/services/controller.go` | Concrete `DebugChangeStream()` on `ControllerServices` |
| `domain/services/model.go` | Concrete `DebugChangeStream()` on `ModelServices` |
| `domain/services/testing/suite.go` | Explicit `DebugChangeStream()` on `domainServices` to resolve embedding ambiguity |
| `internal/worker/domainservices/worker.go` | Explicit `DebugChangeStream()` on `domainServices` to resolve embedding ambiguity |

## State interface (as implemented)

```go
type State interface {
    CurrentTxnID(ctx context.Context) (int64, error)
    DebugState(ctx context.Context) (state string, stepTarget int64, err error)
    SetPaused(ctx context.Context) error
    SetStep(ctx context.Context, stepTarget int64) error
    SetRunning(ctx context.Context) error
    AllNodesReachedTxn(ctx context.Context, txnID int64) (bool, error)
    EventCountInRange(ctx context.Context, minTxn, maxTxn int64) (int, error)
}
```

## Service method signatures

```go
func NewService(st State, logger logger.Logger) *Service
func (s *Service) Pause(ctx context.Context) error
func (s *Service) Step(ctx context.Context, count int) ([]StepResult, error)
func (s *Service) Resume(ctx context.Context) error
func (s *Service) Status(ctx context.Context) (string, error)
```

## SQL strings used in the state layer

```sql
-- CurrentTxnID
SELECT &dbTxnSeq.id FROM change_log_txn_seq LIMIT 1;

-- DebugState
SELECT &dbDebugState.* FROM debug_change_stream LIMIT 1;

-- SetPaused
UPDATE debug_change_stream
SET state      = 'paused',
    updated_at = STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc');

-- SetStep
UPDATE debug_change_stream
SET state       = 'step',
    step_target = $M.step_target,
    updated_at  = STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc');

-- SetRunning
UPDATE debug_change_stream
SET state      = 'running',
    updated_at = STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc');

-- AllNodesReachedTxn
SELECT COUNT(*) AS &M.count
FROM   change_log_witness
WHERE  upper_bound < $M.upper_bound;

-- EventCountInRange
SELECT COUNT(*) AS &M.count
FROM   change_log
WHERE  txn_id >= $M.min_txn
AND    txn_id <= $M.max_txn;
```

## sqlair vs stdlib choice

**sqlair** was used throughout the state layer, consistent with all other
domain state packages in the codebase. The `domain.StateBase.Prepare`
helper is used to cache prepared statements. For simple queries with no
Go type parameters (SetPaused, SetRunning), `st.Prepare(query)` is called
with no type samples. For COUNT queries, `sqlair.M{}` is used as both
the parameter type and result type.

## Deviations from spec

### Step target uses `currentTxn` not `currentTxn + 1`

The spec says `SetStep(ctx, currentTxn + 1)` and `AllNodesReachedTxn(ctx,
currentTxn + 1)`. Based on how `change_log_txn_seq` works (the id is
incremented before being assigned to `change_log.txn_id`), `currentTxn`
is the highest assigned txn_id. Using `currentTxn` as the step_target
tells the stream to process all pending changes. Using `currentTxn + 1`
would require a txn that doesn't exist yet, causing `AllNodesReachedTxn`
to block indefinitely. The implementation uses `currentTxn` as the target.

### Embedding ambiguity: explicit DebugChangeStream on domainServices

Both `ControllerDomainServices` and `ModelDomainServices` define
`DebugChangeStream()`. Wherever both are embedded into a single struct
(in `domain/services/testing/suite.go` and
`internal/worker/domainservices/worker.go`), an explicit forwarding
method was added that delegates to the model services implementation.
This follows the principle that `DomainServices` is per-model.

### STRFTIME format

The spec shows `DATETIME('now', 'utc')` for `updated_at`. The actual
schema seed uses `STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc')` (with
milliseconds precision), matching the pattern from task-05 and the
existing schema. The implementation uses `STRFTIME`.
