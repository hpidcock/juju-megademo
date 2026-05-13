# Task 1 Memory — TUI Model with Hardcoded Mock Data

## Files Created

| File | Purpose |
|------|---------|
| `cmd/juju/debug/dqlite_api.go` | `DqliteAPI` interface (5 methods), local mirror types (`DqliteDatabase`, `DqliteObject`, `DqliteNode`, `DqliteQueryResult`), `dqliteAPIImpl` with `panic("not implemented")` stubs |
| `cmd/juju/debug/dqlite_messages.go` | `loadDatabasesMsg`, `loadObjectsMsg`, `loadDDLMsg`, `loadQueryMsg`, `loadClusterMsg`, `errMsg` + `load*Cmd` factory functions |
| `cmd/juju/debug/dqlite_model.go` | `dqliteModel` struct, `NewDqliteModel`, `Init`, `Update`, `View` — full 4-pane layout with hardcoded mock data |

## Dependencies Added

- `github.com/charmbracelet/bubbletea v1.3.10`
- `github.com/charmbracelet/bubbles v1.0.0`
- `github.com/charmbracelet/lipgloss v1.1.0`

Added via `go get` + `go mod tidy`.

## Interface

```go
type DqliteAPI interface {
    Databases(ctx context.Context) ([]DqliteDatabase, error)
    Objects(ctx context.Context, ns, kind string) ([]DqliteObject, error)
    DDL(ctx context.Context, ns, name string) (string, error)
    Query(ctx context.Context, ns, sql string, limit int) (*DqliteQueryResult, error)
    Cluster(ctx context.Context) ([]DqliteNode, error)
}
```

## Hardcoded Mock Data

- **Databases**: `controller` (type: controller), `lxd-pilot` (type: model, UUID: deadbeef-…)
- **Objects**: `change_log` (table), `model` (table), `v_model_status` (view)
- **DDL**: `CREATE TABLE change_log (id INTEGER PRIMARY KEY, edit_type_id INTEGER NOT NULL, ns_id INTEGER NOT NULL)`
- **Cluster**: voter at `10.0.0.1:12345` (ID: `00ab1234`), stand-by at `10.0.0.2:12345` (ID: `00cd5678`)
- **Query results**: 3 columns (`id`, `edit_type_id`, `ns_id`), 3 rows, `truncated: true`

## Model Behaviour

- `Init()` returns `tea.Batch(tea.EnterAltScreen, hardcodedLoadAllCmd())` — all mock data loaded in one message
- `hardcodedAllMsg` populates all model fields simultaneously
- Key routing: `ctrl+c` quit, `ctrl+h` help, `ctrl+r` refresh, `tab`/`shift+tab` focus cycle, `↑`/`↓` cursor, `enter` select, `ctrl+1/2/3` kind switch, `ctrl+enter` execute query
- Query textarea receives all alphanumeric input when query pane focused; `tab`/`shift+tab`/`ctrl+enter`/`esc`/`ctrl+r` intercepted first
- Focused pane has purple border (`color 63`); unfocused has gray (`color 240`)

## Wiring Location (Task 2)

- Command registration: `cmd/juju/commands/main.go` — not yet modified (deferred to Task 2)
- Real API wiring: `dqliteAPIImpl` methods currently `panic("not implemented")` — deferred to Task 6

## Deviations from Spec

- Combined all mock data into a single `hardcodedAllMsg` rather than separate `loadDatabasesCmd` returning hardcoded values then triggering real API calls. This avoids `panic("not implemented")` during the visual demo phase while keeping the `load*Cmd` factories ready for Task 6 wiring.
- Used local mirror types instead of `common.Dqlite*` (Phase 01 types don't exist yet), with a TODO comment.

## Verification

`go build ./cmd/juju/debug/...` passes.
