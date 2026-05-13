# Task 08 — API Facade: `DebugChangeStream` (completed)

## Facade registration

- Name: `"DebugChangeStream"`
- Version: `1`
- Type: `MultiModelContext` facade (`MustRegisterForMultiModel`)

## Files created

| File | Purpose |
|------|---------|
| `apiserver/facades/controller/debugchangestream/doc.go` | Package documentation |
| `apiserver/facades/controller/debugchangestream/register.go` | Facade registration |
| `apiserver/facades/controller/debugchangestream/facade.go` | Facade implementation |
| `apiserver/facades/controller/debugchangestream/facade_test.go` | Unit tests (3 tests) |
| `rpc/params/debugchangestream.go` | Wire types |

## Central registry file

`apiserver/allfacades.go` — `debugchangestream.Register(registry)` added after
`crosscontroller.Register(registry)` in `AllFacades()`, along with the
corresponding import.

## API struct fields

```go
type API struct {
    auth           facade.Authorizer
    controllerTag  names.ControllerTag
    controllerSvc  DebugChangeStreamService
    modelListSvc   ModelListService
    modelSvcGetter ModelServiceGetter
}
```

`modelListSvc` is an addition over the spec's 4-field struct — it is needed to
list all models for `Target.All` coordination.

## Facade interfaces

```go
type DebugChangeStreamService interface {
    Pause(ctx context.Context) error
    Step(ctx context.Context, count int) ([]service.StepResult, error)
    Resume(ctx context.Context) error
}

type ModelListService interface {
    GetAllModels(ctx context.Context) ([]model.Model, error)
}

type ModelServiceGetter func(
    ctx context.Context, modelUUID model.UUID,
) (DebugChangeStreamService, error)
```

## Params wire types (rpc/params/debugchangestream.go)

```go
type DebugChangeStreamTarget struct {
    ModelUUID  string `json:"model-uuid,omitempty"`
    Controller bool   `json:"controller,omitempty"`
    All        bool   `json:"all,omitempty"`
}

type DebugChangeStreamArgs struct {
    Target DebugChangeStreamTarget `json:"target"`
}

type DebugChangeStreamStepArgs struct {
    Target DebugChangeStreamTarget `json:"target"`
    Count  int                     `json:"count"`
}

type DebugChangeStreamDBResult struct {
    Name       string `json:"name"`
    TxnMin     int64  `json:"txn-min"`
    TxnMax     int64  `json:"txn-max"`
    EventCount int    `json:"event-count"`
    Error      *Error `json:"error,omitempty"`
}

type DebugChangeStreamPauseResult struct {
    Results []DebugChangeStreamDBResult `json:"results"`
}

type DebugChangeStreamStepResult struct {
    Results []DebugChangeStreamDBResult `json:"results"`
}
```

## How DomainServicesForModel is used

In `newFacade`, the `modelSvcGetter` closure calls
`ctx.DomainServicesForModel(mctx, uuid)` to obtain a `services.DomainServices`
for each model, then returns `svc.DebugChangeStream()` from it. The result is
always the model-database implementation, which is correct for per-model
targets.

## ControllerDomainServicesProvider — infrastructure addition

The spec assumes `ctx.ControllerDomainServices()` exists on `MultiModelContext`,
but no such method was present. A new approach was taken instead of modifying
`facade.MultiModelContext`:

### New interface in `internal/services/interface.go`

```go
type ControllerDomainServicesProvider interface {
    ControllerDomainSvc() ControllerDomainServices
}
```

### Implementations added

| File | Method |
|------|--------|
| `internal/worker/domainservices/worker.go` | `(d *domainServices) ControllerDomainSvc()` returns `d.ControllerDomainServices` (the embedded interface field = `ctrlFactory`) |
| `domain/services/testing/suite.go` | `(d *domainServices) ControllerDomainSvc()` returns `d.ControllerServices` (the embedded `*ControllerServices` struct) |

In `newFacade`, the pattern is:
```go
cdsp, ok := ctx.DomainServices().(services.ControllerDomainServicesProvider)
controllerSvc := cdsp.ControllerDomainSvc().DebugChangeStream()
modelListSvc  := cdsp.ControllerDomainSvc().Model()
```

This is necessary because `DomainServices.DebugChangeStream()` is overridden on
the combined type to return the model service (to resolve embedding ambiguity).
`ControllerDomainSvc()` returns the embedded field directly, whose
`DebugChangeStream()` dispatches to the controller database implementation.

## Concurrency model

`Pause`, `Step`, and `Resume` (for multi-target) use `golang.org/x/sync/errgroup`
to run all target services concurrently. For `Pause` and `Step`, per-database
errors are captured into `DebugChangeStreamDBResult.Error` (via
`apiservererrors.ServerError`), so no single failure aborts the others. For
`Resume`, the first error is returned via `errgroup.Wait()`.

## Deviations from spec

1. **`modelListSvc` field added** — the spec showed 4 fields but model listing
   for `Target.All` requires a fifth. `modelListSvc ModelListService` was added.

2. **No `ctx.ControllerDomainServices()` method added to `MultiModelContext`** —
   instead, a `ControllerDomainServicesProvider` interface was introduced and
   implemented on the concrete `domainServices` types (worker and testing).
   This avoids modifying `facade.MultiModelContext` and all its implementations.

3. **`NewAPI` constructor exported** — to allow direct injection in unit tests.
