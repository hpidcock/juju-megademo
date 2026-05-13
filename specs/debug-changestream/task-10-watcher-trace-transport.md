# Task 10 — Watcher Interface: `ChangeContext` and API Trace Transport

## Goal

Propagate OTel trace context from `ChangeEvent` all the way to
consumers of a `Watcher[T]`, both in-process and across the RPC
boundary.

Three distinct layers are involved:

1. **`core/watcher`** — add `ChangeContext` to the `Watcher[T]`
   interface.
2. **`core/watcher/eventsource`** — implement `ChangeContext` on
   `BaseWatcher`; have `NamespaceWatcher` and `NotifyWatcher` cache
   the last trace context when they receive events.
3. **RPC transport** — add `TraceID`/`SpanID` fields to every
   `XXXWatchResult` in `rpc/params`; populate them in the server-side
   `Next()` handlers in `apiserver/watcher.go`; store and expose them
   in the client-side watchers in `api/watcher`.

## Dependencies

- **Task 02** — `ChangeEvent` must have `TraceID()` and `SpanID()`.
- **Task 06** — `BaseWatcher.setLastTrace` applies the same coalescing
  logic used by the event multiplexer; task 06 must be complete so
  that logic is established and can be referenced.

This task may run in parallel with task 07. It must complete before
the project is considered done; tasks 08 and 09 do not depend on it
directly (their write scopes are disjoint), but all should be complete
before final integration testing.

## Blast Radius Note

The `Watcher[T]` interface change and the addition of `ChangeContext` to
every concrete watcher type has a large blast radius across the
codebase. Use sub-agents to parallelize the mechanical updates:

- One agent for `core/watcher` and `core/watcher/eventsource`.
- One agent for `api/watcher` and `apiserver/watcher.go`.
- One agent for `rpc/params` `WatchResult` struct additions.

Each agent has a disjoint write scope and can proceed in parallel once
the interface definition (first bullet) is agreed.

## Memory Files to Read

Before writing any code, read the following memory files from
completed dependency tasks:

- `specs/debug-changestream/memory/task-02.md` — exact `TraceID()`
  and `SpanID()` method signatures on `ChangeEvent`, and every stub
  implementation updated. Confirms the interface shape that
  `setLastTrace` must use when inspecting events.
- `specs/debug-changestream/memory/task-06.md` — exact coalescing
  rule implemented in the event multiplexer (same-trace vs
  mixed-trace logic). The `setLastTrace` implementation in
  `BaseWatcher` must mirror this rule precisely.

## Research Required

Before writing any code, read the following:

- `core/watcher/watcher.go` — existing `Watcher[T]` interface.
- `core/watcher/eventsource/base.go` — `BaseWatcher` struct and all
  its existing methods.
- `core/watcher/eventsource/namespace.go` — `NamespaceWatcher.loop()`
  to find where events are received from the subscription channel.
- `core/watcher/eventsource/notify.go` — `NotifyWatcher.loop()` and
  `drainInitialEvent`.
- All other files in `core/watcher/eventsource/` — identify any
  additional watcher types that embed `BaseWatcher` or implement
  `Watcher[T]` directly.
- `api/watcher/watcher.go` — `commonWatcher`, `commonLoop`, and all
  concrete watcher types (`stringsWatcher`, `notifyWatcher`, and any
  others). Note the struct layout and how `w.in` is consumed.
- `apiserver/watcher.go` — all `srvXxxWatcher` types, their `Next()`
  methods, and how `internal.FirstResult` is used.
- `rpc/params/internal.go` — all `XXXWatchResult` structs defined
  there.
- Search `rpc/params/` for any `WatchResult` structs defined in other
  files (e.g. `crossmodel.go`, `secrets.go`).
- `core/trace/context.go` — `WithTraceScope` and `ScopeFromContext`
  signatures.
- The completed task-06 memory file at
  `specs/debug-changestream/memory/task-06.md` — to understand the
  exact coalescing rule used by the event multiplexer so `setLastTrace`
  mirrors it precisely.

## Scope

### `core/watcher/watcher.go`

Add `ChangeContext` to the `Watcher[T]` interface:

```go
// Watcher defines a worker that emits changes for a given type T.
type Watcher[T any] interface {
    worker.Worker

    // Changes returns a channel of type T, closed when the watcher
    // stops.
    Changes() <-chan T

    // ChangeContext returns a new context derived from parent,
    // enriched with the OTel trace ID and span ID for the last
    // value dispatched on Changes(). If no value has been received
    // yet, or no trace context was captured for that value, parent
    // is returned unchanged.
    ChangeContext(parent context.Context) context.Context
}
```

### `core/watcher/eventsource/base.go`

Add two mutex-protected fields to `BaseWatcher` and implement
`ChangeContext`:

```go
type BaseWatcher struct {
    tomb        tomb.Tomb
    watchableDB changestream.WatchableDB
    logger      logger.Logger

    // mu guards lastTraceID and lastSpanID.
    mu          sync.Mutex
    lastTraceID string
    lastSpanID  string
}

// ChangeContext implements watcher.Watcher.
func (w *BaseWatcher) ChangeContext(
    parent context.Context,
) context.Context {
    w.mu.Lock()
    traceID, spanID := w.lastTraceID, w.lastSpanID
    w.mu.Unlock()
    if traceID == "" {
        return parent
    }
    return coretrace.WithTraceScope(parent, traceID, spanID, 0)
}
```

Add `setLastTrace`, a private helper that applies the same coalescing
rule as the event multiplexer (from task 06):

```go
// setLastTrace caches the trace context from a batch of events.
// If all events share the same TraceID, that ID and the SpanID of
// the highest-txn_id event are stored. If TraceIDs differ across
// the batch, lastTraceID is cleared so ChangeContext returns the
// parent unchanged, deferring traceability to the multiplexer path.
// Use value receivers on any concrete ChangeEvent type passed here
// for consistency with the changeEvent receiver style.
func (w *BaseWatcher) setLastTrace(
    events []changestream.ChangeEvent,
) {
    ...
}
```

The implementation should find the event with the highest `txn_id`
(if available via a type assertion to the traced interface from task
02). If no event implements the traced interface, or all `TraceID`
values are empty, clear both stored fields.

### `core/watcher/eventsource/namespace.go`

In `NamespaceWatcher.loop()`, call `w.setLastTrace(subChanges)`
immediately after receiving a non-empty batch from the subscription
channel, before ticking to dispatch mode. The call sits alongside the
existing mapper call:

```go
case subChanges, ok := <-in:
    ...
    changed, err := w.mapper(ctx, subChanges)
    ...
    if len(changed) == 0 {
        continue
    }
    w.setLastTrace(subChanges)
    changes = changed
    in = nil
    out = w.out
```

### `core/watcher/eventsource/notify.go`

Apply the same treatment in `NotifyWatcher.loop()` when ticking to
dispatch mode. Do not call `setLastTrace` in `drainInitialEvent` —
the initial drain discards the event intentionally and should not
influence the stored trace context.

### Other eventsource watcher types

For any additional watcher type found during research that embeds
`BaseWatcher`, apply the same `setLastTrace` call at the equivalent
point in its loop.

### `rpc/params` — `XXXWatchResult` additions

For every `XXXWatchResult` struct found during research, add:

```go
TraceID string `json:"trace-id,omitempty"`
SpanID  string `json:"span-id,omitempty"`
```

Place the two fields after the `Changes` (or equivalent payload) field
and before `Error`. The `omitempty` tag is required on both fields to
preserve wire compatibility with older clients and servers.

Apply this to structs in `internal.go` and any other `rpc/params`
file where `WatchResult` structs appear. Do not create a new file;
edit the existing files in place.

### `apiserver/watcher.go` — server-side `Next()` handlers

After `internal.FirstResult` drains the batch, call `ChangeContext`
on the registered watcher to extract the trace IDs and embed them in
the result. The pattern for every `srvXxxWatcher.Next()` is:

```go
func (w *srvStringsWatcher) Next(
    ctx context.Context,
) (params.StringsWatchResult, error) {
    changes, err := internal.FirstResult[[]string](ctx, w.watcher)
    if err != nil {
        return params.StringsWatchResult{}, errors.Trace(err)
    }
    traceCtx := w.watcher.ChangeContext(ctx)
    traceID, spanID, _, _ := coretrace.ScopeFromContext(traceCtx)
    return params.StringsWatchResult{
        Changes: changes,
        TraceID: traceID,
        SpanID:  spanID,
    }, nil
}
```

Apply the same change to every other `srvXxxWatcher.Next()` method
found in the file, adapting the concrete `WatchResult` type as
appropriate. For `srvNotifyWatcher.Next()` (which returns only an `error` today),
convert it to return `(params.NotifyWatchResult, error)` and populate
the trace fields. This is an intentional breaking change — notify
watchers should have carried a result type from the start. Update all
callers of the old signature.

### `api/watcher/watcher.go` — client-side watcher changes

For each concrete watcher type (`stringsWatcher`, `notifyWatcher`,
and any others found during research):

1. Add `mu sync.Mutex`, `lastTraceID string`, `lastSpanID string`
   fields.
2. Implement `ChangeContext` with the same mutex-protected pattern as
   `BaseWatcher`.
3. In `loop()`, after pulling a result from `w.in`, store the trace
   IDs before forwarding the payload to `w.out`:

```go
result := data.(*params.StringsWatchResult)
changes = result.Changes
w.mu.Lock()
w.lastTraceID = result.TraceID
w.lastSpanID = result.SpanID
w.mu.Unlock()
```

The initial changes passed into `loop()` carry no trace context
(they came from the initial `WatchResult`, which is populated from
a database query). Do not attempt to extract trace IDs from the
initial value; the stored fields start as empty strings.

## Sub-Agent Testing

To prevent context ballooning, this task already recommends spawning
sub-agents for the blast-radius work (see the Blast Radius Note above).
Extend that delegation to cover testing as well.

For each of the three sub-agents described in the blast radius note,
include testing in their scope:

**Sub-agent 1 — `core/watcher` and `core/watcher/eventsource`**
- Write scope: `core/watcher/` and `core/watcher/eventsource/`
  test files.
- After writing production code, write tests for `BaseWatcher
  .ChangeContext` (no trace set, uniform trace ID, mixed trace IDs)
  and the `setLastTrace` coalescing rule.
- Run: `go test ./core/watcher/...` and
  `go test ./core/watcher/eventsource/...`.

**Sub-agent 2 — `api/watcher` and `apiserver/watcher.go`**
- Write scope: `api/watcher/` and `apiserver/` test files.
- After writing production code, write tests for the client-side
  `stringsWatcher.ChangeContext` round-trip.
- Run: `go test ./api/watcher/...` and `go test ./apiserver/...`.

**Sub-agent 3 — `rpc/params` `WatchResult` struct additions**
- Write scope: `rpc/params/` files (production edits only — no
  dedicated test file needed; compilation is the acceptance test).
- Run: `go build ./rpc/params/...`.

Do not proceed to the Memory File step until all sub-agents report
passing tests and a clean build.

## Memory File

On completion, write `specs/debug-changestream/memory/task-10.md`
containing:

- The exact `setLastTrace` implementation (coalescing rule and any
  edge cases).
- The full list of `XXXWatchResult` structs modified and the files
  they live in.
- The full list of `srvXxxWatcher.Next()` methods modified.
- The full list of client-side watcher types modified in
  `api/watcher/watcher.go`.
- Whether `srvNotifyWatcher.Next()` required a signature change and
  how any callers were updated.
- Any deviations from this task spec and the reason.

## Acceptance Criteria

- `Watcher[T]` in `core/watcher` has `ChangeContext`.
- `BaseWatcher` implements `ChangeContext`; `setLastTrace` caches
  trace IDs from incoming event batches.
- `NamespaceWatcher` and `NotifyWatcher` call `setLastTrace` at the
  correct point in their loops.
- Every `XXXWatchResult` in `rpc/params` has `TraceID` and `SpanID`
  with `omitempty`.
- Every `srvXxxWatcher.Next()` in `apiserver/watcher.go` populates
  the trace fields.
- Every concrete client-side watcher type in `api/watcher` implements
  `ChangeContext` and stores trace IDs from each received result.
- `go test ./core/watcher/...` passes.
- `go test ./core/watcher/eventsource/...` passes.
- `go test ./apiserver/...` passes.
- `go test ./api/watcher/...` passes.
- A unit test for `BaseWatcher.ChangeContext` verifies: returns parent
  when no trace has been set; returns enriched context after
  `setLastTrace` with a uniform trace ID; returns parent after
  `setLastTrace` with mixed trace IDs.
- A unit test for the client-side `stringsWatcher.ChangeContext`
  verifies it returns the IDs from the most recently received
  `StringsWatchResult`.
