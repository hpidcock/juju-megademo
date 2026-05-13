# Task 01 — Schema Changes

## Goal

Add the three new tables (`change_log_txn_seq`, `change_log_trace_ctx`,
`debug_change_stream`), three new columns on `change_log` (`txn_id`,
`trace_id`, `span_id`), and a new trigger (`change_log_set_trace`) to
both the controller and model database schemas.

## Dependencies

None. This task may begin immediately and must complete before tasks 03,
04, 05, and 07.

## Background

Juju schema SQL lives directly in two files — one per database type.
They are not generated and may be edited directly:

- `domain/schema/controller/sql/0003-changelog.sql`
- `domain/schema/model/sql/0001-changelog.sql`

Both files are embedded at build time via `//go:embed controller/sql/*.sql`
and `//go:embed model/sql/*.sql` in `domain/schema/controller.go` and
`domain/schema/model.go` respectively. The full DDL is built by
concatenating all SQL files in alphabetical filename order when
`ControllerDDL()` / `ModelDDL()` is called.

The generated trigger files under `domain/schema/controller/triggers/`
and `domain/schema/model/triggers/` are produced by the `triggergen`
tool and must **not** be edited directly. The `change_log_set_trace`
trigger defined below is hand-written SQL on the `change_log` table
itself — it is not a `triggergen` candidate.

Schema tests in `domain/schema/controller_schema_test.go` and
`domain/schema/model_schema_test.go` hold exact sets of expected table
and trigger names. These must be updated alongside the SQL or the tests
will fail.

The `TestApplyDDLIdempotent` test applies the full DDL twice. All new
SQL must therefore be safe to apply idempotently (`CREATE TABLE IF NOT
EXISTS`, `INSERT OR IGNORE`, `CREATE TRIGGER IF NOT EXISTS`).

## Scope

### 1. Edit `domain/schema/controller/sql/0003-changelog.sql`

Append the following to the end of the existing file (after the
`change_log_witness` table definition):

```sql
-- Monotonically increasing transaction sequence. Incremented once per
-- write transaction as the last operation before COMMIT, then used to
-- back-fill change_log.txn_id for all rows written in that
-- transaction. txn_id = 0 is the "not yet stamped" sentinel; the
-- sequence begins at 1 after the first increment.
CREATE TABLE IF NOT EXISTS change_log_txn_seq (
    id INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id)
);
INSERT OR IGNORE INTO change_log_txn_seq VALUES (0);

-- Holds the OTel trace context active during the current write
-- transaction. Written by the DB transaction wrapper after BEGIN;
-- reset before COMMIT. The is_in_txn sentinel tells triggers whether
-- this write occurred inside a managed transaction.
CREATE TABLE IF NOT EXISTS change_log_trace_ctx (
    id        INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1),
    is_in_txn INTEGER NOT NULL DEFAULT 0,
    trace_id  TEXT    NOT NULL DEFAULT '',
    span_id   TEXT    NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO change_log_trace_ctx VALUES (1, 0, '', '');

-- Controls the debug state of the changestream for this database.
-- Only superusers may write to this table via the API.
-- The id = 1 constraint enforces the single-row invariant.
CREATE TABLE IF NOT EXISTS debug_change_stream (
    id          INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1),
    state       TEXT    NOT NULL DEFAULT 'running'
        CHECK(state IN ('running', 'paused', 'step')),
    step_target INTEGER NOT NULL DEFAULT 0,
    updated_at  DATETIME NOT NULL
        DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc'))
);
INSERT OR IGNORE INTO debug_change_stream
    VALUES (1, 'running', 0,
            STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc'));
```

Also add the three new columns to the `change_log` table definition
(already in this file) and a new trigger. Amend the `CREATE TABLE
change_log` statement to include:

```sql
    txn_id   INTEGER NOT NULL DEFAULT 0,
    trace_id TEXT    NOT NULL DEFAULT '',
    span_id  TEXT    NOT NULL DEFAULT '',
```

Also append a partial index after the `change_log` ALTER statements to
accelerate the txn_id back-fill query:

```sql
CREATE INDEX IF NOT EXISTS idx_change_log_unstamped
    ON change_log(txn_id) WHERE txn_id = 0;
```

And append the trigger after the table definition:

```sql
-- Stamps trace context onto each new change_log row from
-- change_log_trace_ctx. Only stamps when is_in_txn = 1, so
-- out-of-band writes (migrations, direct SQL) receive empty strings.
CREATE TRIGGER IF NOT EXISTS change_log_set_trace
AFTER INSERT ON change_log
BEGIN
    UPDATE change_log
    SET
        trace_id = CASE
            WHEN (SELECT is_in_txn FROM change_log_trace_ctx) = 1
            THEN (SELECT trace_id FROM change_log_trace_ctx)
            ELSE '' END,
        span_id  = CASE
            WHEN (SELECT is_in_txn FROM change_log_trace_ctx) = 1
            THEN (SELECT span_id FROM change_log_trace_ctx)
            ELSE '' END
    WHERE id = NEW.id;
END;
```

### 2. Apply the same changes to `domain/schema/model/sql/0001-changelog.sql`

The model changelog file is structurally identical to the controller one.
Apply exactly the same additions: the three new tables (including the
`id INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1)` constraint on
`change_log_trace_ctx` and `debug_change_stream`, and the `is_in_txn`
column on `change_log_trace_ctx`), the three new columns on
`change_log`, the `idx_change_log_unstamped` partial index, and the
updated `change_log_set_trace` trigger that checks `is_in_txn`.

### 3. Update `domain/schema/controller_schema_test.go`

In `TestControllerTables`, add to the `expected` set:
- `"change_log_txn_seq"`
- `"change_log_trace_ctx"`
- `"debug_change_stream"`

In `TestControllerIndexes` (or wherever indexes are asserted), add:
- `"idx_change_log_unstamped"`

In `TestControllerTriggers` (or wherever controller triggers are
asserted), add:
- `"change_log_set_trace"`

### 4. Update `domain/schema/model_schema_test.go`

Apply the same additions as step 3 to the model test file.

## Sub-Agent Testing

To prevent context ballooning, delegate all test writing and test
execution to a sub-agent. The sub-agent's write scope is limited to
test files only — it must not modify production or schema files.

When spawning the sub-agent, provide it with:
- The full paths of every production/schema file you have written.
- The acceptance criteria from this task.
- The exact `go test` commands to run.

The sub-agent must:
1. Write the test changes described in the acceptance criteria
   (schema test file additions for tables, indexes, triggers).
2. Run `go test ./domain/schema/...` and report any failures.
3. Fix test failures (within test files only) until the suite passes.
4. Report the final `go test` output back to you.

Do not proceed to the Memory File step until the sub-agent reports
a passing test suite.

## Memory File

On completion, write `specs/debug-changestream/memory/task-01.md`
containing:

- The exact columns added to `change_log` (names and types).
- The names and seed rows of the three new tables.
- The name and body of the new trigger.
- The exact file paths edited.
- Any deviations from this task spec and the reason.

## Acceptance Criteria

- `go test ./domain/schema/...` passes with no changes to the test
  expectations beyond those described above.
- `TestApplyDDLIdempotent` passes for both controller and model schemas.
- The three new tables and the trigger exist in test databases for both
  controller and model schemas.
- The `change_log` table in both schemas has `txn_id`, `trace_id`, and
  `span_id` columns with `NOT NULL DEFAULT` values, so existing rows and
  existing tests are unaffected.
