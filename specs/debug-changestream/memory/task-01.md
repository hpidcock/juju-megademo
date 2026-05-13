# Task 01 — Schema Changes (completed)

## Columns added to `change_log`

| Name      | Type    | Constraint              |
|-----------|---------|-------------------------|
| `txn_id`  | INTEGER | NOT NULL DEFAULT 0      |
| `trace_id`| TEXT    | NOT NULL DEFAULT ''     |
| `span_id` | TEXT    | NOT NULL DEFAULT ''     |

## New tables and seed rows

### `change_log_txn_seq`
- Columns: `id INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (id)`
- Seed: `INSERT OR IGNORE INTO change_log_txn_seq VALUES (0)`

### `change_log_trace_ctx`
- Columns: `id INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1)`,
  `is_in_txn INTEGER NOT NULL DEFAULT 0`,
  `trace_id TEXT NOT NULL DEFAULT ''`,
  `span_id TEXT NOT NULL DEFAULT ''`
- Seed: `INSERT OR IGNORE INTO change_log_trace_ctx VALUES (1, 0, '', '')`

### `debug_change_stream`
- Columns: `id INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1)`,
  `state TEXT NOT NULL DEFAULT 'running' CHECK(state IN ('running', 'paused', 'step'))`,
  `step_target INTEGER NOT NULL DEFAULT 0`,
  `updated_at DATETIME NOT NULL DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc'))`
- Seed: `INSERT OR IGNORE INTO debug_change_stream VALUES (1, 'running', 0, STRFTIME(...))`

## Partial index

`idx_change_log_unstamped` on `change_log(txn_id) WHERE txn_id = 0`

## New trigger

Name: `change_log_set_trace`

Fires AFTER INSERT ON `change_log`. Updates `trace_id` and `span_id` on
the new row by reading from `change_log_trace_ctx`. Values are only
stamped when `is_in_txn = 1`; otherwise they remain empty strings.

## Files edited

- `domain/schema/controller/sql/0003-changelog.sql`
- `domain/schema/model/sql/0001-changelog.sql`
- `domain/schema/controller_schema_test.go`
- `domain/schema/model_schema_test.go`

## Deviations from spec

None. All changes applied exactly as described.
