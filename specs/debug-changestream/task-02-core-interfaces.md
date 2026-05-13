# Task 02 — Core Interface Extensions

## Goal

Extend `core/changestream` with two additive interface changes:

1. Two new methods on `ChangeEvent` — `TraceID()` and `SpanID()` — that
   carry OpenTelemetry trace context alongside a change event.
2. Two new methods on `Term` — `TxnMinID()` and `TxnMaxID()` — that
   expose the transaction ID range covered by the term.

These interface extensions are stubs that later tasks (04, 06) will
implement and consume. They must land before those tasks begin.

## Dependencies

None. This task may begin immediately and must complete before tasks 04
and 06.

## Research Required

Before writing any code, read the following files in full:

- `core/changestream/change.go` — existing `ChangeEvent`, `Term`, and
  `ChangeType` definitions.
- Any `//go:generate` comment or `generate.go` file in
  `core/changestream/` — determines how mocks are regenerated.
- `core/changestream/changestream.go` (if it exists) — any other
  interfaces in the package.
- Search for all implementations of `Term` outside of
  `internal/changestream/stream/` (e.g. test fakes) that will also need
  the two new methods added.

## Scope

### `core/changestream/change.go`

**Extend `ChangeEvent` with trace context methods:**

```go
// TraceID returns the OpenTelemetry trace ID from the originating
// write transaction. Returns an empty string when no trace context
// was captured.
TraceID() string
// SpanID returns the OpenTelemetry span ID from the originating
// write transaction. Returns an empty string when no trace context
// was captured.
SpanID() string
```

No external code implements `ChangeEvent` outside of the changestream
package itself. The concrete `changeEvent` struct in
`internal/changestream/stream/` is covered by task 04.

**Extend the `Term` interface with txn range methods:**

```go
// TxnMinID returns the lowest txn_id present in this term.
TxnMinID() int64
// TxnMaxID returns the highest txn_id present in this term.
TxnMaxID() int64
```

### Stub / no-op updates for `ChangeEvent`

Every existing fake or stub implementation of `ChangeEvent` (found
during research) must have `TraceID()` and `SpanID()` added, returning
`""` until the concrete stream implementation lands in task 04.

### Stub / no-op updates for `Term`

Every existing fake or stub implementation of `Term` (found during
research) must have the two new methods added, returning `0` until the
concrete stream implementation lands in task 04.

Do not modify the concrete `changeEvent` or `term` struct inside
`internal/changestream/stream/` — those are covered by task 04.

### Mock regeneration

After updating the interface, run:

```
go generate ./core/changestream/...
```

Commit the regenerated mock files.

## Sub-Agent Testing

To prevent context ballooning, delegate all test writing and test
execution to a sub-agent. The sub-agent's write scope is limited to
test files only — it must not modify production code or mocks
(mocks are regenerated via `go generate`, not hand-edited).

When spawning the sub-agent, provide it with:
- The full paths of every production file you have written.
- The acceptance criteria from this task.
- The exact `go test` and `go build` commands to run.

The sub-agent must:
1. Run `go generate ./core/changestream/...` to regenerate mocks.
2. Run `go test ./core/changestream/...` and report any failures.
3. Run `go build ./...` to confirm no compilation errors.
4. Fix any failures (within test files or stubs only) until the
   suite passes.
5. Report the final output back to you.

Do not proceed to the Memory File step until the sub-agent reports
a clean build and passing tests.

## Memory File

On completion, write `specs/debug-changestream/memory/task-02.md`
containing:

- The exact method signatures added to `ChangeEvent` and `Term`.
- The file path(s) modified.
- The list of existing `ChangeEvent` and `Term` implementations found
  and updated (file path, type name, stub values returned).
- The `go generate` command used to regenerate mocks.
- Any deviations from this task spec and the reason.

## Acceptance Criteria

- `ChangeEvent` has `TraceID() string` and `SpanID() string`.
- `Term` has `TxnMinID() int64` and `TxnMaxID() int64`.
- All existing consumers of `ChangeEvent` and `Term` compile — stubs
  return `""` or `0` as appropriate.
- Mocks are regenerated and committed.
- `go test ./core/changestream/...` passes.
- `go build ./...` passes (no compilation errors from the interface
  extensions).
