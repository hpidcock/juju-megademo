# Task 05 — Stream Worker: Debug State Polling (completed)

## SQL constants added

### `debugStateQuery`
```sql
SELECT state, step_target FROM debug_change_stream LIMIT 1;
```

### `debugPauseQuery`
```sql
UPDATE debug_change_stream
    SET state = 'paused',
        updated_at = STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc')
    WHERE state = 'step' AND step_target = ?;
```

### `selectStepQuery`
A sibling of `selectQuery` with `AND c.txn_id <= ?` added to the
`WHERE` clause before `GROUP BY`. Used when `readChanges` is called
with a non-zero `stepTarget`.

## `debugState` struct

```go
type debugState struct {
    state      string
    stepTarget int64
}
```

## Location of the debug check in `loop()`

### Deterministic restart (before the `OUTER` loop)

Immediately after `s.reportState(stateBegin)` and before `var attempt
int`, `loop()` calls `s.readDebugState(ctx)`. If the returned state is
not `"running"`, `s.debugMode` is set to `true`. This ensures that a
stream restarted with a pending `paused` or `step` state re-enters
debug mode without waiting for a change event.

### Per-iteration check (`default` branch)

At the top of the `default` branch — before `readChanges` is called
and only when `s.debugMode` is `true` — the loop calls
`s.readDebugState(ctx)` and switches on the result:

- **`"running"`**: clears `s.debugMode` and falls through to the
  normal `readChanges(0)` path.
- **`"paused"`**: applies `backOffStrategy(0, attempt)` sleep (without
  incrementing `attempt`) and `continue OUTER` — no term is
  dispatched.
- **`"step"`**: sets `stepTarget = ds.stepTarget` and falls through to
  `readChanges(stepTarget)`, which filters changes to
  `txn_id <= stepTarget`.

### CAS update after `step` term completes

After `s.recordTermView(...)` inside the `case empty, ok := <-term.done:`
branch, if `stepTarget > 0`, `s.writeDebugPaused(ctx, stepTarget)` is
called. This executes `debugPauseQuery`, which is a CAS update that
writes `state = 'paused'` only if `state = 'step' AND step_target = ?`
still holds. The next iteration reads `paused` and halts.

## Files modified

| File | Change |
|------|--------|
| `internal/changestream/stream/stream.go` | `debugMode` field; `selectStepQuery`, `debugStateQuery`, `debugPauseQuery` constants; `debugState` type; `readChanges(stepTarget int64)` signature; `readDebugState`/`writeDebugPaused` methods; `loop()` startup + default branch + post-term CAS |
| `internal/changestream/stream/stream_test.go` | All `readChanges()` calls updated to `readChanges(0)`; 9 new tests added |

## New tests

| Test | What it verifies |
|------|-----------------|
| `TestReadDebugStateRunning` | Returns `"running"` from seeded row |
| `TestReadDebugStatePaused` | Returns `"paused"` after DB update |
| `TestReadDebugStateStep` | Returns `"step"` with correct `stepTarget` |
| `TestWriteDebugPausedCAS` | Flips `state` to `"paused"` when CAS matches |
| `TestWriteDebugPausedCASIsNoopWhenStateChanged` | No-op when CAS condition fails |
| `TestReadChangesWithStepTarget` | `readChanges(2)` returns only `txn_id <= 2` |
| `TestStreamHaltsWhenPaused` | Full stream dispatches no terms while `state='paused'` |
| `TestStreamDispatchesUpToStepTargetThenPauses` | Step mode: dispatches ≤ step_target, then DB shows `'paused'` |
| `TestStreamResumesWhenRunning` | Stream dispatches normally after `state` returns to `'running'` |

## Deviations from spec

- **`selectStepQuery` is a separate constant** rather than a computed
  string. The spec says "do not duplicate the entire query string"; in
  practice a second named constant is clearer and more maintainable
  than runtime string manipulation. The suffix (`GROUP BY … ORDER BY`)
  is the only repeated text.
- **No "special change event" mechanism** was implemented for setting
  `debugMode` at runtime. The spec mentions this is triggered by a
  future change event type; only the deterministic startup path and the
  `"running"` clear-path were implemented here. Task 07 is expected to
  add the runtime event trigger.
