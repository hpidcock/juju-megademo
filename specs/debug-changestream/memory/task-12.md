# Task 12 — Memory: Extend `core/trace.Span` with `AddLink`

## Method signature added to `Span`

```juju/core/trace/tracer.go#L117-122
// AddLink records a causal link from this span to another trace.
// This is used when a coalesced batch of changes originates from
// multiple transactions with different trace IDs; the fresh batch
// span links back to each originating trace.
// traceID must be a W3C-format trace ID (32 lower-case hex chars).
// spanID must be a W3C-format span ID (16 lower-case hex chars).
AddLink(traceID, spanID string)
```

## Files modified

- `core/trace/tracer.go` — `AddLink` added to `Span` interface.
- `core/trace/context.go` — no-op `AddLink` added to `NoopSpan`.
- `core/trace/context_test.go` — no-op `AddLink` added to `stubSpan`.
- `internal/worker/trace/client.go` — `AddLink(link trace.Link)` added to
  `ClientSpan` interface (OTel SDK span already implements it).
- `internal/worker/trace/tracer.go` — delegating `AddLink` implemented on
  `managedSpan`.

## Span implementations updated

| File | Type | Change |
|---|---|---|
| `core/trace/context.go` | `NoopSpan` | no-op `AddLink(string, string)` |
| `core/trace/context_test.go` | `stubSpan` | no-op `AddLink(string, string)` |
| `internal/worker/trace/tracer.go` | `managedSpan` | delegating `AddLink` via OTel `trace.Link` |
| `internal/worker/trace/tracer.go` | `limitedSpan` | inherited from embedded `managedSpan` via `coretrace.Span` |

## `managedSpan.AddLink` implementation detail

Parses `traceID` and `spanID` with `trace.TraceIDFromHex` /
`trace.SpanIDFromHex`. On parse error the call is silently ignored.
Constructs an OTel `trace.SpanContext` with `Remote: true` and
`TraceFlags: trace.FlagsSampled`, then calls
`s.span.AddLink(trace.Link{SpanContext: sc})`.

## go generate

No `go:generate` directives exist in `core/trace`. The mock in
`internal/worker/trace/trace_mock_test.go` is generated for the OTel
`go.opentelemetry.io/otel/trace.Span` interface (which already had
`AddLink`), not for `core/trace.Span`, so no regeneration was needed.

## Deviations from spec

None. All acceptance criteria met:
- `core/trace.Span` has `AddLink(traceID, spanID string)`.
- All implementations compile with no-op or delegating `AddLink`.
- `managedSpan` delegates to the OTel SDK via `trace.Link`.
- `go test ./core/trace/...` passes.
- `go build ./...` passes.
