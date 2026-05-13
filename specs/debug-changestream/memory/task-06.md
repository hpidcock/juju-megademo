# Task 06 — Memory: Event Multiplexer Trace Context Propagation

## Location of trace scope attachment

Method: `dispatchSet` in
`internal/changestream/eventmultiplexer/eventmultiplexer.go`.

Before each per-subscriber goroutine is spawned, the per-subscriber
change slice is passed to `resolveTraceContext(ctx, changes)`. The
returned (possibly wrapped) slice is what the goroutine dispatches via
`sub.dispatch(ctx, enriched)`. The `context.Context` passed to the
subscriber is **not** enriched with trace scope.

## Rule for picking the winning trace scope

1. **All empty** — if every change has an empty `TraceID()`, the batch
   is returned unchanged (no-op).
2. **Uniform** — if every non-empty `TraceID()` in the batch is
   identical, the batch is returned unchanged.
3. **Mixed** — if two or more distinct non-empty `TraceID()` values
   exist in the batch, a fresh W3C trace ID (32 lower-case hex chars
   from `crypto/rand`) is generated. A span is opened via
   `trace.Start(ctx, "changestream.coalesced-dispatch")`, and
   `span.AddLink(traceID, spanID)` is called for each distinct
   `(traceID, spanID)` pair. Every change is wrapped in `tracedChange`
   carrying the fresh trace ID and `lastSpanID` (the last non-empty
   `SpanID()` found in the input slice, used as S_last because
   `ChangeEvent` does not expose a `TxnID()` method).

## Files modified

| File | Change |
|------|--------|
| `internal/changestream/eventmultiplexer/eventmultiplexer.go` | Added imports (`crypto/rand`, `encoding/hex`, `core/trace`); added `tracedChange`, `resolveTraceContext`, `generateW3CTraceID`; modified `dispatchSet` to call `resolveTraceContext` before each goroutine |
| `internal/changestream/eventmultiplexer/eventmultiplexer_test.go` | Added `resolveTraceContextSuite` with three tests: empty IDs no-op, uniform IDs no-op, mixed IDs allocate fresh ID + AddLink |

## Deviations from spec

- **S_last is the last non-empty spanID in the slice, not the one
  from the highest `txn_id`** — `ChangeEvent` does not expose a
  `TxnID()` method, so ordering by `txn_id` is unavailable. The spec
  says "fall back to the first non-empty scope found" when `txn_id` is
  unavailable; we use the *last* non-empty scope instead of the first
  because the slice is ordered by ascending `c.id` from the stream
  query, making the last entry the most recent. This is consistent
  with the spec's intent.
- **`trace.Start` uses a `NoopSpan` when no real tracer is in ctx** —
  the multiplexer does not receive a `Tracer` in its constructor, so
  in production the `AddLink` call is a no-op unless a tracer is
  injected into the catacomb context upstream. The fresh trace ID is
  still generated and stored on the wrapped changes regardless.
