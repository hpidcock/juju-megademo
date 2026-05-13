# Phase 00 — Working `juju debug` Command with Bubbletea TUI

## Goal

`juju debug` launches a TUI that renders the 3-pane layout with mock data.
No API calls, no model selection, no real data. This phase establishes the
bubbletea program structure, sub-model composition, keybinding routing, and
terminal lifecycle management.

## Dependencies

None. This phase may begin immediately.

## Research Required

Before writing any code, read the following:

- `cmd/juju/commands/debuglog.go` — structural example for `Info()`,
  `SetFlags()`, `Init()`, `Run()`, and the constructor pattern.
- `cmd/juju/commands/main.go` — find `registerCommands` to understand
  exactly where new commands are added.
- `cmd/juju/commands/version.go` — simplest example of a command
  embedding `cmd.CommandBase`.
- `cmd/modelcmd/base.go` — understand `ControllerCommandBase` and the
  `WrapBase` / `Wrap` patterns.
- `cmd/cmd.go` — the `cmd.Command` interface and `cmd.Context`.
- `go.mod` — verify no bubbletea dependencies exist yet.

## Scope

### 1. Add bubbletea dependencies to `go.mod`

Run:

```
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/lipgloss@latest
go get github.com/charmbracelet/bubbles@latest
```

### 2. Create `cmd/juju/debug/model.go`

Top-level bubbletea model:

```go
type debugModel struct {
    width        int
    height       int
    changestream changestreamModel
    log          logModel
    trace        traceModel
    help         helpModel
    showHelp     bool
    quitting     bool
}
```

Implement `Init()`, `Update()`, and `View()`:

- `Init()` returns a batch of all sub-model `Init()` results plus a
  `tea.EnterAltScreen` command.
- `Update()` routes key messages to the appropriate sub-model:
  - `q` → set `quitting = true`, return `tea.Quit`
  - `?` → toggle `showHelp`
  - `s`, `S`, `p`, `r` → forward to `changestreamModel` (no-op in this
    phase, but must be routed)
  - `↑`, `↓`, `Enter` → forward to `changestreamModel`
  - `l`, `/` → forward to `logModel`
  - `Esc` → forward to whichever sub-model has an active input
  - `tea.WindowSizeMsg` → update `width`, `height`, propagate to all
    sub-models
- `View()`:
  - If `showHelp`, render `helpModel.View()` as a full-screen overlay.
  - Otherwise, lay out the three panes vertically using lipgloss:
    - Changestream pane (top, ~40% of height)
    - Log pane (middle, ~35% of height)
    - Trace pane (bottom, ~25% of height)
  - Each pane has a bordered box with a title in the top-left corner.

### 3. Create `cmd/juju/debug/changestream.go`

```go
type changestreamModel struct {
    width          int
    height         int
    transactions   []transactionEntry
    cursor         int
    currentTxnIdx  int
}

type transactionEntry struct {
    TxnID      int64
    EventCount int
    TraceID    string
    SpanID     string
}
```

- `Init()` returns nil.
- `Update()`:
  - `s`, `p`, `r` → no-op (return model unchanged).
  - `↑` → decrement `cursor` (clamped to 0).
  - `↓` → increment `cursor` (clamped to `len(transactions)-1`).
  - `Enter` → set `currentTxnIdx = cursor`, emit a `selectTxnMsg` to
    the parent so the trace pane updates.
- `View()`:
  - Header bar: left-aligned "Changestream", centre-aligned
    `[s]tep [r]esume [q]uit`, right-aligned (empty in this phase).
  - Transaction list: each row formatted as:
    - `● <txnID>  <N> events  trace: <traceID>…` for the current
      transaction (`currentTxnIdx`).
    - `  <txnID>  <N> events  trace: <traceID>…` for all others.
  - Highlight the row at `cursor` using lipgloss reverse video.
- Populate `transactions` with a hardcoded mock:
  - `{TxnID: 42, EventCount: 3, TraceID: "abc123def456", SpanID: "1234567890ab"}`
  - `{TxnID: 41, EventCount: 1, TraceID: "def789abc012", SpanID: "fedcba098765"}`
  - `{TxnID: 40, EventCount: 5, TraceID: "ghi345jkl678", SpanID: "1a2b3c4d5e6f"}`
- Set `currentTxnIdx = 0` and `cursor = 0`.

### 4. Create `cmd/juju/debug/log.go`

```go
type logModel struct {
    width      int
    height     int
    viewport   viewport.Model
    lines      []string
    levelIndex int
}
```

- `Init()` returns nil.
- `Update()`:
  - `l` → cycle `levelIndex` (no visible effect in this phase since
    lines are static).
  - `/` → no-op in this phase.
- `View()`:
  - Render the `viewport` with mock log lines, each styled with
    `ansiterm` severity colors matching the `debug-log` palette:
    - `TRACE` → default foreground
    - `DEBUG` → green
    - `INFO` → bright blue
    - `WARNING` → yellow
    - `ERROR` → bright red
  - Border with title "Log".

Mock log lines:

```
10:42:01 INFO  juju.worker.uniter handling configure
10:42:01 DEBUG juju.changestream dispatching term 42
10:42:00 INFO  juju.worker.caas starting
10:41:59 WARN  juju.api reconnecting
10:41:58 ERROR juju.db connection lost
```

### 5. Create `cmd/juju/debug/trace.go`

```go
type traceModel struct {
    width   int
    height  int
    entries []spanEntry
}

type spanEntry struct {
    Operation string
    Duration  string
}
```

- `Init()` returns nil.
- `Update()`:
  - On receiving a `selectTxnMsg`, update `entries` to match the
    selected transaction's mock spans.
- `View()`:
  - Render header: `txn <id> — trace <traceID> — span <spanID>`.
  - Render each span entry indented with `→`.
  - Border with title "Trace".
  - When no transaction is selected, show placeholder: "Select a
    transaction to view traces."

Mock spans for txn 42 (traceID: abc123def456, spanID: 1234567890ab):

```
→ juju:API.AddRelation    1.2ms
→ juju:changestream.write 0.3ms
→ juju:worker.uniter      0.8ms
```

### 6. Create `cmd/juju/debug/help.go`

```go
type helpModel struct {
    width  int
    height int
}
```

- `View()` renders a bordered overlay listing all keybindings from the
  spec's keybinding table.

### 7. Create `cmd/juju/debug/messages.go`

Define all internal bubbletea messages:

```go
type selectTxnMsg struct {
    txnIndex int
}
```

### 8. Create `cmd/juju/debug/command.go`

```go
type debugCommand struct {
    modelcmd.ControllerCommandBase
}

func newDebugCommand() *debugCommand {
    return &debugCommand{}
}

func (c *debugCommand) Info() *cmd.Info
func (c *debugCommand) SetFlags(f *gnuflag.FlagSet)
func (c *debugCommand) Init(args []string) error
func (c *debugCommand) Run(ctx *cmd.Context) error
```

- `Info()` returns name `"debug"`, purpose, and a short doc string.
- `SetFlags()` is empty in this phase (flags are added in later phases).
- `Init()` accepts no positional args.
- `Run()`:
  - Check if stdout is a terminal using `go-isatty`. If not, print an
    error and return.
  - Create a `debugModel` with default mock data.
  - Create `tea.NewProgram(model, tea.WithAltScreen())`.
  - Call `program.Run()`.
  - On return, exit cleanly.

### 9. Register `juju debug` in `cmd/juju/commands/main.go`

Add to `registerCommands`:

```go
r.Register(newDebugCommand())
```

## Memory File

On completion, write `specs/debug-tui/memory/phase-00.md` containing:

- All file paths created.
- The exact bubbletea/lipgloss/bubbles versions added to `go.mod`.
- The `registerCommands` line number where the command was added.
- Any deviations from this phase spec and the reason.

## Acceptance Criteria

- `go build ./cmd/juju/...` succeeds.
- Running `juju debug` (with a terminal) displays the 3-pane TUI with
  mock data.
- Arrow keys navigate the transaction list; `Enter` updates the Trace
  pane.
- `q` exits the TUI cleanly and restores the terminal.
- `?` shows the help overlay; `Esc` or `?` again dismisses it.
- `s`, `p`, `r` are accepted without crashing (no-op).
- Terminal resize is handled without rendering artifacts.
