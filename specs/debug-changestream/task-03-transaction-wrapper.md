# Task 03 — Transaction Wrapper Hooks

## Goal

Redesign the `RetryingTxnRunner` so that every write transaction
automatically:

1. Starts as a read-only transaction; upgrades to a write transaction
   when the caller attempts a mutation (detected by the
   `SQLITE_READONLY` error returned by SQLite).
2. On the write-transaction path, injects setup statements immediately
   after `BEGIN IMMEDIATE`: set `is_in_txn = 1` and write the active
   OTel trace/span IDs into `change_log_trace_ctx`.
3. Injects finalise statements before `COMMIT`: advance
   `change_log_txn_seq`, back-fill `change_log.txn_id`, and reset
   `change_log_trace_ctx`.

No individual call site needs changing. The hooks are invoked
transparently by the transaction runner infrastructure.

## Dependencies

- **Task 01** must be complete (the three new tables must exist in the
  schema before the hook SQL can run against a real database).

Must complete before task 04.

## Memory Files to Read

Before writing any code, read the following memory files from
completed dependency tasks:

- `specs/debug-changestream/memory/task-01.md` — exact table names,
  column definitions, seed rows, and trigger body added to the schema.
  Use this to confirm the exact SQL table/column names before writing
  the `ChangeLogTxnHooks` SQL strings.

## Research Required

Before writing any code, read the following files in full:

- `internal/database/txn/transaction.go` — the `RetryingTxnRunner` type,
  its `Txn`, `StdTxn`, and internal `run`/`commit`/`rollback` methods.
- `internal/database/txn.go` — the package-level shim functions and
  `defaultTransactionRunner`.
- `internal/database/testing/runner.go` — the test-only `postHook`
  pattern, to understand the precedent.
- `internal/database/` — list all files; read any `db.go` or `database.go`
  to understand how `RetryingTxnRunner` is wired into the rest of Juju.
- Search for all call sites of `txn.NewRetryingTxnRunner()` or
  `database.NewRetryingTxnRunner()` to understand where runners are
  constructed and therefore where the hooks must be provided.

## Scope

### Hook interface

Define a `TxnHooks` struct that the runner accepts:

```go
// TxnHooks are optional callbacks invoked around each write
// transaction. Both hooks receive the plain *sql.Tx so they can
// issue SQL directly regardless of whether the caller uses sqlair
// or stdlib.
type TxnHooks struct {
    // Setup is called immediately after BEGIN IMMEDIATE on the
    // write-transaction path, before the user callback is retried.
    // It receives the context passed to Txn/StdTxn. If it returns
    // an error the transaction is rolled back.
    Setup func(ctx context.Context, tx *sql.Tx) error

    // Finalise is called after the user callback returns nil, before
    // COMMIT is issued. It receives the context passed to Txn/StdTxn.
    // If it returns an error the transaction is rolled back.
    Finalise func(ctx context.Context, tx *sql.Tx) error
}
```

`RetryingTxnRunner` gains an optional `hooks TxnHooks` field. A
constructor variant (e.g. `NewRetryingTxnRunnerWithHooks`) accepts them;
the existing `NewRetryingTxnRunner()` constructor remains unchanged and
produces a runner with nil hooks (no-op).

The runner begins every transaction with `BEGIN` (read-only). When the
caller's function returns an `SQLITE_READONLY` error, the runner rolls
back, upgrades to `BEGIN IMMEDIATE`, calls `Setup`, retries the user
function, then calls `Finalise` before `COMMIT`. If the caller never
attempts a write, `Setup` and `Finalise` are never invoked — avoiding
unnecessary round-trips to `change_log_trace_ctx` on read-only
transactions.

### Concrete hook implementation

Create a function `ChangeLogTxnHooks() TxnHooks` in `internal/database`
(or a sub-package) that returns the populated hooks:

**`Setup`** — set the sentinel and write the OTel trace context:

```sql
UPDATE change_log_trace_ctx
    SET is_in_txn = 1, trace_id = ?, span_id = ?;
```

The `trace_id` and `span_id` values are extracted from the `ctx`
argument using `coretrace.ScopeFromContext(ctx)`. If tracing is
inactive (returns `ok = false`), both values are empty strings, but
`is_in_txn` is still set to `1`.

**`Finalise`** — advances the sequence, stamps un-stamped rows, and
resets the sentinel:

```sql
UPDATE change_log_txn_seq SET id = id + 1;
UPDATE change_log
    SET txn_id = (SELECT id FROM change_log_txn_seq)
    WHERE txn_id = 0;
UPDATE change_log_trace_ctx
    SET is_in_txn = 0, trace_id = '', span_id = '';
```

If no `change_log` rows were written during the transaction, the
second `UPDATE` is a no-op — no rows match `txn_id = 0`. A warning
should be logged when the stream encounters `txn_id = 0` rows so
developers can identify out-of-band writes.

### Wiring

Locate the site(s) where Juju constructs the `RetryingTxnRunner` used for
controller and model databases (found during research) and switch them to
use `NewRetryingTxnRunnerWithHooks` with the concrete hooks above.

## Sub-Agent Testing

To prevent context ballooning, delegate all test writing and test
execution to a sub-agent. The sub-agent's write scope is limited to
test files only — it must not modify production code.

When spawning the sub-agent, provide it with:
- The full paths of every production file you have written.
- The full paths of the schema SQL files from task 01 (needed to
  set up a real `DqliteSuite` test database).
- The acceptance criteria from this task.
- The exact `go test` commands to run.

The sub-agent must:
1. Write the unit test(s) described in the acceptance criteria.
2. Run `go test ./internal/database/...` and
   `go test ./internal/database/txn/...` and report any failures.
3. Fix test failures (within test files only) until the suite passes.
4. Report the final `go test` output back to you.

Do not proceed to the Memory File step until the sub-agent reports
a passing test suite.

## Memory File

On completion, write `specs/debug-changestream/memory/task-03.md`
containing:

- The exact name and signature of the `TxnHooks` type and its fields.
- The name of the new constructor and how it differs from the original.
- The exact SQL strings used in `BeforeFirstWrite` and `BeforeCommit`.
- The file path(s) where `RetryingTxnRunner` was modified.
- The call site(s) where the hooks were wired in (file path, line
  context).
- Any deviations from this task spec and the reason.

## Acceptance Criteria

- `RetryingTxnRunner` accepts optional `TxnHooks`.
- Existing callers using `NewRetryingTxnRunner()` are unaffected.
- Read-only transactions (no write attempted) do not invoke `Setup` or
  `Finalise`.
- A new unit test (using `testing.DqliteSuite` and the Task 01 schema)
  verifies:
  - After a transaction that inserts into a watched table, the resulting
    `change_log` rows have a non-zero `txn_id`.
  - All rows produced by the same transaction share the same `txn_id`.
  - Two sequential transactions receive different `txn_id` values.
  - `trace_id` and `span_id` are populated correctly when a tracer scope
    is present in the context, and are empty strings when it is not.
  - `change_log_trace_ctx.is_in_txn` is `0` and `trace_id`/`span_id`
    are empty strings after the transaction commits.
  - A read-only transaction does not modify `change_log_trace_ctx` at
    all.
- `go test ./internal/database/...` passes.
- `go test ./internal/database/txn/...` passes.
