# Task 09 — CLI Commands: `debug-pause`, `debug-step`, `debug-resume` (completed)

## Registered command names

- `debug-pause`
- `debug-step`
- `debug-resume`

## Flag names and defaults

### All three commands

| Flag | Short | Type | Default | Purpose |
|------|-------|------|---------|---------|
| `--model` | `-m` | string | `""` (current model) | Target a specific model |
| `--all` | | bool | `false` | Target all databases |
| `--controller-db` | | bool | `false` | Target the controller database only |

### `debug-step` only

| Flag | Type | Default | Purpose |
|------|------|---------|---------|
| `--count` | int | `1` | Number of transactions to step |

## Files created

| File | Purpose |
|------|---------|
| `api/client/debugchangestream/doc.go` | Package documentation |
| `api/client/debugchangestream/client.go` | API client wrapping DebugChangeStream facade |
| `cmd/juju/commands/debugchangestream.go` | Three CLI commands and shared helpers |
| `cmd/juju/commands/debugchangestream_test.go` | Unit tests (22 tests) |

## registerCommands location

`cmd/juju/commands/main.go` — three `r.Register(...)` calls added after
`r.Register(newDebugLogCommand(nil))` and before
`r.Register(ssh.NewDebugHooksCommand(...))`.

## Deviations from spec

1. **`--controller` renamed to `--controller-db`** — The spec used
   `--controller` as a BoolVar, but `modelcmd.WrapController` already
   registers `--controller` (and `-c`) as a StringVar for selecting
   which controller to connect to. Using the same flag name would cause
   a flag redefinition panic at runtime. Renamed to `--controller-db`
   to disambiguate: it targets the controller *database*, not the
   controller *endpoint*.

2. **`resolveTarget` returns three values** — The spec's
   `resolveTarget` signature returned only
   `(params.DebugChangeStreamTarget, error)`. The implementation
   returns `(params.DebugChangeStreamTarget, string, error)` where the
   second string is the model name, used for human-readable output
   instead of the raw UUID.

3. **Pause `--all` output is a single line** — The spec shows
   `Change stream paused (all).` as a single summary line for `--all`.
   The implementation follows this exactly rather than iterating over
   per-database results.

4. **No `--format json` option** — Matches the spec; these commands
   are for human consumption only.
