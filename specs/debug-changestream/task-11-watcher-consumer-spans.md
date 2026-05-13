# Task 11 — Watcher Consumers: Start a Child Span on Each Change

## Goal

Every worker loop that reacts to a `watcher.Changes()` event should
open a new OTel span that is causally linked to the database transaction
that produced the change. This is done by passing
`watcher.ChangeContext(ctx)` as the parent context to `trace.Start`,
giving the span the correct parent regardless of whether the watcher is
an in-process eventsource watcher or an API-transported one.

## Dependencies

- **Task 10** — `ChangeContext` must be implemented on the `Watcher[T]`
  interface and on all concrete watcher types before this task can begin.

## Memory Files to Read

Before writing any code, read the following memory files from
completed dependency tasks:

- `specs/debug-changestream/memory/task-10.md` — exact
  `ChangeContext` method signature on the `Watcher[T]` interface and
  on every concrete watcher type, plus any caveats about when the
  method returns the parent context unchanged. Required before
  calling `watcher.ChangeContext(ctx)` in worker loop `case` blocks.

## Research Required

Before making any changes, do the following:

1. Run a search for all `case` clauses that read from a watcher in
   `internal/worker/`:

   ```
   grep -r "\.Changes():" internal/worker/ --include="*.go" -l
   ```

   This gives you the complete file list. Skim each file to understand
   the shape of the loop.

2. Read `core/trace/tracer.go` to confirm the `trace.Start` signature:

   ```go
   func Start(
       ctx context.Context,
       name Name,
       options ...Option,
   ) (context.Context, Span)
   ```

3. Read one fully-traced example — `internal/worker/lease/manager.go`
   `withTrace` and `internal/worker/uniter/operation/executor.go`
   `Run` — to see the idiomatic span lifecycle:

   ```go
   ctx, span := trace.Start(ctx, trace.NameFromFunc())
   defer func() {
       span.RecordError(err)
       span.End()
   }()
   ```

4. Note that `trace.Start` falls back to a noop span when no tracer is
   present in the context. Calling it is always safe — workers without a
   configured tracer incur negligible overhead.

## Scope

### Pattern to apply

For each worker loop `case` that reads from `watcher.Changes()`,
replace the existing handling with a scoped span. The canonical form
for a named handler function is:

```go
case _, ok := <-watcher.Changes():
    if !ok {
        return errors.New("watcher closed")
    }
    if err := w.handleChange(
        watcher.ChangeContext(ctx),
    ); err != nil {
        return errors.Capture(err)
    }
```

Where `handleChange` opens the span internally:

```go
func (w *myWorker) handleChange(ctx context.Context) (err error) {
    ctx, span := trace.Start(ctx, trace.NameFromFunc())
    defer func() {
        span.RecordError(err)
        span.End()
    }()

    // ... do work with ctx ...
}
```

For loops where the work is written inline rather than delegated to a
named function, open and close the span within the `case` block:

```go
case _, ok := <-watcher.Changes():
    if !ok {
        return errors.New("watcher closed")
    }
    ctx, span := trace.Start(
        watcher.ChangeContext(ctx),
        trace.NameFromFunc(),
    )
    err := doWork(ctx)
    span.RecordError(err)
    span.End()
    if err != nil {
        return errors.Capture(err)
    }
```

Do **not** use `defer span.End()` in a loop body — `defer` runs at
function return, not at the end of the `case` block. Close the span
explicitly before the `case` block exits.

### Where to apply it

Apply the pattern to every file in `internal/worker/` where a select
loop reads from `watcher.Changes()`. The change is purely additive —
no logic changes, only span instrumentation wrapping the existing work.

Do not apply the pattern to:
- Test files (`_test.go`).
- Cases that only set a channel to enable/disable dispatching (i.e.
  cases that do no work themselves, only flip a flag).
- The `eventsource` watcher loops themselves — these are the source of
  changes, not consumers.

### Span naming

Prefer `trace.NameFromFunc()` when the work is already in a named
handler function, so the span name is automatically the function name.
For inline work, use a short descriptive literal string:

```go
trace.Name("handle-config-change")
```

### Attributes

Add at minimum:
```go
trace.WithAttributes(
    trace.StringAttr("worker", "my-worker-name"),
)
```

Where a meaningful entity identifier is available (e.g. model UUID,
application name, unit tag), include it as an additional attribute.

## Sub-Agent Testing

To prevent context ballooning, delegate all test execution to a
sub-agent. Because this task makes purely additive changes across
many files, the sub-agent's role is verification rather than writing
new test files.

When spawning the sub-agent, provide it with:
- The full list of files you have modified.
- The acceptance criteria from this task.

The sub-agent must:
1. Run `go build ./internal/worker/...` and report any compile
   errors.
2. Run `go test ./internal/worker/...` and report any failures.
3. Confirm no spans are left open across loop iterations (no `defer
   span.End()` inside a `for` body in the files you modified).
4. Fix any compilation or test failures (within the modified files
   only) until the suite passes.
5. Report the final `go test` output back to you.

Do not proceed to the Memory File step until the sub-agent reports
a passing build and test suite.

## Memory File

On completion, write `specs/debug-changestream/memory/task-11.md`
containing:

- The total number of files modified.
- The list of files and, for each, the watcher variable name(s) and
  whether the work was inline or delegated to a handler.
- Any cases skipped and why.
- Any deviations from this task spec and the reason.

## Acceptance Criteria

- Every `case <-watcher.Changes():` block in `internal/worker/` that
  performs work opens a `trace.Start` span using
  `watcher.ChangeContext(ctx)` as the parent context.
- No spans are left open across loop iterations (no stray `defer`
  inside a `for` body).
- `go build ./internal/worker/...` passes.
- `go test ./internal/worker/...` passes (no new test failures).
- The change is purely additive — no pre-existing behaviour is altered.
