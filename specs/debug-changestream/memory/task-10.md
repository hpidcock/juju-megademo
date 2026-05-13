# Task 10 — Memory: Watcher Interface `ChangeContext` and API Trace Transport

## `setLastTrace` implementation

Defined on `*BaseWatcher` in
`core/watcher/eventsource/base.go`.

Coalescing rule:
1. Iterate through all events in the batch.
2. Skip events with an empty `TraceID()`.
3. If the first non-empty `TraceID` is encountered, record it and its
   `SpanID`.
4. If a subsequent event has a different non-empty `TraceID`, clear both
   stored fields immediately and return — mixed batch.
5. If a subsequent event has the same `TraceID`, update `spanID` to the
   latest (most recent) event's `SpanID`.
6. Store the result under `mu.Lock()`.

Edge cases:
- Empty batch: no-op (both fields unchanged after the lock).
- All events have empty `TraceID`: stored fields are cleared (set to
  `""`).
- Uniform trace: stored fields set to that `TraceID` and the `SpanID`
  of the last event in the batch.
- Mixed traces: stored fields cleared immediately on first mismatch.

## `XXXWatchResult` structs modified

### `rpc/params/internal.go`

| Struct | File |
|--------|------|
| `NotifyWatchResult` | `rpc/params/internal.go` |
| `StringsWatchResult` | `rpc/params/internal.go` |
| `EntitiesWatchResult` | `rpc/params/internal.go` |
| `RelationUnitsWatchResult` | `rpc/params/internal.go` |
| `MachineStorageIdsWatchResult` | `rpc/params/internal.go` |

### `rpc/params/crossmodel.go`

| Struct | File |
|--------|------|
| `RemoteApplicationWatchResult` | `rpc/params/crossmodel.go` |
| `RemoteRelationWatchResult` | `rpc/params/crossmodel.go` |
| `RelationLifeSuspendedStatusWatchResult` | `rpc/params/crossmodel.go` |
| `OfferStatusWatchResult` | `rpc/params/crossmodel.go` |

### `rpc/params/secrets.go`

| Struct | File |
|--------|------|
| `SecretTriggerWatchResult` | `rpc/params/secrets.go` |
| `SecretBackendRotateWatchResult` | `rpc/params/secrets.go` |
| `SecretRevisionWatchResult` | `rpc/params/secrets.go` |

All 12 structs have `TraceID string \`json:"trace-id,omitempty"\`` and
`SpanID string \`json:"span-id,omitempty"\`` placed after the payload
field and before `Error`.

## `srvXxxWatcher.Next()` methods modified

All in `apiserver/watcher.go`. Each now calls
`w.watcher.ChangeContext(ctx)` and `coretrace.ScopeFromContext(traceCtx)`
to extract `traceID`/`spanID`, which are embedded in the returned result.

| Method | Return type |
|--------|-------------|
| `srvNotifyWatcher.Next` | `(params.NotifyWatchResult, error)` — **signature changed** |
| `srvStringsWatcher.Next` | `(params.StringsWatchResult, error)` |
| `srvRelationUnitsWatcher.Next` | `(params.RelationUnitsWatchResult, error)` |
| `srvRemoteRelationWatcher.Next` | `(params.RemoteRelationWatchResult, error)` |
| `srvRelationStatusWatcher.Next` | `(params.RelationLifeSuspendedStatusWatchResult, error)` |
| `srvOfferStatusWatcher.Next` | `(params.OfferStatusWatchResult, error)` |
| `srvEntitiesWatcher.Next` | `(params.EntitiesWatchResult, error)` |
| `srvSecretTriggerWatcher.Next` | `(params.SecretTriggerWatchResult, error)` |
| `srvSecretBackendsRotateWatcher.Next` | `(params.SecretBackendRotateWatchResult, error)` |
| `srvSecretsRevisionWatcher.Next` | `(params.SecretRevisionWatchResult, error)` |

`SrvModelSummaryWatcher.Next` was **not** updated — its result type
`params.SummaryWatcherNextResults` has no `TraceID`/`SpanID` fields
and was not listed as a `WatchResult` struct to modify.

## `srvNotifyWatcher.Next()` signature change

Original: `func (w *srvNotifyWatcher) Next(ctx context.Context) error`

New: `func (w *srvNotifyWatcher) Next(ctx context.Context) (params.NotifyWatchResult, error)`

This is an RPC method dispatched via reflection — no direct Go callers
to update. The client-side `notifyWatcher.loop()` in `api/watcher` was
updated to use `new(params.NotifyWatchResult)` as `newResult` and to
extract `TraceID`/`SpanID` from the result.

## Client-side watcher types modified in `api/watcher/watcher.go`

All concrete watcher types had `mu sync.Mutex`, `lastTraceID string`,
`lastSpanID string` fields added and a mutex-protected `ChangeContext`
method implemented:

| Type | Trace IDs extracted from |
|------|--------------------------|
| `notifyWatcher` | `params.NotifyWatchResult` |
| `stringsWatcher` | `params.StringsWatchResult` |
| `relationUnitsWatcher` | `params.RelationUnitsWatchResult` |
| `remoteRelationWatcher` | `params.RemoteRelationWatchResult` |
| `relationStatusWatcher` | `params.RelationLifeSuspendedStatusWatchResult` |
| `offerStatusWatcher` | `params.OfferStatusWatchResult` |
| `machineAttachmentsWatcher` | `params.MachineStorageIdsWatchResult` |
| `secretsTriggerWatcher` | `params.SecretTriggerWatchResult` |
| `secretBackendRotateWatcher` | `params.SecretBackendRotateWatchResult` |
| `SecretsRevisionWatcher` | `params.SecretRevisionWatchResult` |
| `remoteRelationCompatWatcher` | trivial `return parent` |
| `migrationStatusWatcher` | trivial `return parent` |

## Blast-radius fixes beyond the three stated scopes

The `Watcher[T]` interface change required `ChangeContext` to be added
to many types across the codebase. All were given trivial
`return parent` implementations:

- `core/watcher/normalise.go` — `normaliseWatcher`
- `core/watcher/todo.go` — `todoWatcher[T]`
- `core/watcher/watchertest/type.go` — `MockWatcher[T]`
- `core/watcher/watchertest/notify.go` — `MockNotifyWatcher`
- `core/watcher/watchertest/strings.go` — `MockStringsWatcher`
- `core/watcher/eventsource/legacy.go` — `StringsNotifyWatcher`,
  `MultiWatcher[T]`
- `internal/provider/kubernetes/watcher/k8swatcher.go` —
  `kubernetesNotifyWatcher`, `kubernetesStringsWatcher`
- `api/common/disablewatcher.go` — `disabledWatcher`
- `domain/secret/service/watcher.go` — `secretWatcher[T]`
- `domain/secretbackend/service/` — `secretBackendRotateWatcher`
- `apiserver/facades/agent/provisioner/machineerror.go` —
  `machineErrorRetry`
- `apiserver/facades/agent/uniter/watcherrelationunit.go` —
  `relationUnitsWatcher`
- `apiserver/testing/fakenotifywatcher.go` — `FakeNotifyWatcher`
- `apiserver/facades/controller/firewaller/modelfirewallruleswatcher.go`
  — `modelFirewallRulesWatcher`
- `apiserver/facades/agent/storageprovisioner/watcher.go` —
  `stringSourcedWatcher[T]`
- `internal/worker/machiner/addresswatcher_linux.go` —
  `addressChangeNotifyWatcher`
- `internal/worker/machiner/notifywatcher.go` — `mergedNotifyWatcher`
- Various hand-written test stubs in `internal/worker/` and
  `apiserver/facades/`

Generated mock files were regenerated via `go generate` for:
`apiserver/`, `apiserver/facades/agent/credentialvalidator/`,
`apiserver/facades/agent/secretsmanager/mocks/`,
`apiserver/facades/agent/storageprovisioner/`,
`apiserver/facades/agent/uniter/`,
`apiserver/facades/client/action/`,
`apiserver/facades/controller/crossmodelrelations/`,
`apiserver/facades/controller/crosscontroller/`,
`apiserver/facades/controller/externalcontrollerupdater/`,
`apiserver/facades/controller/secretbackendmanager/`,
`apiserver/facades/controller/usersecrets/mocks/`,
`domain/controllerconfig/service/`,
`domain/model/service/`,
`domain/secretbackend/service/`,
`domain/secret/service/`,
`internal/worker/secretbackendrotate/mocks/`,
`internal/worker/secretrotate/mocks/`,
`internal/worker/instancepoller/mocks/`,
`internal/worker/operationpruner/`,
`internal/worker/containerprovisioner/`.

## Deviations from spec

- **`SrvModelSummaryWatcher.Next`** was not updated. Its result type
  `params.SummaryWatcherNextResults` has no `TraceID`/`SpanID` fields
  (it was not listed among the `XXXWatchResult` structs to modify).

- **`srvRemoteRelationWatcher`, `srvRelationStatusWatcher`,
  `srvOfferStatusWatcher`** — these read directly from
  `w.watcher.Changes()` rather than `internal.FirstResult`, but
  `ChangeContext` is still called after each successful event dispatch.
  Their underlying watcher types have trivial `return parent` for
  `ChangeContext` so these calls are no-ops in practice. The
  `apiserver/watcher_test.go` tests were updated to expect the
  `ChangeContext` call.

- **`watchertest.MockWatcher[T]`** in
  `core/watcher/watchertest/type.go` had `ChangeContext` added manually
  (it is not generated). Same for `MockNotifyWatcher` and
  `MockStringsWatcher` in the same package.

- **Blast radius** was substantially larger than the spec anticipated.
  The `Watcher[T]` interface change propagated to ~30+ concrete types
  and ~20+ mock packages. All were fixed.
