# Task 11 — Memory: Watcher Consumers Start a Child Span on Each Change

## Total files modified

48 source files + 5 test files.

## Source files and watcher details

| File | Watcher variable(s) | Inline or Delegated |
|------|---------------------|---------------------|
| `internal/worker/agentconfigupdater/worker.go` | `watcher` | Delegated to `handleConfigChange` |
| `internal/worker/apiremotecaller/worker.go` | `watcher` | Inline |
| `internal/worker/asynccharmdownloader/downloadworker.go` | `watcher` | Inline |
| `internal/worker/caasapplicationprovisioner/worker.go` | `appWatcher` | Inline |
| `internal/worker/caasbroker/broker.go` | `modelWatcher`, `cloudWatcher` | Inline (both) |
| `internal/worker/caasfirewaller/appfirewaller.go` | `portsWatcher` | Delegated to `ensureOpenPorts` |
| `internal/worker/caasfirewaller/firewaller.go` | `w` (firewaller itself) | Delegated to `observeApplicationFirewallChange` |
| `internal/worker/caasmodelconfigmanager/worker.go` | `watcher` | Inline |
| `internal/worker/caasmodeloperator/modeloperator.go` | `watcher` | Delegated to `update` |
| `internal/worker/caasupgrader/upgrader.go` | `versionWatcher` | Inline (event-only span, shared code after select) |
| `internal/worker/charmrevisioner/worker.go` | `configWatcher` | Inline |
| `internal/worker/containerprovisioner/containerprovisioner.go` | `modelWatcher` | Inline |
| `internal/worker/containerprovisioner/containerworker.go` | `w.containerWatcher` | Inline |
| `internal/worker/controllerpresence/worker.go` | `subscriber` | Inline (uses `ctx` as parent, not `ChangeContext` — `Subscription` lacks `ChangeContext`) |
| `internal/worker/credentialvalidator/worker.go` | `v.modelCredentialWatcher` | Inline |
| `internal/worker/dbaccessor/worker.go` | `w.cfg.ControllerConfigWatcher` | Delegated to `handleClusterConfigChange` (uses `ctx` as parent — `ConfigWatcher` lacks `ChangeContext`) |
| `internal/worker/externalcontrollerupdater/externalcontrollerupdater.go` | `watcher`, `nw` | Inline (both) |
| `internal/worker/firewaller/firewaller.go` | `fw.machinesWatcher`, `fw.subnetWatcher`, `fw.consumerRelationsWatcher`, `fw.offererRelationsWatcher`, `unitw` (x3), `appWatcher`, `ingressAddressWatcher`, `egressAddressWatcher` (x2) | Mixed: `subnetsChanged` delegated, rest inline |
| `internal/worker/instancepoller/worker.go` | `watch` | Inline |
| `internal/worker/lifeflag/worker.go` | `watcher` | Delegated to `lifeChanged` |
| `internal/worker/migrationflag/worker.go` | `watcher` | Inline |
| `internal/worker/migrationmaster/worker.go` | `watcher`, `watch` | Inline (both) |
| `internal/worker/migrationminion/worker.go` | `watch` | Delegated to `handle` |
| `internal/worker/modellife/worker.go` | `watcher` | Delegated to `lifeChanged` |
| `internal/worker/modelworkermanager/modelworkermanager.go` | `watcher` | Delegated to `modelChanged` |
| `internal/worker/objectstoredrainer/worker.go` | `cfgWatcher`, `drainingWatcher` | `cfgWatcher` delegated to `handleConfigChange`; `drainingWatcher` inline |
| `internal/worker/objectstores3caller/worker.go` | `watcher` | Inline (uses `coretrace` alias) |
| `internal/worker/objectstore/trackerworker.go` | `modelWatcher` | Delegated to `checkModel` (uses `coretrace` alias) |
| `internal/worker/operationpruner/worker.go` | `watch` | Inline |
| `internal/worker/providertracker/trackerworker.go` | `modelConfigWatcher`, `modelWatcher` | Inline (both, uses `coretrace` alias) |
| `internal/worker/remoterelationconsumer/consumerunitrelations/worker.go` | `watcher` | Inline |
| `internal/worker/remoterelationconsumer/localconsumerworker.go` | `relationsWatcher` | Inline |
| `internal/worker/remoterelationconsumer/offererrelations/worker.go` | `watcher` | Inline |
| `internal/worker/remoterelationconsumer/offererunitrelations/worker.go` | `watcher` | Inline |
| `internal/worker/remoterelationconsumer/worker.go` | `modelWatcher`, `offersWatcher` | Delegated to `handleModelDying`, `handleApplicationChanges` |
| `internal/worker/removal/worker.go` | `watch` | Delegated to `processRemovalJobs` |
| `internal/worker/secretbackendrotate/rotate.go` | `changes` | Delegated to `handleTokenRotateChanges` |
| `internal/worker/secretexpire/secretexpire.go` | `changes` | Delegated to `handleSecretRevisionExpiryChanges` |
| `internal/worker/secretrotate/secretrotate.go` | `changes` | Delegated to `handleSecretRotateChanges` |
| `internal/worker/secretsdrainworker/worker.go` | `watcher` | Inline |
| `internal/worker/secretspruner/worker.go` | `watcher` | Delegated to `processChanges` |
| `internal/worker/undertaker/worker.go` | `watcher` | Inline |
| `internal/worker/uniter/relation/statetracker.go` | `unitWatcher` | Inline |
| `internal/worker/uniter/remotestate/watcher.go` | `unitw`, `resolveModew`, `applicationw`, `charmConfigw`, `trustConfigw`, `actionsw`, `relationsw`, `storagew`, `updateStatusIntervalw`, `ruw`, `saw` | Mixed: `unitChanged`, `resolveModeChanged`, `applicationChanged`, `relationsChanged`, `storageChanged` delegated; `configHashChanged`, `trustHashChanged`, `actionsChanged`, `updateStatusIntervalw` inline; `ruw`, `saw` inline |
| `internal/worker/uniter/uniter.go` | `unitWatcher` | Inline (uses `coretrace` alias) |
| `internal/worker/upgradedatabase/worker.go` | `completedWatcher`, `failedWatcher`, `watcher` | Inline (all three) |
| `internal/worker/upgrader/upgrader.go` | `versionWatcher` | Inline (event-only span, shared code after select) |
| `internal/worker/upgradestepscontroller/controllerworker.go` | `completedWatcher`, `failedWatcher` | Inline (both) |

## Test files modified

- `internal/worker/containerprovisioner/containerworker_test.go` — Added `ChangeContext` mock expectation
- `internal/worker/instancepoller/worker_test.go` — Added `ChangeContext` mock expectation
- `internal/worker/secretbackendrotate/rotate_test.go` — Added `ChangeContext` mock expectation
- `internal/worker/secretexpire/secretexpire_test.go` — Added `context` import and `ChangeContext` mock expectation
- `internal/worker/secretrotate/secretrotate_test.go` — Added `context` import and `ChangeContext` mock expectation

## Cases skipped and why

| File | Line | Watcher | Reason |
|------|------|---------|--------|
| `internal/worker/storageprovisioner/machines.go` | 145 | `w` | Only sets `out = mw.out` — enables dispatching, no work |
| `internal/worker/apiaddresssetter/controllertracker.go` | 73 | `addressWatcher` | Only sets `notifyCh = c.notifyCh` — enables dispatching, no work |
| `internal/worker/machiner/notifywatcher.go` | 57 | `w.primary` | Only calls `w.notify()` — enables dispatching, no work |
| `internal/worker/machiner/notifywatcher.go` | 68 | `w.secondary` | Only calls `w.notify()` — enables dispatching, no work |
| `internal/worker/caasapplicationprovisioner/application.go` | 364 | `appScaleWatcher` | Only sets `scaleChan` timer — enables dispatching, no work |
| `internal/worker/caasapplicationprovisioner/application.go` | 400 | `appSettingsWatcher` | Only sets `trustChan` timer — enables dispatching, no work |
| `internal/worker/caasapplicationprovisioner/application.go` | 432 | `appUnitsWatcher` | Only sets `reconcileDeadChan` timer — enables dispatching, no work |

## Deviations from spec

1. **`controllerpresence/worker.go`** — `subscriber` is an `apiremotecaller.Subscription`, not a `watcher.Watcher[T]`. It has no `ChangeContext` method. The span uses `ctx` as the parent context instead of `subscriber.ChangeContext(ctx)`.

2. **`dbaccessor/worker.go`** — `w.cfg.ControllerConfigWatcher` is a `controlleragentconfig.ConfigWatcher`, not a `watcher.Watcher[T]`. It has no `ChangeContext` method. The span uses `ctx` as the parent context instead.

3. **`upgrader/upgrader.go` and `caasupgrader/upgrader.go`** — The `versionWatcher.Changes()` case shares code with a `case <-retry:` case after the select statement. The span is created inside the case and ended immediately, recording only the "received version change notification" event. The actual upgrade work runs after the select and is shared with the retry path, so it cannot be exclusively attributed to the watcher change.

4. **`firewaller/firewaller.go`** — Added `scopedContext()` method to `machineData` struct, which was missing but needed by the `watchLoop` method that was already calling it.

5. **`firewaller/firewaller.go`** — In `machineData.watchLoop`, the `ctx` returned by `trace.Start` shadows the outer `ctx` but is not used (the span is just for recording the event). Added `_ = ctx` to suppress the "declared and not used" compiler error.

6. **`firewaller/firewaller.go`** — Fixed duplicate `err` declaration in `subnetsChanged` method (the named return `(err error)` conflicted with `var err error` inside the function body). Removed the inner `var err error`.
