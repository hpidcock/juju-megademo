# Task 04 — Stream Worker: Query Extensions and Trace Fields

## Goal

Update the `Stream` worker's `readChanges` query and `changeEvent` struct
to select and carry `txn_id`, `trace_id`, and `span_id` from
`change_log`. Implement the `TracedChangeEvent` and the updated `Term`
interfaces on the concrete stream types.

## Dependencies

- **Task 01** — schema columns must exist.
- **Task 02** — `TracedChangeEvent` and updated `Term` interfaces must be
  defined.
- **Task 03** — `txn_id` must be populated in the database by the
  transaction hooks.

Must complete before tasks 05 and 06.

## Memory Files to Read

Before writing any code, read the following memory files from
completed dependency tasks:

- `specs/debug-changestream/memory/task-01.md` — exact column names
  added to `change_log` (`txn_id`, `trace_id`, `span_id`) and their
  types. Use this to write the correct `rows.Scan` arguments.
- `specs/debug-changestream/memory/task-02.md` — exact method
  signatures added to `ChangeEvent` and `Term`, plus every stub
  implementation that was updated. Confirms the interface contract
  that this task's concrete struct must satisfy.
- `specs/debug-changestream/memory/task-03.md` — confirms the
  transaction hooks are wired in and `txn_id` is being populated,
  so integration tests in this task can rely on non-zero `txn_id`
  values.

## Research Required

Before writing any code, read the following files in full:

- `internal/changestream/stream/stream.go` — the `selectQuery` constant,
  `readChanges` method, `changeEvent` struct, and the internal `term`
  struct (including its `Done` method).
- `internal/changestream/stream/stream_test.go` — understand the test
  pattern, particularly how terms are asserted.
- `internal/changestream/stream/doc.go` — confirm the package description
  is still accurate.

## Scope

### `internal/changestream/stream/stream.go`

#### Update `selectQuery`

Add `c.txn_id`, `c.trace_id`, `c.span_id` to the SELECT list:

```sql
SELECT MAX(c.id), c.edit_type_id, n.namespace, c.changed,
       c.created_at, c.txn_id, c.trace_id, c.span_id
    FROM change_log c
        JOIN change_log_edit_type t ON c.edit_type_id = t.id
        JOIN change_log_namespace n ON c.namespace_id = n.id
    WHERE c.id > ?
    GROUP BY c.namespace_id, c.changed
    ORDER BY c.id;
```

Note: `MAX(c.id)` already selects the most recent row per
`(namespace, changed)` for coalescing purposes. The `txn_id`,
`trace_id`, and `span_id` values should come from the same
(highest-id) row. Verify the existing coalescing behaviour before
deciding whether additional SQL changes are needed.

#### Update `changeEvent` struct

Add three new fields:

```go
type changeEvent struct {
    id         int64
    changeType int
    namespace  string
    changed    string
    createdAt  string
    txnID      int64
    traceID    string
    spanID     string
}
```

Update the `rows.Scan(...)` call in `readChanges` to scan the three
new columns.

#### Implement `TracedChangeEvent` on `changeEvent`

Use value receivers for consistency with existing methods on `changeEvent`:

```go
func (e changeEvent) TraceID() string { return e.traceID }
func (e changeEvent) SpanID()  string { return e.spanID  }
```

#### Update the `term` struct and implement `TxnMinID` / `TxnMaxID`

The `term` struct carries the lower and upper `change_log.id` bounds
today. Add `txnMin` and `txnMax int64` fields computed from the batch
of `changeEvent` values in `readChanges`.

Implement the two new `Term` interface methods:

```go
func (t *term) TxnMinID() int64 { return t.txnMin }
func (t *term) TxnMaxID() int64 { return t.txnMax }
```

The `txnMin` and `txnMax` are calculated from the scanned `changeEvent`
slice: `txnMin = min(e.txnID for e in changes)`,
`txnMax = max(e.txnID for e in changes)`.

## Sub-Agent Testing

To prevent context ballooning, delegate all test writing and test
execution to a sub-agent. The sub-agent's write scope is limited to
test files only — it must not modify production code.

When spawning the sub-agent, provide it with:
- The full paths of every production file you have written.
- The acceptance criteria from this task.
- The exact `go test` commands to run.

The sub-agent must:
1. Write the new test(s) described in the acceptance criteria
   (`TxnMinID`/`TxnMaxID` bounds, `TraceID`/`SpanID` round-trip).
2. Run `go test ./internal/changestream/stream/...` and report any
   failures.
3. Fix test failures (within test files only) until the suite passes.
4. Report the final `go test` output back to you.

Do not proceed to the Memory File step until the sub-agent reports
a passing test suite.

## Memory File

On completion, write `specs/debug-changestream/memory/task-04.md`
containing:

- The exact updated `selectQuery` SQL string.
- The final `changeEvent` struct field list.
- How `txnMin` / `txnMax` are computed from the scanned rows.
- The file path modified.
- Any deviations from this task spec and the reason.

## Acceptance Criteria

- `changeEvent` implements `core/changestream.TracedChangeEvent`
  using value receivers.
- `term` implements the updated `core/changestream.Term` (including
  `TxnMinID` and `TxnMaxID`).
- `go test ./internal/changestream/stream/...` passes.
- Existing stream tests that assert on change events continue to pass.
- A new test verifies that `TxnMinID()` and `TxnMaxID()` return the
  correct bounds for a term containing changes with known `txn_id`
  values.
- A new test verifies that `TraceID()` and `SpanID()` return the values
  stored in `change_log` for a given row.
