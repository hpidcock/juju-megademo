# Task 04 — Stream Worker: Query Extensions and Trace Fields (completed)

## Updated `selectQuery`

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

SQLite's bare-column rule guarantees that non-aggregated columns in a
query containing `MIN`/`MAX` come from the row holding the extremum.
`c.txn_id`, `c.trace_id`, and `c.span_id` therefore come from the row
with the highest `c.id` in each `(namespace_id, changed)` group —
exactly the coalesced row we want. No subquery was needed.

## Final `changeEvent` struct fields

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

`rows.Scan` order matches the SELECT list:
`id`, `changeType`, `namespace`, `changed`, `createdAt`,
`txnID`, `traceID`, `spanID`.

## `txnMin` / `txnMax` computation

`Term` gains two unexported fields `txnMin int64` and `txnMax int64`.

In the `loop`, after `readChanges` returns the `[]changeEvent` slice,
the existing `lower`/`upper` iteration is extended with two local
variables initialised to `math.MaxInt64` and `math.MinInt64`
respectively. Each `change.txnID` is compared against them. After the
loop:

```go
if txnMin != math.MaxInt64 { term.txnMin = txnMin }
if txnMax != math.MinInt64 { term.txnMax = txnMax }
```

The guards preserve zero-value semantics when all `txn_id` values are 0
(e.g. writes that pre-date the hooks or test fixtures that do not set
`txn_id`).

## Files modified

| File | Change |
|------|--------|
| `internal/changestream/stream/stream.go` | `selectQuery` extended; `changeEvent` fields added; `TraceID`/`SpanID` now return real fields; `Term` gains `txnMin`/`txnMax`; `TxnMinID`/`TxnMaxID` stubs replaced; `readChanges` scans new columns; `loop` computes `txnMin`/`txnMax` |
| `internal/changestream/stream/stream_test.go` | Two new tests: `TestReadChangesTraceIDAndSpanID`, `TestTermTxnMinIDAndTxnMaxID` |

## Deviations from spec

- **`trace_id`/`span_id` round-trip test uses `UPDATE` instead of
  direct `INSERT`**: The schema has a `change_log_set_trace` trigger
  (`AFTER INSERT ON change_log`) that unconditionally overwrites
  `trace_id` and `span_id` from `change_log_trace_ctx`. Because test
  transactions do not go through the hook runner, `is_in_txn` is always
  0 and the trigger resets the fields to `''`. The test therefore
  inserts the row first, then updates `trace_id`/`span_id` directly,
  bypassing the trigger.
