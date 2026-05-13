# Sequential Implementation Plan (UI-demo first)

**Mode**: Single-track sequential (no worktrees, no parallelism).
**Demo milestone**: Completed step 1 → functional TUI runnable via `go run`.
**Commit pattern**: One logical commit per step after explicit user validation.

## Agreed Steps (from user confirmation 2026-05-13)

1. **Step 1 — Functional mock TUI demo** (first commit)
   - Create `cmd/juju/debug/` directory
   - `dqlite_api.go`: DqliteAPI interface + local mock types + `NewMockDqliteAPI()` returning hardcoded sample data (controller + 2 models, tables, sample query rows)
   - `dqlite_model.go`: full `dqliteModel` struct / `NewDqliteModel(api DqliteAPI, preSelect string)`
   - `dqlite_messages.go`: UpdateMsg / Load*Msg / error handling
   - `dqlite_keymap.go`: keyboard Update (nav panes / query textarea / quit)
   - `dqlite_view.go`: lipgloss panels rendering (databases list, objects, DDL pane, query input + results + truncation banner)
   - `dbdebug.go`: thin `dbDebugCommand` with `Run()` calling `tea.NewProgram(NewDqliteModel(...))`
   - `dbdebug_test.go` (minimal)
   - `go run ./cmd/juju/debug` launches interactive demo TUI (static data only)
   - No command registration, no api/common import, no go.mod change

2. **Step 2 — Real client + registration**
   - Perform 02.1b alignment: replace local types with `api/common` types + add `dqliteAPIImpl`
   - Update `NewDqliteAPI(client)` factory
   - Implement real client (if needed) calling `ConnectControllerStream("/dqlite")`
   - Register `newDBDebugCommand` in main.go (after debug-log line ~373)
   - First real `juju db-debug [--database name]` works (requires live controller)

3. **Step 3 — Backend /dqlite WebSocket handler**
   - Implement phase-00: `apiserver/dqlite.go` + `dqlite_test.go`
   - Wire route + shared.dqliteDBGetter in apiserver.go
   - SQL policy, row limits, cluster introspection, version handshake

4. **Step 4 — Polish, cross-tests & documentation**
   - Integrate tests (race-mode + stress for WS path)
   - Update phase memory files
   - Ensure `make pre-check` / build + command help & usage docs
   - Embed prep notes for future debug TUI

## Constraints
- All rules in AGENTS.*.md apply at every step
- Bubbletea deps deferred until step 2 (or explicit after 1)
- Each step ends by writing `specs/debug-db/memory/step-N.md` summary
- After user OK, `git add && git commit -m "<concise step desc>"`

Start Step 1 now?
