# Task 06 — Event Multiplexer: Trace Context Propagation

## Goal

When the event multiplexer dispatches a term to subscribers, handle
causal linking for coalesced batches via `core/trace.Span.AddLink`.
Trace propagation to consumers relies solely on `ChangeEvent.TraceID()`
and `SpanID()` methods — the multiplexer does **not** enrich the
`context.Context` passed to the subscriber.

## Dependencies

- **Task 02** — `ChangeEvent` must have `TraceID()` and `SpanID()` methods.
- **Task 04** — `changeEvent` must implement those methods with real
  values from the database.

This task may begin once tasks 02 and 04 are complete. It is independent
of tasks 05 and 07.

## Memory Files to Read

Before writing any code, read the following memory files from
completed dependency tasks:

- `specs/debug-changestream/memory/task-02.md` — exact `TraceID()`
  and `SpanID()` method signatures on `ChangeEvent`. Confirms the
  interface shape used to inspect trace context on each change.
- `specs/debug-changestream/memory/task-04.md` — confirms that
  concrete `changeEvent` structs carry real `traceID`/`spanID`
  values, and how coalescing by `MAX(c.id)` interacts with the trace
  fields (the `txn_id` of the representative row).
- `specs/debug-changestream/memory/task-12.md` — exact `AddLink`
  method signature on `core/trace.Span`. Required before calling
  `span.AddLink(traceID, spanID)` in the multiplexer.

## Research Required

Before writing any code, read the following files in full:

- `internal/changestream/eventmultiplexer/eventmultiplexer.go` — the
  `dispatchSet` and `dispatch` methods, and how `context.Context` is
  threaded through the dispatch call.
- `internal/changestream/eventmultiplexer/subscription.go` — the
  `dispatch` method on `subscription`.
- `core/trace/context.go` — `WithTraceScope` signature and behaviour.
- A concrete example of `WithTraceScope` being called in production code
  (search the codebase for existing usages).

## Scope

### `internal/changestream/eventmultiplexer/eventmultiplexer.go`

In `dispatchSet` (or wherever the per-subscriber change batch is
assembled), inspect the trace scopes of the changes destined for each
subscriber:

1. Identify the most recent change by highest `txn_id`; its `SpanID()`
   is `S_last`.
2. Check whether all changes in the batch share the same `TraceID()`.
   - If **yes**: no action required. The shared `(traceID, S_last)` is
     already consistent across the batch.
   - If **no** (coalesced changes from transactions with different trace
     IDs): allocate a fresh trace ID (W3C format: 32 lower-case hex
     characters generated with `crypto/rand` + `encoding/hex`). Open a
     new span with the fresh trace ID, then call `span.AddLink(traceID,
     spanID)` for each distinct originating trace to preserve causal
     links. The fresh `(traceID, S_last)` is stored back onto the
     changes so downstream consumers see a consistent batch.

If all changes have empty `TraceID()` and `SpanID()`, skip the causal
link logic entirely.

The subscriber's `dispatch` call receives the original (unenriched)
context. Consumers access trace context via `watcher.ChangeContext()`
(implemented in Task 10).

### Handling multiple trace scopes per term

A term may coalesce changes from multiple transactions (each with a
different `txn_id` and potentially a different `traceID`). The correct
behaviour is:

- **Same `traceID` across all changes**: no action needed. The shared
  `(traceID, spanID_last)` is already consistent.
- **Mixed `traceID` values**: allocate a fresh W3C trace ID (32
  lower-case hex chars via `crypto/rand` + `encoding/hex`). Open a
  span with the fresh ID, call `span.AddLink(traceID, spanID)` for
  every distinct originating trace. Store the fresh `(traceID,
  spanID_last)` back onto the change batch so all downstream
  consumers see a coherent trace.

If `txn_id` is zero or unavailable, fall back to the first non-empty
scope found.

## Sub-Agent Testing

To prevent context ballooning, delegate all test writing and test
execution to a sub-agent. The sub-agent's write scope is limited to
test files only — it must not modify production code.

When spawning the sub-agent, provide it with:
- The full paths of every production file you have written.
- The acceptance criteria from this task.
- The exact `go test` commands to run.

The sub-agent must:
1. Write the new tests described in the acceptance criteria (mixed
   trace IDs allocate a fresh ID + AddLink; uniform trace IDs no-op;
   empty IDs no-op).
2. Run `go test ./internal/changestream/eventmultiplexer/...` and
   report any failures.
3. Fix test failures (within test files only) until the suite passes.
4. Report the final `go test` output back to you.

Do not proceed to the Memory File step until the sub-agent reports
a passing test suite.

## Memory File

On completion, write `specs/debug-changestream/memory/task-06.md`
containing:

- The exact location in `eventmultiplexer.go` where the trace scope is
  attached (method name, brief description of the change).
- The rule used to pick which trace scope wins when a term has changes
  from multiple transactions.
- The file path(s) modified.
- Any deviations from this task spec and the reason.

## Acceptance Criteria

- When a subscriber receives a dispatch, the `context.Context` passed
  to it is **not** enriched with trace scope.
- When changes carry mixed `traceID` values, a fresh W3C trace ID is
  allocated, a span is opened, and `span.AddLink` is called for each
  distinct originating trace.
- When all changes share the same `traceID`, no new trace is created.
- When all changes have empty `traceID`/`spanID`, no action is taken.
- The change to `dispatchSet` / `dispatch` does not alter the set of
  subscribers that receive changes (no functional regression).
- `go test ./internal/changestream/eventmultiplexer/...` passes.
- A new test verifies that when changes carry mixed `traceID` values, a
  fresh W3C trace ID is allocated and `AddLink` is called for each
  distinct originating trace.
- A new test verifies that when all changes share the same `traceID`,
  no new trace is created.
- A new test verifies that no action is taken when all changes have
  empty IDs.
