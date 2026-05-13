# Debug Change Stream: `juju debug-pause` and `juju debug-step`

## Status

Draft — design decisions resolved, ready for detailed review.

## Summary

Add three new CLI commands, `juju debug-pause`, `juju debug-step`, and
`juju debug-resume`, that allow a developer to halt and step through a
Juju changestream one database transaction at a time. This provides a
deterministic inspection tool for debugging the distributed Juju system
without restarting agents or attaching a native debugger.

Commands target the current model's changestream by default. Flags allow
them to operate across all model changestreams and the controller
changestream simultaneously.

## Motivation

Juju is a distributed system comprised of many workers across one or more
controller agents. Most workers operate on a watch-act-wait loop:

1. A watcher subscribes to the changestream.
2. The changestream delivers a *term* — a coalesced batch of database
   change events.
3. The worker acts on the term, then waits for the next one.

Diagnosing subtle ordering or timing bugs in this system is extremely
difficult because the changestream continuously delivers terms to all
subscribed workers. There is no way to observe a single causal step in
isolation.

These commands allow a developer to:

1. Pause changestream delivery for a model, for the controller, or across
   all databases in the cluster simultaneously.
2. Step the system forward one database transaction at a time.
3. Know exactly how many change events became visible to the system per
   step, without the noise of continuous polling.
4. Trace the downstream effects of a single transaction through the system
   using OpenTelemetry span IDs stored alongside each change.

## Background: How the Change Stream Works

The changestream converts SQLite `change_log` table rows into watcher
notifications. Each Juju database (one per model, plus the controller
database) has its own `change_log` table and its own pair of stream
workers:

- **`change_log` table**: SQLite `AFTER INSERT/UPDATE/DELETE` triggers on
  every watched table append a row here, with a monotonically increasing
  `id`, a `namespace_id`, the `changed` primary-key value, and an
  `edit_type_id`.
- **`Stream` worker** (`internal/changestream/stream`): polls `change_log`
  continuously, coalescing rows by `(namespace, changed)` into a *term*,
  then sends that term on a channel.
- **`EventMultiplexer` worker** (`internal/changestream/eventmultiplexer`):
  receives terms from the stream and fans out each change to all matching
  watcher subscriptions.
- **`change_log_witness` table**: one row per controller node, tracking
  each node's high-water mark so a background pruner can delete old rows.

In an HA cluster each controller node runs its own Stream and
EventMultiplexer against the shared Dqlite database, so all nodes observe
the same change log.

There is already a file-based pause hook (`FileNotifier`) in the stream
worker. It is per-node, not co-ordinated across the HA cluster, and
provides no step-through capability. It is retained and serves a
different operational purpose (emergency per-node circuit-breaker).

## User Interface

### Scope Flags

All three commands share the same scope flags:

| Flag | Meaning |
|------|---------|
| *(none)* | Targets the current model's changestream only. |
| `--all` | Targets all model changestreams and the controller changestream. |
| `--controller` | Targets only the controller changestream. |

Standard model selection (`-m <model>`) targets a specific model's
changestream.

### `juju debug-pause`

Pauses the targeted changestream(s). The command blocks until all nodes
that serve those streams have acknowledged the pause (no in-flight term
is being dispatched). After it returns, no new terms are delivered to any
watcher for the targeted databases.

```
juju debug-pause [-m <model>] [--all | --controller]
```

Only controller superusers may run this command.

**Example output:**
```
Change stream paused (model "mymodel", txn 42).
```

```
Change stream paused (all, txn 42).
```

### `juju debug-step`

Advances the paused changestream(s) by one transaction (default) or N
transactions. After each batch of steps completes, the stream is paused
again. Returns the number of change events that became visible to the
system during the step.

```
juju debug-step [--count N] [-m <model>] [--all | --controller]
```

**Flags:**
- `--count N`: number of transactions to step through (default: `1`).

Only controller superusers may run this command.

**Example output:**
```
Stepped 1 transaction(s) (txn 43): 3 event(s).
```

```
Stepped 2 transaction(s) (txn 43-44): 7 event(s).
```

When targeting `--all`, each database is reported on its own line:
```
controller: stepped 1 transaction(s) (txn 19): 1 event(s).
model "mymodel": stepped 1 transaction(s) (txn 43): 3 event(s).
model "other": already at head, 0 event(s).
```

### `juju debug-resume`

Resumes the targeted changestream(s) and returns to normal operation.

```
juju debug-resume [-m <model>] [--all | --controller]
```

Only controller superusers may run this command.

**Example output:**
```
Change stream resumed (model "mymodel").
```

## Technical Design

### Design Principles

- **Database-driven state**: all pause/step/resume state lives in each
  database's own `debug_change_stream` table. Co-ordination across HA
  nodes is automatic because they share the same Dqlite database.
- **Per-database granularity**: each Juju database (controller or model)
  has its own `debug_change_stream` table and can be paused independently.
- **Non-destructive**: pausing does not drop in-flight terms; it prevents
  new ones from being dispatched once the current term is complete.
- **Additive tracing**: span/trace ID propagation is additive — existing
  subscribers are unaffected if they do not request the trace context.
- **Superuser-only**: the commands are restricted to controller
  superusers. Pausing a live system will delay all worker reactions.

### Database Schema Changes

The following changes are applied to every Juju database — both the
controller database and each model database.

#### 1. `change_log_txn_seq` — transaction sequence table

A single-row table that acts as the monotonically increasing transaction
ID counter.

```sql
CREATE TABLE IF NOT EXISTS change_log_txn_seq (
    id  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id)
);
INSERT OR IGNORE INTO change_log_txn_seq VALUES (0);
```

`txn_id = 0` is the sentinel meaning "not yet stamped by a
transaction-wrapper-managed write". The sequence begins at `1` after
the first increment, making `0` unambiguous as the out-of-band
sentinel.

As the *last* operation in any Juju write transaction, the application
code increments this counter and back-fills `change_log.txn_id` for all
rows written during that transaction:

```sql
-- Step 1: advance the sequence.
UPDATE change_log_txn_seq SET id = id + 1;

-- Step 2: stamp all un-stamped change_log rows with the new txn_id.
-- txn_id = 0 is safe as a sentinel because SQLite serialises writes:
-- only one write transaction runs at a time.
UPDATE change_log
    SET txn_id = (SELECT id FROM change_log_txn_seq)
    WHERE txn_id = 0;
```

These two statements are issued automatically by the Juju database
transaction wrapper (in `internal/database`) as the final writes before
`COMMIT`. No changes are required at individual call sites.

#### 2. `change_log` — new `txn_id`, `trace_id`, `span_id` columns

```sql
ALTER TABLE change_log ADD COLUMN txn_id   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE change_log ADD COLUMN trace_id TEXT    NOT NULL DEFAULT '';
ALTER TABLE change_log ADD COLUMN span_id  TEXT    NOT NULL DEFAULT '';
```

- `txn_id`: populated by the sequence mechanism described above.
- `trace_id` / `span_id`: populated from `change_log_trace_ctx` (below)
  by the INSERT trigger on `change_log`. When tracing is disabled, both
  columns are empty strings.

A partial index is added to accelerate the txn_id back-fill query:

```sql
CREATE INDEX IF NOT EXISTS idx_change_log_unstamped
    ON change_log(txn_id) WHERE txn_id = 0;
```

#### 3. `change_log_trace_ctx` — trace context for current transaction

A single-row table that holds the OTel trace context active during the
current write transaction.

```sql
CREATE TABLE IF NOT EXISTS change_log_trace_ctx (
    id        INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1),
    is_in_txn INTEGER NOT NULL DEFAULT 0,
    trace_id  TEXT    NOT NULL DEFAULT '',
    span_id   TEXT    NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO change_log_trace_ctx VALUES (1, 0, '', '');
```

The application code writes the current span's IDs into this table at
the first write of a transaction (before any watched table is mutated).
The `AFTER INSERT` trigger on `change_log` reads from this table to
populate the new columns:

```sql
CREATE TRIGGER IF NOT EXISTS change_log_set_trace
AFTER INSERT ON change_log
BEGIN
    UPDATE change_log
    SET
        trace_id = CASE
            WHEN (SELECT is_in_txn FROM change_log_trace_ctx) = 1
            THEN (SELECT trace_id FROM change_log_trace_ctx)
            ELSE '' END,
        span_id  = CASE
            WHEN (SELECT is_in_txn FROM change_log_trace_ctx) = 1
            THEN (SELECT span_id FROM change_log_trace_ctx)
            ELSE '' END
    WHERE id = NEW.id;
END;
```

At the end of the transaction (after the `txn_id` back-fill), the
application resets the trace context and clears the sentinel:

```sql
UPDATE change_log_trace_ctx
    SET is_in_txn = 0, trace_id = '', span_id = '';
```

`is_in_txn = 1` is written as part of the setup statements before
the first write. The trigger checks this flag: if it is `1`, trace
context is stamped; if it is `0`, the row was written outside a
managed write transaction (e.g. a migration or direct SQL) and
`trace_id`/`span_id` are left empty. A warning is logged when
`txn_id = 0` rows are observed by the stream so developers can
identify out-of-band writes.

This table always has exactly one row, enforced by the primary key
constraint `CHECK(id = 1)`. Because SQLite serialises write
transactions, there is no concurrency hazard.

#### 4. `debug_change_stream` — debug control table

A single-row table (per database) that governs debug mode for that
database's changestream.

```sql
CREATE TABLE IF NOT EXISTS debug_change_stream (
    id          INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1),
    state       TEXT    NOT NULL DEFAULT 'running'
        CHECK(state IN ('running', 'paused', 'step')),
    step_target INTEGER NOT NULL DEFAULT 0,
    updated_at  DATETIME NOT NULL
        DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc'))
);
INSERT OR IGNORE INTO debug_change_stream
    VALUES (1, 'running', 0,
            STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc'));
```

`step_target` is the `txn_id` value the stream must reach (inclusive)
before returning to `paused`.

**State machine:**

```
         pause                  step N sets step_target
running ──────▶ paused ──────────────────────────────▶ step
        ◀──────────────────────── resume ◀──────────────────
                                               │
                                     stream reaches step_target
                                               │
                                               ▼
                                            paused
```

- `running` — normal operation.
- `paused` — stream halts after the current in-flight term completes.
- `step` — stream advances until `change_log.txn_id = step_target`,
  then writes `state = 'paused'` and halts. State transitions use
  compare-and-swap updates (e.g.
  `UPDATE ... SET state = 'paused' WHERE state = 'step'`) to prevent
  lost-update races.

### Transaction Wrapper Changes

The `RetryingTxnRunner` in `internal/database/txn/transaction.go` is
redesigned to inject trace context and txn_id stamping automatically
without requiring any changes at individual call sites.

**Transaction upgrade strategy**: every transaction begins as a
read-only SQLite transaction (`BEGIN`). If the caller attempts a
write and SQLite returns `SQLITE_READONLY`, the runner rolls back
and retries the entire transaction as a read-write transaction
(`BEGIN IMMEDIATE`). This avoids upgrade contention while keeping
the common (read-only) case cheap.

**Setup statements** — injected immediately after `BEGIN IMMEDIATE`
on the write-transaction path, executed directly on the underlying
`*sql.Tx`:

```sql
UPDATE change_log_trace_ctx
    SET is_in_txn = 1, trace_id = ?, span_id = ?;
```

The `trace_id` and `span_id` values are extracted from the
`context.Context` passed to `Txn`/`StdTxn` using
`coretrace.ScopeFromContext`. If tracing is inactive, both values
are empty strings. Setting `is_in_txn = 1` tells the trigger that
this row was produced by a managed write transaction.

**Finalise statements** — injected before `COMMIT` on the
write-transaction path:

```sql
UPDATE change_log_txn_seq SET id = id + 1;
UPDATE change_log
    SET txn_id = (SELECT id FROM change_log_txn_seq)
    WHERE txn_id = 0;
UPDATE change_log_trace_ctx
    SET is_in_txn = 0, trace_id = '', span_id = '';
```

If no `change_log` rows were written, the middle `UPDATE` is a
no-op. The runner executes these statements directly on the
underlying `*sql.Tx`, so they apply regardless of whether the
caller used sqlair or stdlib.

These hooks are invoked automatically. No individual call sites need
updating.

### Stream Worker Changes

The `Stream` worker (`internal/changestream/stream`) gains:

1. **Debug mode discovery**: the stream worker discovers that debug mode
   has been enabled via a special change-stream event type (not by
   polling `debug_change_stream` on every iteration). This means zero
   overhead during normal operation. Once the special event is
   received, an in-memory flag is set and the worker begins reading
   `debug_change_stream` on each iteration. The special event is a
   *hint* to start checking; the actual state machine is always driven
   by `readDebugState()`.

   **Deterministic restart**: the debug state records a specific
   `change_log` ID at which debug mode is considered active. On
   controller restart, the stream worker reads the debug state and
   compares against its current position. If it has not yet processed
   past that ID, it enters debug mode immediately, making restart
   behaviour deterministic.

   If `state = 'paused'`, the worker applies its normal backoff and
   retries without dispatching. If `state = 'step'`, it proceeds to
   read changes bounded by `step_target`.

2. **Step-bounded reads**: when `state = 'step'`, the `readChanges` query
   gains an additional predicate:

   ```sql
   WHERE c.id > ? AND c.txn_id <= ?
   ```

   where the second `?` is `step_target`. After dispatching that term,
   the stream writes `state = 'paused'` back to `debug_change_stream`
   using a CAS-style update:
   `UPDATE debug_change_stream SET state = 'paused'
    WHERE state = 'step' AND step_target = ?`.

3. **`txn_id`-aware query**: `readChanges` selects `c.txn_id` as well as
   the existing columns. The `Term` interface is extended with two new
   methods so the API facade can report the range consumed:

   ```go
   // TxnMinID returns the lowest txn_id present in this term.
   TxnMinID() int64
   // TxnMaxID returns the highest txn_id present in this term.
   TxnMaxID() int64
   ```

   These are computed from the scanned `changeEvent` slice:
   `txnMin = min(e.txnID for e in changes)`,
   `txnMax = max(e.txnID for e in changes)`.

4. **Traceability fields**: `changeEvent` gains `traceID` and `spanID`
   fields populated from the corresponding `change_log` columns.

### Event Multiplexer Changes

The `ChangeEvent` interface is extended directly with trace context
methods. No external code implements `ChangeEvent` outside of the
changestream package itself, so this is a non-breaking change:

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

The multiplexer does **not** enrich the `context.Context` passed to
subscribers. Trace propagation relies solely on the `TraceID()` and
`SpanID()` methods on `ChangeEvent`, which are readable by consumers
via `watcher.ChangeContext()` (implemented in Task 10).

When a term contains coalesced changes from multiple transactions
that carry **different** trace IDs, the multiplexer handles causal
linking via `core/trace.Span.AddLink`:

- A fresh trace ID is allocated for the coalesced batch (W3C format:
  32 lower-case hex characters generated with `crypto/rand`).
- A new span is opened for the coalesced batch with the fresh trace
  ID.
- `AddLink(traceID, spanID)` is called for every distinct originating
  trace, linking the coalesced span back to all original traces.
- The `spanID` of the most recent change (highest `txn_id`) is used
  as the parent span reference.

When all changes in a coalesced batch already share the same
`traceID`, no new trace is created; the existing `(traceID, spanID)`
of the most recent change is used directly.

### Watcher Interface Extension

The `core/watcher` package `Watcher[T]` interface gains a
`ChangeContext` method:

```go
// Watcher defines a worker that emits changes for a given type T.
type Watcher[T any] interface {
	worker.Worker

	// Changes returns a channel of type T, closed when the watcher
	// stops.
	Changes() <-chan T

	// ChangeContext returns a new context derived from parent,
	// enriched with the OTel trace ID and span ID associated with
	// the last value dispatched on Changes(). If no value has been
	// received yet, or no trace context was captured for that
	// value, parent is returned unchanged.
	ChangeContext(parent context.Context) context.Context
}
```

#### `BaseWatcher` implementation

`BaseWatcher` in `core/watcher/eventsource` stores the trace context
for the most recently received term and implements `ChangeContext`:

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

The `setLastTrace(events []changestream.ChangeEvent)` helper is called
by each watcher's loop whenever it receives a non-empty batch from the
subscription channel. It applies the same coalescing logic as the
event multiplexer: if all events share one trace ID, that ID and the
span ID of the highest-`txn_id` event are stored; otherwise the trace
ID is cleared (empty string) so that `ChangeContext` returns the
parent unchanged, deferring traceability to the server-side multiplexer
path instead.

`NamespaceWatcher` and `NotifyWatcher` both call `setLastTrace` when
they accept events from the subscription `Changes()` channel, before
ticking to dispatch mode.

### API Transport: Trace Context in Watch Results

Trace context must also cross the RPC boundary so that client-side
consumers can call `ChangeContext` on a remote watcher and get a
meaningful context.

#### `XXXWatchResult` struct additions

Every `WatchResult` struct in `rpc/params` gains two optional fields:

```go
type StringsWatchResult struct {
	StringsWatcherId string   `json:"watcher-id"`
	Changes          []string `json:"changes,omitempty"`
	TraceID          string   `json:"trace-id,omitempty"`
	SpanID           string   `json:"span-id,omitempty"`
	Error            *Error   `json:"error,omitempty"`
}

type NotifyWatchResult struct {
	NotifyWatcherId string `json:"NotifyWatcherId"`
	TraceID         string `json:"trace-id,omitempty"`
	SpanID          string `json:"span-id,omitempty"`
	Error           *Error `json:"error,omitempty"`
}
```

The `omitempty` tag on both fields preserves wire compatibility with
older clients and servers that do not populate them.

All other `XXXWatchResult` types (`EntitiesWatchResult`,
`RelationUnitsWatchResult`, `MachineStorageIdsWatchResult`,
`SecretTriggerWatchResult`, `SecretRevisionWatchResult`, etc.) receive
the same two fields on the same basis.

#### Server-side `Next()` handlers

In `apiserver/watcher.go`, each `srvXxxWatcher.Next()` extracts trace
context from the registered watcher via `ChangeContext` after draining
the changes:

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

Because `ChangeContext` is called immediately after `FirstResult`
returns, the stored trace IDs correspond exactly to the batch of
changes just drained.

The initial `WatchResult` returned by the facade's `Watch` method does
not carry trace context. The initial state is synthesised from a
direct database query, not from a `ChangeEvent`, so there is no OTel
parent to propagate. Clients treat the initial result as having an
empty trace context.

#### Client-side watcher changes

The client-side watcher implementations in `api/watcher` add the same
`ChangeContext` method. Each concrete watcher type stores the last
received trace IDs under a mutex:

```go
type stringsWatcher struct {
	commonWatcher
	caller           base.APICaller
	stringsWatcherId string
	out              chan []string

	mu          sync.Mutex
	lastTraceID string
	lastSpanID  string
}

func (w *stringsWatcher) ChangeContext(
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

In `loop()`, after receiving a new `*params.StringsWatchResult` from
`w.in`, the watcher updates the stored IDs before forwarding the
changes to `w.out`:

```go
result := data.(*params.StringsWatchResult)
changes = result.Changes
w.mu.Lock()
w.lastTraceID = result.TraceID
w.lastSpanID = result.SpanID
w.mu.Unlock()
```

The `notifyWatcher` and all other concrete client-side watcher types
follow the same pattern.

### Domain Layer

A new `domain/debugchangestream` domain encapsulates the database
interactions for the debug control table:

```go
// Service provides pause/step/resume operations on a single database's
// changestream debug state.
type Service struct { ... }

func (s *Service) Pause(ctx context.Context) error
func (s *Service) Step(ctx context.Context, count int) ([]StepResult, error)
func (s *Service) Resume(ctx context.Context) error
func (s *Service) Status(ctx context.Context) (string, error)
```

`Step` returns one `StepResult` per sub-step requested. Each `StepResult`
contains the number of events that became visible and the `txn_id` range
that was consumed. `Status` returns the raw state string
(`"running"`, `"paused"`, or `"step"`).

### API Facade

A new controller-level `DebugChangeStream` facade wires the CLI commands
to the domain layer. It operates across multiple databases when `--all`
is requested by calling the corresponding model database service for each
known model, plus the controller database service.

The facade polls `change_log_witness` watermarks to confirm that all HA
nodes have reached or surpassed `step_target` before returning a result
to the CLI.

### CLI Commands

The three commands are registered under `cmd/juju/commands`. Each
command embeds `modelcmd.ControllerCommandBase` (not `ModelCommandBase`)
because they always require a controller connection.

An optional `--model` flag defaults to the current model when neither
`--controller` nor `--all` is specified. When `--model` is provided, the
command targets that specific model's changestream.

Each command defines `Info()`, `SetFlags()`, `Init()`, and `Run()`, and
enforces that the caller is a controller superuser before making the
facade call.

### HA Considerations

- All debug state lives in the shared Dqlite database. No node-to-node
  RPC is required.
- In `paused` state every stream worker for that database (one per
  controller node) independently reads the table and halts. The pause is
  global across the HA cluster for that database.
- In `step` state the first stream worker to read the table advances to
  `step_target` and then transitions the state back to `paused`. All
  other nodes will already be halted in `paused` and will re-read the
  state on their next iteration — they will find `paused` and remain
  halted.
- The `change_log_witness` table provides per-node watermarks. The facade
  uses these to confirm all nodes have consumed the step before reporting
  completion to the CLI.

### Traceability: Span ID Flow

Trace context flows from the originating API call all the way to
client-side watchers via two distinct paths: the in-process path
(server-internal workers) and the RPC path (client-side watchers).

#### In-process path (server-internal workers)

```
Juju API operation begins
  │  OTel span active: trace_id=T, span_id=S
  ▼
DB write transaction begins
  → application writes (T, S) to change_log_trace_ctx
  ▼
Watched table mutated
  → AFTER INSERT/UPDATE/DELETE trigger fires
  → change_log row inserted (txn_id=0, trace_id=T, span_id=S)
  ▼
Transaction commit hook runs
  → change_log_trace_ctx reset to ('', '')
  → change_log_txn_seq incremented to N
  → change_log rows with txn_id=0 stamped with txn_id=N
  → COMMIT
  ▼
Stream.readChanges() reads rows
  → changeEvent{txnID: N, traceID: T, spanID: S, ...}
  ▼
EventMultiplexer.dispatchSet()
  → if all changes share trace_id T:
      ctx = trace.WithTraceScope(ctx, T, S_last, flags)
  → else (mixed trace IDs from coalescing):
      T_new = freshly allocated trace ID
      ctx = trace.WithTraceScope(ctx, T_new, S_last, flags)
  → watcher.setLastTrace(events) also caches (T, S_last)
  ▼
Server-internal worker reads Changes(), then calls
  ctx2 = watcher.ChangeContext(ctx)
  → ctx2 carries (T, S_last) in its trace scope
  → worker creates child OTel span (parent = S_last)
  → all downstream work is linked to the originating API call
```

#### RPC path (client-side watchers)

```
[server-side srvStringsWatcher.Next() unblocks]
  → changes, _ = internal.FirstResult[[]string](ctx, w.watcher)
  → traceCtx = w.watcher.ChangeContext(ctx)
  → traceID, spanID, _, _ = coretrace.ScopeFromContext(traceCtx)
  → returns StringsWatchResult{
        Changes: changes,
        TraceID: traceID,  // "T" or "T_new"
        SpanID:  spanID,   // "S_last"
    }
  ▼
[JSON over RPC]
  ▼
[client commonLoop receives *params.StringsWatchResult]
  → stringsWatcher.lastTraceID = result.TraceID
  → stringsWatcher.lastSpanID  = result.SpanID
  → result.Changes forwarded to w.out
  ▼
API client code reads Changes(), then calls
  ctx2 = watcher.ChangeContext(ctx)
  → ctx2 carries (T, S_last) in its trace scope
  → client creates a child OTel span (parent = S_last)
  → client-side work is linked to the originating server transaction
```

#### Old clients and servers

Older servers that do not populate `TraceID`/`SpanID` send the fields
as absent JSON keys. Client-side watchers receive empty strings and
`ChangeContext` returns the parent context unchanged — no span is
created on the client side for those changes, and nothing breaks.

```
debug-step reports: "Stepped 1 transaction(s) (txn N): K event(s)."
Developer looks up span S in their trace backend to see the
full causal chain from API call to worker reaction.
```

### Worker Consumer Spans

Every internal worker loop that reacts to a `watcher.Changes()` event
should open a new OTel span causally linked to the database transaction
that produced the change. This is done by passing
`watcher.ChangeContext(ctx)` as the parent context to `trace.Start`:

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

For inline case blocks, the span must be opened and closed explicitly
(not via `defer`, which runs at function return, not case-block exit).

This pattern is applied to every `case <-watcher.Changes():` block in
`internal/worker/` that performs work. The change is purely additive —
no pre-existing behaviour is altered. Workers without a configured
tracer incur negligible overhead because `trace.Start` falls back to a
noop span when no tracer is present in the context.

## Out of Scope

- Pausing individual workers directly (the stream is the correct
  chokepoint for the whole system).
- Recording a replay of a session for automated regression testing
  (future work).
- Providing a TUI or interactive step-through interface (future work).
- Exposing raw change details in `debug-step` output (only the event
  count is shown; the trace backend is the tool for deeper inspection).
