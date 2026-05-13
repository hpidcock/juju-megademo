# Phase 02 — `juju db-debug` Command + Bubbletea TUI

## Goal

`juju db-debug` launches an interactive terminal UI for browsing Dqlite
databases. The TUI uses the same bubbletea/bubbles/lipgloss stack as the
`juju debug` TUI (`specs/debug-tui.md`) and is designed as a composable
model that can later be embedded as a pane or tab in the merged `juju
debug` TUI.

## Dependencies

- **Phase 00** — the `/dqlite` WebSocket handler must exist.
- **Phase 01** — the `api/common/dqlite.go` client must exist.

## Memory Files to Read

- `specs/debug-db/memory/phase-00.md` — route path, value formatter
  behaviour.
- `specs/debug-db/memory/phase-01.md` — exported types, `DqliteClient`
  methods, `OpenDqlite`.
- `specs/debug-tui/memory/phase-00.md` — bubbletea dependency versions,
  model composition pattern, `registerCommands` location.

## Research Required

- `cmd/juju/commands/debuglog.go` — command structure, `SetFlags`,
  `Init`, `Run`, constructor, API connection setup, TTY check with
  `go-isatty`.
- `cmd/juju/commands/main.go` — `registerCommands` location and
  registration pattern.
- `cmd/juju/debug/model.go` — existing `debugModel` structure and
  sub-model composition from phase 00.
- `cmd/modelcmd/base.go` — `ControllerCommandBase` and `Wrap` pattern.
- `github.com/charmbracelet/bubbles/textarea` — multiline text input.
- `github.com/charmbracelet/bubbles/viewport` — scrollable list.
- `github.com/charmbracelet/lipgloss` — styling, borders, layout.
- `github.com/charmbracelet/bubbletea` — `Model`, `Cmd`, `KeyMsg`,
  `WindowSizeMsg`, `tea.EnterAltScreen`/`tea.Quit`.

## Scope

### 1. `DqliteAPI` interface — `cmd/juju/debug/dqlite_api.go`

```go
package debug

import (
    "context"
    "github.com/juju/juju/api/common"
)

// DqliteAPI is the interface consumed by the dqlite TUI model.
// A mock implementation is used in tests.
type DqliteAPI interface {
    Databases(ctx context.Context) ([]common.DqliteDatabase, error)
    Objects(ctx context.Context, ns, kind string) ([]common.DqliteObject, error)
    DDL(ctx context.Context, ns, name string) (string, error)
    Query(ctx context.Context, ns, sql string, limit int) (*common.DqliteQueryResult, error)
    Cluster(ctx context.Context) ([]common.DqliteNode, error)
}
```

A concrete adapter wraps `common.DqliteClient`:

```go
type dqliteAPIImpl struct {
    client *common.DqliteClient
}

func NewDqliteAPI(client *common.DqliteClient) DqliteAPI {
    return &dqliteAPIImpl{client: client}
}

func (a *dqliteAPIImpl) Databases(ctx context.Context) ([]common.DqliteDatabase, error) {
    return a.client.Databases(ctx)
}
// ... each method delegates to the client
```

### 2. `dqliteModel` — `cmd/juju/debug/dqlite_model.go`

```go
type dqliteModel struct {
    width, height int
    focus         dqlitePane
    showHelp      bool
    quitting      bool
    err           string

    // Pre-selection (from CLI flags)
    preSelectDatabase string
    defaultLimit      int

    // Database list
    databases  []common.DqliteDatabase
    selectedDB int

    // Object browser — flat list, kind cycled by ctrl+1/2/3
    kind        string // "table" | "view" | "trigger"
    objects     []common.DqliteObject
    selectedObj int

    // DDL viewer
    ddl string

    // Query editor + results
    queryInput     textarea.Model
    queryColumns   []string
    queryRows      [][]string
    queryCount     int
    queryTruncated bool
    queryError     string

    // Cluster view
    clusterNodes []common.DqliteNode

    api DqliteAPI
}

type dqlitePane int

const (
    dqlitePaneDatabases dqlitePane = iota
    dqlitePaneObjects
    dqlitePaneQuery
    dqlitePaneCluster
)
```

**Factory:**

```go
// NewDqliteModel creates a dqlite browser sub-model suitable for
// composition into a parent TUI (e.g. the future merged juju debug).
func NewDqliteModel(api DqliteAPI) *dqliteModel {
    m := &dqliteModel{
        focus:        dqlitePaneDatabases,
        kind:         "table",
        api:          api,
        defaultLimit: 100,
    }
    m.queryInput = textarea.New()
    m.queryInput.Placeholder = "SELECT ..."
    m.queryInput.ShowLineNumbers = false
    m.queryInput.CharLimit = 0
    return m
}
    m.queryInput = textarea.New()
    m.queryInput.Placeholder = "SELECT ..."
    m.queryInput.ShowLineNumbers = false
    m.queryInput.CharLimit = 0
    return m
}
```

`Init()`:
- Returns `tea.Batch(tea.EnterAltScreen, loadDatabasesCmd)`.

`Update(msg tea.Msg)` handles:

| message | action |
|---------|--------|
| `tea.KeyMsg` `ctrl+c` | `quitting = true`, return `tea.Quit` |
| `tea.KeyMsg` `ctrl+h` | toggle `showHelp` |
| `tea.KeyMsg` `ctrl+r` | fire reload cmd for active pane |
| `tea.KeyMsg` `tab` | `focus = (focus + 1) % 4` |
| `tea.KeyMsg` `shift+tab` | `focus = (focus + 3) % 4` |
| `tea.KeyMsg` `esc` | If help shown: dismiss. If query focused: defocus query. Else: clear error |
| `tea.KeyMsg` `↑`/`↓` | If databases pane: `selectedDB = clamp(selectedDB +/- 1)`. If objects pane: `selectedObj = clamp(selectedObj +/- 1)` |
| `tea.KeyMsg` `ctrl+1` (objects pane) | `kind = "table"`, reload objects |
| `tea.KeyMsg` `ctrl+2` (objects pane) | `kind = "view"`, reload objects |
| `tea.KeyMsg` `ctrl+3` (objects pane) | `kind = "trigger"`, reload objects |
| `tea.KeyMsg` `enter` (databases pane) | reload objects for selected DB |
| `tea.KeyMsg` `enter` (objects pane) | reload DDL for selected object |
| `tea.KeyMsg` `ctrl+enter` (query pane) | execute query, reload results |
| `tea.WindowSizeMsg` | set `width`, `height` |
| `loadDatabasesMsg` | call `api.Databases`, populate `databases`. If `preSelectDatabase` is set, scan the list for a matching `Name` and set `selectedDB` to its index. Then fire `loadObjectsCmd`. |
| `loadObjectsMsg` | call `api.Objects`, populate `objects` |
| `loadDDLMsg` | call `api.DDL`, set `ddl` |
| `loadQueryMsg` | call `api.Query`, set `queryColumns`/`queryRows`/`queryCount`/`queryTruncated`/`queryError` |
| `loadClusterMsg` | call `api.Cluster`, populate `clusterNodes` |
| `errMsg` | set `err` |

When the query textarea is focused (`focus == dqlitePaneQuery`), key
messages that the textarea handles internally (alphanumeric, backspace,
delete, arrows, home, end) are forwarded to `m.queryInput.Update(msg)`.
Only `tab`/`shift+tab`/`ctrl+enter`/`esc`/`ctrl+r` are intercepted before
reaching the textarea.

`View()`:

```
┌─ Databases ─┬─ Objects ─┬─ DDL / Query ──────────────────────────────┐
│ *controller │ [Tables]  │ CREATE TABLE change_log (                  │
│  model-foo  │  change…  │   id         INTEGER PRIMARY KEY,          │
│  model-bar  │  model    │   edit_type… INTEGER NOT NULL,             │
│             │  ...      │   ...                                      │
│             │           ├───────────────────────────────────────────│
│             │           │ SELECT * FROM change_log           [^ENTER]│
│             │           ├───────────────────────────────────────────│
│             │           │ id  │ edit_type_id │ ns_id │ ...           │
│             │           │ 1   │ 1            │ 1     │ ...           │
│             │           │ 3 rows            (truncated)              │
└─────────────┴───────────┴───────────────────────────────────────────┘
┌─ Cluster ────────────────────────────────────────────────────────────┐
│ ID                  Address                    Role                  │
│ 00ab…               10.0.0.1:12345             voter                 │
│ 00cd…               10.0.0.2:12345             stand-by              │
└─────────────────────────────────────────────────────────────────────┘
  ^1/^2/^3 obj kind  Tab focus  ^R refresh  ^H help  ^C quit
```

- Focused pane has a highlighted (thicker or colored) border.
- Databases pane: `*` marks the selected database. `>` marks the cursor
  when navigated with arrow keys.
- Objects pane: `>` marks cursor. Kind label `[Tables]` updated by
  `ctrl+1/2/3`.
- Query pane: upper sub-pane shows DDL output; lower sub-pane shows
  `textarea` + results table below it.
- When `queryError != ""`, display the error in red below the textarea
  instead of the results table.
- When `queryTruncated`, display `(truncated)` in yellow below the table.
- When `err != ""`, display a banner at the top of the screen in red,
  self-clearing after 5 seconds or on `esc`.
- Cluster pane: simple tabulated list of nodes.
- Status bar: contextual keybindings.

### 3. `load*` messages — `cmd/juju/debug/dqlite_messages.go`

```go
type loadDatabasesMsg struct {
    databases []common.DqliteDatabase
    err       error
}

type loadObjectsMsg struct {
    objects []common.DqliteObject
    err     error
}

type loadDDLMsg struct {
    ddl string
    err error
}

type loadQueryMsg struct {
    result *common.DqliteQueryResult
    err    error
}

type loadClusterMsg struct {
    nodes []common.DqliteNode
    err   error
}

type errMsg struct{ err error }
```

Each `load*Cmd` function creates a `tea.Cmd` that calls the API in a
goroutine and returns the corresponding message:

```go
func loadDatabasesCmd(api DqliteAPI) tea.Cmd {
    return func() tea.Msg {
        dbs, err := api.Databases(context.Background())
        return loadDatabasesMsg{databases: dbs, err: err}
    }
}
// ... same pattern for objects, ddl, query, cluster
```

### 4. `dbDebugCommand` — `cmd/juju/debug/dbdebug.go`

```go
type dbDebugCommand struct {
    modelcmd.ControllerCommandBase
    database string // optional: pre-select target database name
    limit    int    // default query row limit
}

func newDbDebugCommand() *dbDebugCommand {
    return &dbDebugCommand{limit: 100}
}

func (c *dbDebugCommand) Info() *cmd.Info {
    return jujucmd.Info(&cmd.Info{
        Name:    "db-debug",
        Purpose: "launch an interactive Dqlite database browser",
        Doc:     "Launches a terminal UI for browsing and querying Juju Dqlite databases.",
    })
}

func (c *dbDebugCommand) SetFlags(f *gnuflag.FlagSet) {
    c.ControllerCommandBase.SetFlags(f)
    f.StringVar(&c.database, "database", "", "Pre-select target database (controller or model name)")
    f.IntVar(&c.limit, "limit", 100, "Default query row limit (1-1000)")
}

func (c *dbDebugCommand) Init(args []string) error {
    if c.limit < 1 || c.limit > 1000 {
        return errors.Errorf("--limit must be between 1 and 1000")
    }
    return cmd.CheckEmpty(args)
}

func (c *dbDebugCommand) Run(ctx *cmd.Context) error {
    if !isatty.IsTerminal(os.Stdout.Fd()) {
        return errors.New("juju db-debug requires an interactive terminal")
    }

    conn, err := c.NewAPIRoot()
    if err != nil {
        return errors.Trace(err)
    }
    defer conn.Close()

    client, err := common.OpenDqlite(ctx, conn)
    if err != nil {
        return errors.Trace(err)
    }
    defer client.Close()

    model := NewDqliteModel(NewDqliteAPI(client))
    model.preSelectDatabase = c.database
    model.defaultLimit = c.limit
    return nil
}
```

### 5. Registration

In `cmd/juju/commands/main.go`, `registerCommands()`:

```go
r.Register(newDbDebugCommand())
```

### 6. Help overlay

When `showHelp` is true, render a full-screen bordered overlay listing all
keybindings:

```
┌─ Help ───────────────────────────────────────────────────────────────┐
│                                                                      │
│  Tab          Next pane                     Ctrl+1..3  Object kind   │
│  Shift+Tab    Previous pane                 Ctrl+H     This help     │
│  ↑/↓          Navigate list                 Ctrl+R     Refresh pane  │
│  Enter        Select database / object      Esc        Dismiss       │
│  Ctrl+Enter   Execute query                 Ctrl+C     Quit          │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

### 7. Tests

Create `cmd/juju/debug/dbdebug_test.go`.

Define a `mockDqliteAPI` implementing `DqliteAPI` that returns
preconfigured results and records which methods were called with which
arguments.

Cover:

- Command registration — `juju help` lists `db-debug`.
- `--limit` validation — rejects 0 and 1001.
- Non-TTY rejection — error message when stdout is not a terminal.
- TUI initial state — after `Init`, model shows loading state, fires
  `loadDatabasesCmd`.
- `loadDatabasesMsg` populates `databases` list, clears loading.
- `loadDatabasesMsg` with `preSelectDatabase` set selects the matching
  database and fires `loadObjectsCmd`.
- `loadObjectsMsg` populates `objects` list for selected DB and kind.
- `loadDDLMsg` populates `ddl` string.
- `loadQueryMsg` populates columns, rows, count, truncated flag.
- `loadClusterMsg` populates cluster nodes.
- `errMsg` sets `err` field (visible in status bar).
- Tab cycling — `focus` rotates through panes.
- `ctrl+1/2/3` changes `kind` and fires reload in objects pane.
- `↑`/`↓` moves cursor in databases and objects panes.
- `enter` in databases pane fires `loadObjectsCmd`.
- `enter` in objects pane fires `loadDDLCmd`.
- `ctrl+enter` in query pane fires `loadQueryCmd`.
- `ctrl+h` toggles `showHelp`.
- `ctrl+c` sets `quitting` and returns `tea.Quit`.
- Query textarea captures alphanumeric keys when focused — `q`, `s`, `r`,
  `p` do NOT trigger any action.

TUI tests inject the mock API and simulate keypresses via
`m.Update(tea.KeyMsg{...})`. No real terminal required.

Run: `go test ./cmd/juju/debug/...`.

## Memory File

On completion, write `specs/debug-db/memory/phase-02.md`:

- The registered command name as it appears in `juju help`.
- The `DqliteAPI` interface as implemented (method signatures).
- All file paths created.
- The `registerCommands` location.
- The `DqliteClient` wiring path (command → `api/common.OpenDqlite` →
  `NewDqliteAPI` → `NewDqliteModel`).
- Any deviations from this phase spec and the reason.

## Acceptance Criteria

- `juju db-debug` launches an interactive TUI when run in a terminal.
- `juju db-debug` prints an error and exits when stdout is not a TTY.
- The databases pane lists "controller" and all model databases.
- The objects pane lists tables / views / triggers for the selected DB.
- `ctrl+1`/`ctrl+2`/`ctrl+3` cycle the object kind in the objects pane.
- DDL displays for a selected object.
- Read-only SQL queries execute and display results as a table.
- Mutation statements return an error displayed in the TUI.
- Query results show `(truncated)` indicator when the row limit is hit.
- The cluster pane shows node ID, address, and role.
- The query textarea captures all alphanumeric input when focused.
- Plain keys (`q`, `s`, `r`, `p`, digits) do not trigger actions.
- `go build ./cmd/juju/...` succeeds.
- `go test ./cmd/juju/debug/...` passes.
- TUI tests pass with mock API (no real controller required).
