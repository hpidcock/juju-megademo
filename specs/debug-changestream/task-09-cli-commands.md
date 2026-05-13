# Task 09 — CLI Commands: `debug-pause`, `debug-step`, `debug-resume`

## Goal

Implement the three CLI commands that call the `DebugChangeStream` API
facade. The commands share a common scope flag set and produce the output
described in the specification.

## Dependencies

- **Task 08** — the `DebugChangeStream` facade and its `params` types
  must exist.

This is the final task in the chain.

## Memory Files to Read

Before writing any code, read the following memory files from
completed dependency tasks:

- `specs/debug-changestream/memory/task-08.md` — facade name and
  version registered, full `params` wire type definitions as written,
  all file paths created, and the central registry file path where
  `Register` was called. Use this to write the API client against
  the exact method signatures implemented rather than re-reading the
  facade from scratch.

## Research Required

Before writing any code, read the following:

- `cmd/juju/commands/debuglog.go` — review as a structural example for
  `Info()`, `SetFlags()`, `Init()`, `Run()`, and the constructor pattern.
  Note however that `debuglog` uses `ModelCommandBase`; these commands
  use `ControllerCommandBase` instead.
- `cmd/juju/commands/main.go` — find `registerCommands` to understand
  exactly where new commands are added.
- `api/client/debugchangestream/` — this client package must be created
  as part of this task (see scope below); check `api/client/` for the
  naming convention of existing client packages.
- `rpc/params/debugchangestream.go` from task 08 — the wire types the
  client will use.

## Scope

### `api/client/debugchangestream/client.go`

A thin API client that wraps the facade calls:

```go
// Client provides access to the DebugChangeStream API facade.
type Client struct {
    base.ClientFacade
    facade base.FacadeCaller
}

// NewClient returns a new DebugChangeStream client.
func NewClient(caller base.APICallCloser) *Client

func (c *Client) Pause(
    ctx context.Context, target params.DebugChangeStreamTarget,
) (params.DebugChangeStreamPauseResult, error)

func (c *Client) Step(
    ctx context.Context,
    target params.DebugChangeStreamTarget,
    count int,
) (params.DebugChangeStreamStepResult, error)

func (c *Client) Resume(
    ctx context.Context, target params.DebugChangeStreamTarget,
) error
```

### `cmd/juju/commands/debugchangestream.go`

All three commands live in a single file.

#### Shared scope flags

A helper `addScopeFlags` function attaches the scope flags to a
`*gnuflag.FlagSet`. Both `--all` and `--controller` are `BoolVar`;
`--model` is a `StringVar` that defaults to the current model. When
neither `--controller` nor `--all` is specified and `--model` is empty,
the current model is used as the target.

The `Init` method validates that `--all` and `--controller` are not set
simultaneously, and that `--model` is not combined with `--all` or
`--controller`.

#### `debugPauseCommand`

```go
type debugPauseCommand struct {
    modelcmd.ControllerCommandBase
    model      string
    all        bool
    controller bool
}

func (c *debugPauseCommand) Info() *cmd.Info
func (c *debugPauseCommand) SetFlags(f *gnuflag.FlagSet)
func (c *debugPauseCommand) Init(args []string) error
func (c *debugPauseCommand) Run(ctx *cmd.Context) error
```

`Run` creates a `debugchangestream.Client`, calls `Pause` with the
resolved target, and prints one line per result:

```
Change stream paused (model "mymodel", txn 42).
```

or for `--all`:

```
Change stream paused (all).
```

#### `debugStepCommand`

```go
type debugStepCommand struct {
    modelcmd.ControllerCommandBase
    model      string
    all        bool
    controller bool
    count      int
}
```

`Run` calls `Step` and prints one line per database result:

```
Stepped 1 transaction(s) (txn 43): 3 event(s).
```

For `--all`, one line per database:

```
controller: stepped 1 transaction(s) (txn 19): 1 event(s).
model "f47ac10b-...": stepped 1 transaction(s) (txn 43): 3 event(s).
model "9b2e1c44-...": already at head, 0 event(s).
```

"Already at head" is printed when `TxnMin == 0` and `EventCount == 0`.

#### `debugResumeCommand`

```go
type debugResumeCommand struct {
    modelcmd.ControllerCommandBase
    model      string
    all        bool
    controller bool
}
```

`Run` calls `Resume` and prints:

```
Change stream resumed (model "mymodel").
```

#### Registration in `cmd/juju/commands/main.go`

Add to `registerCommands`:

```go
r.Register(newDebugPauseCommand(nil))
r.Register(newDebugStepCommand(nil))
r.Register(newDebugResumeCommand(nil))
```

Each constructor follows the `newDebugLogCommand` pattern: takes a
`jujuclient.ClientStore`, creates the struct, calls `modelcmd.Wrap`.

## Sub-Agent Testing

To prevent context ballooning, delegate all test writing and test
execution to a sub-agent. The sub-agent's write scope is limited to
test files only — it must not modify production code.

When spawning the sub-agent, provide it with:
- The full paths of every production file you have written.
- The acceptance criteria from this task.
- The exact `go test` commands to run.

The sub-agent must:
1. Write the unit tests described in the acceptance criteria (flag
   mutual-exclusion, target construction, output formatting, "already
   at head" output).
2. Run `go test ./cmd/juju/commands/...` and report any failures.
3. Fix test failures (within test files only) until the suite passes.
4. Report the final `go test` output back to you.

Do not proceed to the Memory File step until the sub-agent reports
a passing test suite.

## Memory File

On completion, write `specs/debug-changestream/memory/task-09.md`
containing:

- The registered command names (as they appear in `juju help`).
- The flag names and their defaults for each command.
- All file paths created.
- The `registerCommands` location where the three commands were added.
- Any deviations from this task spec and the reason.

## Acceptance Criteria

- All three commands are registered and appear in `juju help`.
- `--all` and `--controller` are mutually exclusive; `Init` returns an
  error if both are set.
- `--count` defaults to `1` and rejects values less than `1`.
- Output format matches the specification examples. There is no
  `--format json` option; this tool is not for machine consumption.
- `go test ./cmd/juju/commands/...` passes.
- Unit tests cover:
  - Mutual exclusion of `--all` and `--controller`.
  - `--model` is rejected when combined with `--all` or `--controller`.
  - Correct target construction for each flag combination.
  - Output formatting for single-database and multi-database results.
  - "Already at head" output when `EventCount == 0`.
