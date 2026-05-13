# Task 05 — Stream Worker: Debug State Polling

## Goal

Add debug state awareness to the `Stream` worker loop. The stream
discovers that debug mode has been enabled via a special change-stream
event type, not by polling `debug_change_stream` on every iteration.
Once the special event is received, an in-memory flag is set and the
worker begins calling `readDebugState()` on each iteration.

When `debug_change_stream.state = 'paused'` the stream must halt
without dispatching new terms. When `state = 'step'` the stream must
advance only up to `step_target` (by `txn_id`), then write `paused`
back and halt using a CAS-style update.

## Dependencies

- **Task 01** — `debug_change_stream` table must exist.
- **Task 04** — `txn_id`-aware query and `changeEvent.txnID` field must
  exist (the step predicate relies on `txn_id`).

Must complete before task 07.

## Memory Files to Read

Before writing any code, read the following memory files from
completed dependency tasks:

- `specs/debug-changestream/memory/task-01.md` — exact
  `debug_change_stream` schema (column names, types, CHECK
  constraints, seed row). Use this to write correct SQL constants.
- `specs/debug-changestream/memory/task-04.md` — exact updated
  `selectQuery` SQL string and how `txn_id` is scanned. Required to
  add the `AND c.txn_id <= ?` predicate extension in the step path
  without duplicating the base query.

## Research Required

Before writing any code, read the following files in full:

- `internal/changestream/stream/stream.go` — the full `loop()` method,
  the `OUTER` / `INNER` label structure, the existing `FileNotifier`
  pause branch, and the `readChanges` signature.
- `internal/changestream/stream/stream_test.go` — how the idle state
  and the `FileNotifier` pause are tested.
- The `debug_change_stream` table schema from task 01 (three columns:
  `state TEXT`, `step_target INTEGER`, `updated_at DATETIME`).

## Scope

### `internal/changestream/stream/stream.go`

#### New SQL constants

```sql
-- debugStateQuery reads the current debug state for this database.
SELECT state, step_target FROM debug_change_stream LIMIT 1;

-- debugPauseQuery writes 'paused' back after a step completes.
-- Uses a CAS-style WHERE clause to prevent lost-update races.
UPDATE debug_change_stream
    SET state = 'paused',
        updated_at = STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc')
    WHERE state = 'step' AND step_target = ?;
```

#### New `debugState` type

```go
type debugState struct {
    state      string
    stepTarget int64
}
```

#### New `readDebugState` method

Queries `debug_change_stream` and returns a `debugState`. If the table
returns no rows (not yet populated during a migration), treat the state
as `running`.

#### In-memory debug flag

Add a boolean `debugMode bool` field to the stream worker struct. This
field is only set to `true` when a special debug-mode-enabled change
event is received. While `debugMode` is `false`, `readDebugState` is
never called, preserving zero overhead during normal operation.

#### Deterministic restart behaviour

The debug state records the `change_log` ID at which debug mode is
considered active (stored in `debug_change_stream`). When the stream
worker starts, if `debugMode` is discovered by reading the debug state
and the worker's current watermark has not yet passed that ID, the
worker sets `debugMode = true` immediately, making restart behaviour
deterministic.

#### Changes to `loop()`

At the top of each poll iteration — **before** calling `readChanges`,
and only when `debugMode = true` — call `readDebugState`. Based on
the result:

- `running`: clear `debugMode`, proceed as today (existing behaviour
  unchanged).
- `paused`: apply the existing backoff sleep and continue the loop
  without dispatching. Do not advance the backoff counter
  (the stream is intentionally idle, not retrying due to emptiness).
- `step`: call `readChanges` with an additional SQL predicate
  `AND c.txn_id <= <step_target>`. After the term's `Done` signal is
  received, write `paused` back using a CAS-style update:

  ```sql
  UPDATE debug_change_stream
      SET state = 'paused',
          updated_at = STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc')
  WHERE state = 'step' AND step_target = ?;
  ```

  Then continue the loop (which will immediately read `paused` and halt).

The `step`-bounded read uses the same `selectQuery` from task 04 with an
extra `WHERE` clause appended before the `GROUP BY`. Do not duplicate the
entire query string — extract a helper or add an optional predicate
parameter to `readChanges`.

#### `FileNotifier` interaction

The existing `FileNotifier` branch in the `OUTER` loop is unchanged. The
new database-driven debug state check runs independently, in the `default`
branch, before the `readChanges` call.

## Sub-Agent Testing

To prevent context ballooning, delegate all test writing and test
execution to a sub-agent. The sub-agent's write scope is limited to
test files only — it must not modify production code.

When spawning the sub-agent, provide it with:
- The full paths of every production file you have written.
- The acceptance criteria from this task.
- The exact `go test` commands to run.

The sub-agent must:
1. Write the new tests described in the acceptance criteria (pause,
   step, resume, CAS behaviour).
2. Run `go test ./internal/changestream/stream/...` and report any
   failures.
3. Fix test failures (within test files only) until the suite passes.
4. Report the final `go test` output back to you.

Do not proceed to the Memory File step until the sub-agent reports
a passing test suite.

## Memory File

On completion, write `specs/debug-changestream/memory/task-05.md`
containing:

- The exact SQL constants added (`debugStateQuery`, `debugPauseQuery`).
- The `debugState` struct definition.
- A short prose description of where in `loop()` the check is inserted
  and how the three states (`running`, `paused`, `step`) are handled.
- The file path modified.
- Any deviations from this task spec and the reason.

## Acceptance Criteria

- When `debug_change_stream.state = 'paused'`, the stream does not
  dispatch any new terms. Existing in-flight terms complete normally.
- When `debug_change_stream.state = 'step'` with `step_target = N`,
  the stream dispatches only changes with `txn_id <= N`, then writes
  `state = 'paused'` and halts.
- After `step` completes, `debug_change_stream.state` is `'paused'`
  in the database.
- During normal operation (`debugMode = false`), `readDebugState` is
  never called — zero overhead.
- `running` state preserves the existing behaviour exactly (no
  regression in existing tests).
- On restart with a pending debug state, the stream enters debug mode
  deterministically based on its current position vs. the recorded
  change_log ID.
- State transitions use CAS-style updates to prevent lost-update races.
- `go test ./internal/changestream/stream/...` passes.
- New tests cover:
  - Stream halts when state transitions to `paused`.
  - Stream dispatches only up to `step_target` and then halts.
  - Stream resumes dispatching when state returns to `running`.
  - CAS update for `debugPauseQuery` is a no-op if state has already
    been changed externally.
