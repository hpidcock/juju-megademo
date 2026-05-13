# Task 03 — Transaction Wrapper Hooks (completed)

## TxnHooks type

Package: `github.com/juju/juju/internal/database/txn`

```go
type TxnHooks struct {
    Setup       func(ctx context.Context, tx *sqlair.TX) error
    Finalise    func(ctx context.Context, tx *sqlair.TX) error
    StdSetup    func(ctx context.Context, tx *sql.Tx) error
    StdFinalise func(ctx context.Context, tx *sql.Tx) error
}
```

- `Setup`/`Finalise` are called on the sqlair (`Txn`) write path;
  they receive `*sqlair.TX` directly, no unsafe needed.
- `StdSetup`/`StdFinalise` are called on the stdlib (`StdTxn`) write
  path; they receive `*sql.Tx`.
- Returning an error from any hook rolls back the transaction.

## New constructor

```go
func NewRetryingTxnRunnerWithHooks(
    hooks TxnHooks, opts ...Option,
) *RetryingTxnRunner
```

Calls `NewRetryingTxnRunner(opts...)` internally and sets `r.hooks`.
The existing `NewRetryingTxnRunner()` is unchanged and produces a
runner with nil hooks (no-op).

## Read-only upgrade logic

Both `Txn` (sqlair) and `StdTxn` (stdlib) follow the same pattern when
the respective hook pair is set:

1. Start a read-only transaction (`ReadOnly: true`).
2. Run the user callback.
3. If the callback returns an `isReadOnlyError` ("attempt to write a
   readonly database"), rollback and call the write-with-hooks path.
4. Write path: `BEGIN`, `Setup`/`StdSetup`, user fn,
   `Finalise`/`StdFinalise`, `COMMIT`.
5. If the callback succeeds on the read-only path, commit directly —
   hooks are never called.

The nil-guard differs by path:
- `Txn`: skips hooks when `hooks.Setup == nil`
- `StdTxn`: skips hooks when `hooks.StdSetup == nil`

## SQL strings used in ChangeLogTxnHooks

Both the sqlair and stdlib pairs execute the same logical SQL.

### Setup / StdSetup

```sql
UPDATE change_log_trace_ctx
SET is_in_txn = 1, trace_id = ?, span_id = ?
```

Parameters: `traceID, spanID` extracted from
`coretrace.ScopeFromContext(ctx)`; both are empty strings when tracing
is inactive. The sqlair pair uses `$M.trace_id`/`$M.span_id`
notation; the stdlib pair uses `?` placeholders.

### Finalise / StdFinalise

```sql
UPDATE change_log_txn_seq SET id = id + 1;

UPDATE change_log
SET txn_id = (SELECT id FROM change_log_txn_seq)
WHERE txn_id = 0;

UPDATE change_log_trace_ctx
SET is_in_txn = 0, trace_id = '', span_id = '';
```

## Files modified

### Production code

- `internal/database/txn/transaction.go`
  — Added `TxnHooks` (four fields), `hooks` field on
    `RetryingTxnRunner`, `NewRetryingTxnRunnerWithHooks`, internal
    helpers `txnOnce`, `txnWithHooks`, `txnWriteWithHooks`,
    `stdTxnOnce`, `stdTxnWithHooks`, `stdTxnWriteWithHooks`,
    `isReadOnlyError`. No `unsafe` import.

- `internal/database/hooks.go` (new file)
  — `ChangeLogTxnHooks() txn.TxnHooks`, sqlair pair
    (`changeLogSqulairSetup`, `changeLogSqulairFinalise`) using
    package-level `sqlair.MustPrepare` statements, and stdlib pair
    (`changeLogSetup`, `changeLogFinalise`) using `ExecContext`.

- `internal/worker/dbaccessor/tracker.go`
  — `trackedDBWorker` gains `runner *txn.RetryingTxnRunner` field.
    `newTrackedDBWorker` creates the runner with
    `txn.NewRetryingTxnRunnerWithHooks(database.ChangeLogTxnHooks(), ...)`.
    `Txn` and `StdTxn` now call `w.runner.Txn` / `w.runner.StdTxn`
    instead of the package-level `database.Txn` / `database.StdTxn`.

### Test code

- `internal/database/hooks_test.go` (new file)
  — `hooksSuite` embeds `dbtesting.DqliteSuite`, applies
    `schema.ModelDDL()` in `SetUpTest`. Tests cover all acceptance
    criteria via a `runWithHooks` helper that calls `StdSetup` and
    `StdFinalise` directly on a plain write transaction.

## Call sites where hooks are wired in

`internal/worker/dbaccessor/tracker.go`, in `newTrackedDBWorker`,
after options are applied and `w.dbTxnMetrics` is set:

```go
runnerOpts := []txn.Option{}
if w.logger != nil {
    runnerOpts = append(runnerOpts, txn.WithLogger(w.logger))
}
w.runner = txn.NewRetryingTxnRunnerWithHooks(
    database.ChangeLogTxnHooks(),
    runnerOpts...,
)
```

The `defaultTransactionRunner` in `internal/database/txn.go` is left
unchanged (no hooks). Bootstrap and pragma code continue to use it and
are unaffected.

## Deviations from spec

- **Separate sqlair/stdlib hook pairs**: The spec defined a single
  `Setup`/`Finalise` pair taking `*sql.Tx`. We instead use two pairs —
  `Setup`/`Finalise` for `*sqlair.TX` and `StdSetup`/`StdFinalise` for
  `*sql.Tx` — because `sqlair.TX` does not expose its underlying
  `*sql.Tx`, making a shared `*sql.Tx` signature impossible without
  `unsafe`. The sqlair pair uses `sqlair.MustPrepare` statements and
  `TX.Query(...).Run()` to execute the same SQL.

- **`isReadOnlyError` uses string matching**: Rather than importing
  the sqlite3 error code directly (which would add a platform-specific
  dependency to the `txn` package), the function checks for the
  canonical SQLite error string "attempt to write a readonly database".
  This works for both `mattn/go-sqlite3` and `go-dqlite`.

- **Test helper invokes stdlib hooks directly**: Because
  `mattn/go-sqlite3` silently allows writes inside `ReadOnly: true`
  transactions (unlike dqlite), the test helper calls `StdSetup` /
  `StdFinalise` directly on a write transaction rather than going
  through the runner's read-only upgrade path. The hook SQL being
  tested is identical to production.
