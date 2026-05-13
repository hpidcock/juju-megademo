# Task 08 — API Facade: `DebugChangeStream`

## Goal

Create the `DebugChangeStream` API facade that the CLI commands call.
The facade writes to `debug_change_stream` via the domain service from
task 07 and co-ordinates across multiple databases when `--all` is
requested by the CLI.

## Dependencies

- **Task 07** — the `debugchangestream` domain service must exist.

Must complete before task 09.

## Memory Files to Read

Before writing any code, read the following memory files from
completed dependency tasks:

- `specs/debug-changestream/memory/task-07.md` — full `State`
  interface and `Service` method signatures as actually implemented,
  all file paths created, and the DI registration approach used.
  Use this to wire the facade to the correct domain service without
  re-reading the domain package from scratch.

## Research Required

Before writing any code, read the following:

- `apiserver/facades/controller/migrationmaster/register.go` and its
  `newMigrationMasterFacade` constructor — this is the canonical example
  of a `MultiModelContext` facade that accesses per-model domain services.
- `apiserver/facades/controller/migrationtarget/migrationtarget.go` —
  the `checkAuth` function for the superuser-check pattern.
- `apiserver/facade/` — the `MultiModelContext` interface; specifically
  `DomainServicesForModel` and `ControllerDomainServices`.
- `apiserver/facades/controller/` — list existing controller facades to
  choose a suitable directory name.
- Search for where new controller facades are registered in the API
  server's facade registry (look for a file that calls `Register` for
  many facades, e.g. `apiserver/register.go` or similar).
- `rpc/params/` — find the existing params convention (structs used as
  request/response types for API methods). Read one simple example.

## Scope

### `apiserver/facades/controller/debugchangestream/register.go`

```go
func Register(registry facade.FacadeRegistry) {
    registry.MustRegisterForMultiModel(
        "DebugChangeStream", 1,
        func(
            stdCtx context.Context,
            ctx facade.MultiModelContext,
        ) (facade.Facade, error) {
            return newFacade(stdCtx, ctx)
        },
        reflect.TypeFor[*API](),
    )
}
```

Register this `Register` function in the central facade registry file
(identified during research).

### `apiserver/facades/controller/debugchangestream/facade.go`

```go
// API implements the DebugChangeStream facade.
type API struct {
    auth           facade.Authorizer
    controllerTag  names.ControllerTag
    controllerSvc  DebugChangeStreamService
    modelSvcGetter ModelServiceGetter
}

// DebugChangeStreamService is the subset of
// domain/debugchangestream/service.Service used by this facade.
type DebugChangeStreamService interface {
    Pause(ctx context.Context) error
    Step(ctx context.Context, count int) (
        []service.StepResult, error,
    )
    Resume(ctx context.Context) error
    Status(ctx context.Context) (string, error)
}

// ModelServiceGetter returns a DebugChangeStreamService for a given
// model UUID.
type ModelServiceGetter func(
    ctx context.Context, modelUUID model.UUID,
) (DebugChangeStreamService, error)
```

**Auth check** — all methods call a shared helper before doing any work:

```go
func (api *API) checkAuth(ctx context.Context) error {
    if !api.auth.AuthClient() {
        return apiservererrors.ErrPerm
    }
    return api.auth.HasPermission(
        ctx, permission.SuperuserAccess, api.controllerTag,
    )
}
```

**Methods:**

```go
// Pause pauses the targeted changestream(s).
func (api *API) Pause(
    ctx context.Context, args params.DebugChangeStreamArgs,
) (params.DebugChangeStreamPauseResult, error)

// Step advances the paused changestream(s) by args.Count transactions.
func (api *API) Step(
    ctx context.Context, args params.DebugChangeStreamStepArgs,
) (params.DebugChangeStreamStepResult, error)

// Resume resumes the targeted changestream(s).
func (api *API) Resume(
    ctx context.Context, args params.DebugChangeStreamArgs,
) error
```

### `rpc/params/debugchangestream.go`

Define the wire types:

```go
// DebugChangeStreamTarget selects which database(s) to target.
type DebugChangeStreamTarget struct {
    // ModelUUID targets a specific model. Mutually exclusive with
    // Controller and All.
    ModelUUID string `json:"model-uuid,omitempty"`
    // Controller targets only the controller database.
    Controller bool `json:"controller,omitempty"`
    // All targets all model databases and the controller database.
    All bool `json:"all,omitempty"`
}

type DebugChangeStreamArgs struct {
    Target DebugChangeStreamTarget `json:"target"`
}

type DebugChangeStreamStepArgs struct {
    Target DebugChangeStreamTarget `json:"target"`
    Count  int                     `json:"count"`
}

// DebugChangeStreamDBResult holds the result for one database.
type DebugChangeStreamDBResult struct {
    // Name is "controller" or the model UUID.
    Name       string `json:"name"`
    // TxnMin is the lowest txn_id consumed (0 if already at head).
    TxnMin     int64  `json:"txn-min"`
    // TxnMax is the highest txn_id consumed.
    TxnMax     int64  `json:"txn-max"`
    // EventCount is the number of change events that became visible.
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

### Multi-database coordination

When `Target.All = true`:

1. Call `ctx.ControllerDomainServices()` to get the controller service.
2. List all known model UUIDs (via the model domain service accessible
   from `ctx.ControllerDomainServices()`).
3. For each model UUID call `ctx.DomainServicesForModel(stdCtx, uuid)`
   to get the per-model domain services, then extract the
   `DebugChangeStreamService`.
4. Invoke the requested operation on all services, collect
   `DebugChangeStreamDBResult` entries.

Operations across multiple databases are run concurrently (use
`errgroup` or equivalent) to avoid one slow database blocking the rest.

When `Target.Controller = true`, only the controller service is used.

When `Target.ModelUUID` is set, only that model's service is used.

When no target is specified, the facade returns an error asking the
caller to specify a target (the CLI always sets one).

## Sub-Agent Testing

To prevent context ballooning, delegate all test writing and test
execution to a sub-agent. The sub-agent's write scope is limited to
test files only — it must not modify production code.

When spawning the sub-agent, provide it with:
- The full paths of every production file you have written.
- The acceptance criteria from this task.
- The exact `go test` commands to run.

The sub-agent must:
1. Write the unit tests described in the acceptance criteria
   (superuser check, multi-model `Step` coordination).
2. Run `go test ./apiserver/facades/controller/debugchangestream/...`
   and report any failures.
3. Fix test failures (within test files only) until the suite passes.
4. Report the final `go test` output back to you.

Do not proceed to the Memory File step until the sub-agent reports
a passing test suite.

## Memory File

On completion, write `specs/debug-changestream/memory/task-08.md`
containing:

- The facade name and version registered.
- The full `API` struct field list.
- The full `params` wire type definitions as written.
- All file paths created.
- The central registry file path where `Register` was called.
- How `DomainServicesForModel` was used to obtain per-model services.
- Any deviations from this task spec and the reason.

## Acceptance Criteria

- The facade is registered under the name `"DebugChangeStream"` at
  version `1`.
- All methods check superuser access and return `ErrPerm` otherwise.
- `Pause`, `Step`, and `Resume` delegate to the domain service.
- Multi-database coordination works correctly for `Target.All`.
- `go test ./apiserver/facades/controller/debugchangestream/...` passes.
- A unit test covers the superuser check (non-superuser receives
  `ErrPerm`).
- A unit test covers multi-model coordination for `Step` with
  `Target.All = true`.
