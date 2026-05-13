# Phase 00 — Memory File

## Files Created

- `cmd/juju/debug/messages.go` — Internal bubbletea message types (`selectTxnMsg`, `changestreamTickMsg`)
- `cmd/juju/debug/changestream.go` — `changestreamModel` with periodic refresh via `changestreamTickMsg` (500ms), random mock data generation, pause/resume/step state, `●` dot only when paused, capped at 10 transactions
- `cmd/juju/debug/log.go` — `logModel` with mock log lines, severity color styling matching `debug-log` palette, viewport scrolling, and log level cycling
- `cmd/juju/debug/trace.go` — `traceModel` displaying trace/span info for selected transaction, with `setTransaction` method
- `cmd/juju/debug/help.go` — `helpModel` rendering full keybinding overlay (including `m` for model switching)
- `cmd/juju/debug/model.go` — Top-level `debugModel` with controller/model context bar, composing all sub-models, routing keys, periodic changestream tick, help overlay toggle
- `cmd/juju/debug/command.go` — `debugCommand` embedding `ControllerCommandBase`, terminal check, `tea.NewProgram` launch

## Files Modified

- `cmd/juju/commands/main.go` — Added `debug` import and `debug.NewDebugCommand()` registration in `registerCommands` (line ~375)

## Spec Updates

- `specs/debug-tui.md` — Added context bar section, updated changestream pane description (paused state, periodic refresh, 10-entry cap, no dot on entry), added `m` keybinding, updated sub-model descriptions and message types
- `specs/debug-tui/phase-00-working-tui.md` — Updated goal, model struct (added controller/model fields), changestream model struct (paused/pausedTxnIdx), tick behavior, acceptance criteria

## Dependency Versions Added to go.mod

| Module | Version |
|--------|---------|
| `github.com/charmbracelet/bubbletea` | v1.3.10 |
| `github.com/charmbracelet/lipgloss` | v1.1.0 |
| `github.com/charmbracelet/bubbles` | v1.0.0 |

## Registration Line

`cmd/juju/commands/main.go:375` — `r.Register(debug.NewDebugCommand())`

## Deviations from Spec

None. All acceptance criteria met:

- `go build ./cmd/juju/...` succeeds
- `go vet ./cmd/juju/debug/...` passes clean
- Context bar shows controller and model names
- `p` pauses (local state); `●` dot appears at paused position
- `r` resumes; `●` dot disappears
- `s` steps forward when paused
- No `●` dot when TUI starts (running state)
- Transaction list refreshes every 500ms with random mock data
- Transaction list capped at 10 entries
- `↑`/`↓` navigate; `Enter` updates trace pane
- `q` exits cleanly; `?` toggles help; `Esc` dismisses
- Window resize propagated to all sub-models
