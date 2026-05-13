# Task 12 — Extend `core/trace.Span` with `AddLink`

## Goal

Extend the `core/trace.Span` interface with an `AddLink(traceID,
spanID string)` method. This is required by Task 06, which uses
`AddLink` to link a freshly allocated coalesced-batch trace back to
all of its originating traces, preserving causal history.

## Dependencies

None. This task may begin immediately and must complete before task 06.

## Research Required

Before writing any code, read the following files in full:

- `core/trace/tracer.go` — the `Span` interface and all existing
  methods.
- `core/trace/noop.go` (or equivalent) — the no-op `Span`
  implementation used when no tracer is configured.
- Search for all implementations of `Span` in the codebase (both
  production and test fakes) that will need the new method added.
- Search for any OpenTelemetry SDK integration point that wraps a
  real span — confirm where `AddLink` would delegate to the OTEL
  SDK's `span.AddLink` or equivalent.

## Scope

### `core/trace/tracer.go`

Add `AddLink` to the `Span` interface:

```go
// AddLink records a causal link from this span to another trace.
// This is used when a coalesced batch of changes originates from
// multiple transactions with different trace IDs; the fresh
// batch span links back to each originating trace.
// traceID must be a W3C-format trace ID (32 lower-case hex chars).
// spanID must be a W3C-format span ID (16 lower-case hex chars).
AddLink(traceID, spanID string)
```

### No-op implementation

In the no-op `Span` implementation, add:

```go
func (noopSpan) AddLink(traceID, spanID string) {}
```

### Production OTel wrapper

In the production OTel span wrapper (found during research), add:

```go
func (s *otelSpan) AddLink(traceID, spanID string) {
    // Construct an OTel trace.Link from traceID and spanID and
    // add it to the underlying span.
    // Use go.opentelemetry.io/otel/trace.Link and
    // trace.SpanContextWithRemoteSpan or equivalent.
}
```

### Stub updates for fakes and mocks

Every existing fake or stub implementation of `Span` (found during
research) must have `AddLink` added with a no-op body.

After updating the interface, run:

```
go generate ./core/trace/...
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
1. Run `go generate ./core/trace/...` to regenerate mocks.
2. Run `go test ./core/trace/...` and report any failures.
3. Run `go build ./...` to confirm no compilation errors from the
   interface extension.
4. Fix any failures (within test files or stubs only) until clean.
5. Report the final output back to you.

Do not proceed to the Memory File step until the sub-agent reports
a clean build and passing tests.

## Memory File

On completion, write `specs/debug-changestream/memory/task-12.md`
containing:

- The exact method signature added to `Span`.
- The file path(s) modified.
- The list of `Span` implementations updated (file path, type name).
- The `go generate` command used to regenerate mocks (if applicable).
- Any deviations from this task spec and the reason.

## Acceptance Criteria

- `core/trace.Span` has `AddLink(traceID, spanID string)`.
- All existing implementations of `Span` compile with no-op or
  delegating `AddLink` methods.
- The production OTel wrapper delegates to the underlying SDK span link
  mechanism.
- `go test ./core/trace/...` passes.
- `go build ./...` passes.
