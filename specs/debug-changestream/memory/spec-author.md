# Spec Author Memory

## Latest Changes

All 12 task files were updated with two new structural sections:

1. **`## Memory Files to Read`** — inserted after `## Dependencies` in
   every task that has upstream dependencies. Each entry names the
   memory file to read and explains exactly what information to extract
   from it before writing code.
   - Task 03 reads: task-01
   - Task 04 reads: task-01, task-02, task-03
   - Task 05 reads: task-01, task-04
   - Task 06 reads: task-02, task-04, task-12
   - Task 07 reads: task-01, task-05
   - Task 08 reads: task-07
   - Task 09 reads: task-08
   - Task 10 reads: task-02, task-06
   - Task 11 reads: task-10
   - Tasks 01, 02, 12 have no upstream dependencies; no section added.

2. **`## Sub-Agent Testing`** — inserted before `## Memory File` in
   every task. Instructs the executing agent to delegate test writing
   and test execution to sub-agents so the main agent's context does
   not balloon. Key per-task notes:
   - Task 07: two parallel sub-agents (service unit tests vs. state
     integration tests, disjoint write scopes).
   - Task 10: testing folded into the existing three blast-radius
     sub-agents (one per disjoint write scope).
   - Task 11: verification-only sub-agent (build + run, no new test
     files needed since changes are purely additive).

## What Was Produced

- `specs/debug-changestream.md` — high-level specification.
- `specs/debug-changestream/README.md` — task index and dependency graph.
- `specs/debug-changestream/task-01-schema-migrations.md`
- `specs/debug-changestream/task-02-core-interfaces.md`
- `specs/debug-changestream/task-03-transaction-wrapper.md`
- `specs/debug-changestream/task-04-stream-txn-trace.md`
- `specs/debug-changestream/task-05-stream-debug-state.md`
- `specs/debug-changestream/task-06-eventmux-trace.md`
- `specs/debug-changestream/task-07-domain-debugchangestream.md`
- `specs/debug-changestream/task-08-api-facade.md`
- `specs/debug-changestream/task-09-cli-commands.md`
- `specs/debug-changestream/task-10-watcher-trace-transport.md`
- `specs/debug-changestream/task-11-watcher-consumer-spans.md`
- `specs/debug-changestream/task-12-core-trace-addlink.md`

**Spec updated (Watcher interface + API transport):**
- Added section "Watcher Interface Extension" after Event Multiplexer
  Changes.
- Added section "API Transport: Trace Context in Watch Results" covering
  `rpc/params` struct additions, server-side `Next()` handlers, and
  client-side watcher changes.
- Expanded "Traceability: Span ID Flow" into two sub-flows
  (in-process and RPC path) plus a backwards-compatibility note.

## Original Prompt

Create a specification in `specs/` for two new Juju CLI commands:
`juju debug-pause` and `juju debug-step`. Goal: allow a developer to
debug the Juju distributed system by stopping the world and stepping
through one wakeup at a time.

Key requirements from the prompt:
- The changestream system (`internal/changestream`) powers the watchers.
  It reads from `change_stream` table and wakes interested watchers.
- Pause/step must be co-ordinated via the database (HA clusters have
  multiple stream workers).
- Add a monotonically increasing txn number column to the change log.
- Add a `debug_change_stream` table to control debug state.
- Address traceability by storing span IDs with changes, forwarded to
  watchers so effects can be traced.
- Spec starts high level, breaks down further with user.
- Ask rather than guess on open questions.

## User Decisions

Answers given when the draft spec's open questions were put to the user:

**A. txn_id population** — sequence table; incremented as the *last
step* when a transaction becomes a write transaction. (Not trigger-based
auto-increment at INSERT time.)

**B. Trace/span ID injection** — a special table populated at the first
write of a transaction, then reset at the end of the transaction.

**C. Scope** — both model and controller changestreams are covered.
They exist in different databases, so commands affect the current model
by default; `--all` or `--controller` extend the scope.

**D. Step output** — `debug-step` returns only how many events became
visible. No detailed change list.

**E. FileNotifier** — keep it; it serves a different purpose.

**F. Permissions** — superusers only.

**Schema approach** (follow-up correction) — do not create new numbered
migration SQL files. Edit the existing changelog SQL files directly:
`domain/schema/controller/sql/0003-changelog.sql` and
`domain/schema/model/sql/0001-changelog.sql`. If anything is generated,
change the generator then regenerate.

## Design Decisions (with rationale)

**`Watcher[T].ChangeContext`** — new method on the `core/watcher`
`Watcher[T]` interface. Takes a parent `context.Context`, returns a
derived context enriched with the OTel trace/span IDs for the last
value received on `Changes()`. Returns parent unchanged when no trace
context is available. `BaseWatcher` in `eventsource` implements it;
client-side watchers in `api/watcher` implement it independently.

**Storing trace context in the watcher** — `BaseWatcher` gains
`lastTraceID`/`lastSpanID` fields (mutex-protected). The watcher loops
call `setLastTrace(events)` when they accept a batch from the
subscription channel, using the same coalescing logic as the event
multiplexer (single trace ID → store it; mixed → clear to empty so
`ChangeContext` returns parent unchanged). Value receivers are used on
concrete `ChangeEvent` types for consistency.

**`XXXWatchResult` transport fields** — every `WatchResult` struct in
`rpc/params` gains `TraceID string json:"trace-id,omitempty"` and
`SpanID string json:"span-id,omitempty"`. `omitempty` ensures wire
compatibility with old clients/servers (both directions).

**Server-side `Next()` extraction** — after `internal.FirstResult`
drains a batch, `Next()` calls `w.watcher.ChangeContext(ctx)` then
`coretrace.ScopeFromContext` to extract the IDs and populate the
`WatchResult` before returning it over RPC.

**Initial `WatchResult` has no trace** — the initial state returned by
`Watch` comes from a direct DB query, not from a `ChangeEvent`, so no
OTel parent exists. Clients treat it as having an empty trace context.

**Client-side watcher trace storage** — each concrete watcher type in
`api/watcher` stores `lastTraceID`/`lastSpanID` under a mutex, updated
from each incoming `WatchResult`. `ChangeContext` reads them for the
caller.

**`ChangeContext` is on the interface, not a sub-interface** — it is
always safe to call; it degrades gracefully (returns parent) when no
trace is present. No type assertion or optional interface required.

**txn_id population** — sequence table `change_log_txn_seq` (single
row, `id INTEGER`). `txn_id = 0` is the sentinel for "not yet stamped
by a managed write"; the sequence begins at `1` after the first
increment. A partial index `idx_change_log_unstamped` on
`change_log(txn_id) WHERE txn_id = 0` accelerates the back-fill query.

The runner finalises with:
1. `UPDATE change_log_txn_seq SET id = id + 1`
2. `UPDATE change_log SET txn_id = (SELECT id ...) WHERE txn_id = 0`
3. `UPDATE change_log_trace_ctx SET is_in_txn = 0, trace_id = '', span_id = ''`

**Trace/span ID injection** — `change_log_trace_ctx` single-row table
(`id INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1)`) with an `is_in_txn
INTEGER` sentinel column. Schema:
```
id        INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1)
is_in_txn INTEGER NOT NULL DEFAULT 0
trace_id  TEXT    NOT NULL DEFAULT ''
span_id   TEXT    NOT NULL DEFAULT ''
```

The DB wrapper's `Setup` hook (run after `BEGIN IMMEDIATE`) sets
`is_in_txn = 1` and writes the OTel trace/span IDs. The
`change_log_set_trace` trigger checks `is_in_txn`: if `1`, it stamps the
new row; if `0` (out-of-band write), it leaves `trace_id`/`span_id`
empty. The wrapper's `Finalise` hook resets `is_in_txn = 0` and clears
the IDs before `COMMIT`. Out-of-band writes (migrations, direct SQL)
produce `txn_id = 0` rows; the stream logs a warning when it sees them.

`debug_change_stream` also gains `id INTEGER PRIMARY KEY DEFAULT 1
CHECK(id = 1)` to enforce the single-row invariant. Timestamps use
`STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc')` for millisecond
precision.

**`ChangeEvent` trace interface** — `TraceID()` and `SpanID()` are added
directly to the `ChangeEvent` interface rather than via a separate
`TracedChangeEvent`. No external code implements `ChangeEvent` outside
the changestream package, so this is a non-breaking extension.

**Coalescing trace semantics** — the event multiplexer does **not**
enrich the `context.Context` passed to subscribers. When all changes in
a coalesced batch share the same `traceID`, no action is needed. When
changes carry mixed `traceID` values, a fresh W3C trace ID (32
lower-case hex chars via `crypto/rand` + `encoding/hex`) is allocated,
a new span is opened, and `span.AddLink(traceID, spanID)` is called for
every distinct originating trace. This requires `core/trace.Span` to
gain `AddLink(traceID, spanID string)` (Task 12).

Consumers access trace context via `watcher.ChangeContext()`, not via
the subscriber `context.Context`.

**CLI scope** — commands embed `modelcmd.ControllerCommandBase` (not
`ModelCommandBase`). An optional `--model` flag defaults to the current
model when neither `--controller` nor `--all` is specified. `--model`
may not be combined with `--all` or `--controller`.

No `--format json` option — this tool is not for machine consumption.

**debug-step output** — returns only the event count and txn range, not
a full change list. The trace backend is the tool for deeper inspection.

**FileNotifier** — retained unchanged as a per-node emergency
circuit-breaker; the new DB-driven mechanism is independent.

**Permissions** — superuser only, enforced in the API facade via
`authorizer.AuthClient()` + `authorizer.HasPermission(permission.SuperuserAccess, controllerTag)`.

## Key Codebase Facts Discovered

**Watcher interface** — `core/watcher/watcher.go`: `Watcher[T any]`
has only `worker.Worker` + `Changes() <-chan T` today.

**`BaseWatcher`** — `core/watcher/eventsource/base.go`: embedded by
`NamespaceWatcher` and `NotifyWatcher`. Both watchers have a `loop()`
that reads `subscription.Changes()` (`<-chan []changestream.ChangeEvent`)
before dispatching to `w.out`.

**Client-side watcher** — `api/watcher/watcher.go`:
`commonWatcher.commonLoop()` polls `Next` RPC (long-poll). Concrete
types: `stringsWatcher` (`NewStringsWatcher`), `notifyWatcher`
(`NewNotifyWatcher`), etc. Each reads from `w.in` (which commonLoop
populates) and forwards to `w.out` (the public `Changes()` channel).

**`EnsureRegisterWatcher`** — `apiserver/internal/watcher.go`: drains
first event, then registers watcher. Initial changes go into the
`WatchResult` returned by the facade's `Watch` method.

**Server-side per-watcher facades** — `apiserver/watcher.go`:
`srvStringsWatcher.Next()`, `srvNotifyWatcher.Next()`, etc. Each calls
`internal.FirstResult` then builds a `WatchResult`. These are the
methods that need `ChangeContext` calls added.

**`coretrace.ScopeFromContext`** — returns
`(traceID, spanID string, flags int, ok bool)` from a context enriched
by `WithTraceScope`.

**Schema**
- Controller changelog: `domain/schema/controller/sql/0003-changelog.sql`
- Model changelog: `domain/schema/model/sql/0001-changelog.sql`
- Files are edited directly (not generated). Embedded via `//go:embed`.
- Trigger files under `domain/schema/*/triggers/` ARE generated by
  `triggergen` — do not edit them directly.
- Schema tests (`controller_schema_test.go`, `model_schema_test.go`)
  hold exact sets of table/trigger names that must be kept in sync.
- `TestApplyDDLIdempotent` applies full DDL twice — all SQL must be
  idempotent.

**Transaction wrapper**
- Type: `RetryingTxnRunner` in `internal/database/txn/transaction.go`.
- Redesigned: every transaction starts as read-only (`BEGIN`). On
  `SQLITE_READONLY` error the runner rolls back and retries as
  `BEGIN IMMEDIATE`. `Setup` and `Finalise` hooks only run on the
  write-transaction path — read-only transactions incur zero overhead.
- Hook type is `TxnHooks{Setup func(ctx, *sql.Tx) error,
  Finalise func(ctx, *sql.Tx) error}`. Both receive `context.Context`.
- New constructor: `NewRetryingTxnRunnerWithHooks(hooks TxnHooks)`.
  Existing `NewRetryingTxnRunner()` unchanged.
- OTel scope extraction: `coretrace.ScopeFromContext(ctx)` →
  `(traceID, spanID string, flags int, ok bool)`.

**Changestream**
- `selectQuery` in `internal/changestream/stream/stream.go` coalesces
  by `GROUP BY namespace_id, changed` with `MAX(c.id)`.
- `Term` handshake: stream blocks on `term.done` after dispatch; mux
  calls `term.Done(empty, dying)` to unblock it.
- Existing file-based pause: `FileNotifier.Changes() <-chan bool` in the
  stream `OUTER` loop.
- No existing trace context anywhere in changestream.
- Debug mode is discovered via a special change-stream event type (zero
  polling overhead during normal operation). An in-memory `debugMode`
  bool flag gates calls to `readDebugState()`.
- Deterministic restart: the debug state records the `change_log` ID at
  which debug mode is considered active. On restart, the stream compares
  its watermark against that ID.
- State transitions use CAS-style SQL (`WHERE state = 'step' AND
  step_target = ?`) to prevent lost-update races.

**API facade registration**
- Controller facades that need per-model DB access use
  `registry.MustRegisterForMultiModel`.
- Per-model domain services: `ctx.DomainServicesForModel(stdCtx, uuid)`.
- Controller services: `ctx.ControllerDomainServices()`.
- Superuser pattern from `migrationtarget`: `checkAuth` calls
  `AuthClient()` then `HasPermission(SuperuserAccess, controllerTag)`.
- Central registry file to add `Register` call: identified by searching
  for other controller facade `Register` calls (task 08 must confirm).

**Tracing**
- `core/trace` package: `Tracer`, `Span`, `Scope` interfaces; no OTel
  import at core level.
- `WithTraceScope(ctx, traceID, spanID, flags) context.Context` enriches
  a context.
- `ScopeFromContext(ctx)` extracts it.
- OTel worker in `internal/worker/trace`.

## Task Dependency Order

01, 02, 12 (parallel) → 03 → 04 → 05, 06 (parallel; 06 needs 12) →
07, 10 (parallel) → 08, 11 (parallel) → 09

Task 10 has large blast radius; use sub-agents for parallel watcher
updates (disjoint write scopes: core/watcher, api/watcher, rpc/params).
