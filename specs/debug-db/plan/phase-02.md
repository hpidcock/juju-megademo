# Phase 02 — `juju db-debug` Command + Bubbletea TUI

## Goal

`juju db-debug` launches an interactive terminal UI for browsing Dqlite
databases. Uses bubbletea/bubbles/lipgloss stack. Designed as a composable
`Model` embeddable in future `juju debug` TUI.

## Dependencies

- Phase 01.1 (exported types) for final type alignment.
- Phase 01.2 (client methods) for 02.6 real-API wiring.
- No backend dependency for 02.1–02.5 (mock-driven development).

## Task Breakdown

```
02.1 (DqliteAPI + local types) ─────────────────────────────┐
02.2 (dqliteModel + factory) ──┐                             │
02.3 (load* messages) ─────────┤                             │
                               ├──► 02.4 (keyboard) ──► 02.5 (View/render)
02.1b (align to api/common) ───┘
                                                               │
02.6 (command + registration) ◄── needs 01.3 ◄──────────────────┘
```

**02.1 and 02.1b are two stages of the same file:**
- **02.1**: Define the `DqliteAPI` interface using local types (no import of
  `api/common`). This allows TUI development to start before 01.1.
- **02.1b**: Once 01.1 is complete, replace local types with
  `common.DqliteDatabase` etc. This is a search-and-replace operation —
  the shapes are identical.

**02.2–02.5 can start immediately** with a mock `DqliteAPI`. These tasks
can themselves be partially parallelized:
- 02.2 (struct) and 02.3 (messages) can be done in parallel.
- 02.4 needs 02.2 + 02.3.
- 02.5 needs 02.4.

**02.6** is the final wiring step. It needs 01.3 (real client) and 02.5
(TUI ready).

### 02.1 — `DqliteAPI` Interface (Local Types Stage)

**File**: `cmd/juju/debug/dqlite_api.go`

**Important**: This directory does not exist. Create `cmd/juju/debug/`.

```go
package debug

import "context"

// DqliteAPI is the interface consumed by the dqlite TUI model.
// A mock implementation is used in tests.
type DqliteAPI interface {
    Databases(ctx context.Context) ([]dqliteDatabase, error)
    Objects(ctx context.Context, ns, kind string) ([]dqliteObject, error)
    DDL(ctx context.Context, ns, name string) (string, error)
    Query(ctx context.Context, ns, sql string, limit int) (*dqliteQueryResult, error)
    Cluster(ctx context.Context) ([]dqliteNode, error)
}
```

**Local types** (mirror `api/common` types — replace with imports in 02.1b):

```go
type dqliteDatabase struct {
    Name, UUID, Namespace, Type string
}
type dqliteObject struct {
    Name, Kind string
}
type dqliteNode struct {
    ID, Address, Role string
}
type dqliteQueryResult struct {
    Columns   []string
    Rows      [][]string
    RowCount  int
    Truncated bool
}
```

These local types are **temporary**. They allow 02.2–02.5 to build without
waiting for 01.1. The concrete adapter `dqliteAPIImpl` is defined in 02.1b.

### 02.1b — Align to `api/common` Types

After 01.1 is complete:

1. Replace local `dqliteDatabase` → `common.DqliteDatabase` (and same for
   all four types).
2. Update `DqliteAPI` interface signatures to use `common.*` types.
3. Add the concrete adapter:

```go
import "github.com/juju/juju/api/common"

type dqliteAPIImpl struct {
    client *common.DqliteClient
}

func NewDqliteAPI(client *common.DqliteClient) DqliteAPI {
    return &dqliteAPIImpl{client: client}
}

func (a *dqliteAPIImpl) Databases(ctx context.Context) ([]common.DqliteDatabase, error) {
    return a.client.Databases(ctx)
}
// ... each method delegates to client
```

4. Update all references in `dqlite_model.go`, `dqlite_messages.go`,
   `dbdebug.go`, and `dbdebug_test.go`.

### 02.2 — `dqliteModel` Struct + Factory

**File**: `cmd/juju/debug/dqlite_model.go`

**Research**: Read existing bubbletea usage in this codebase if any, plus
`cmd/juju/commands/debuglog.go` for command structure patterns.

**Struct**:

```go
type dqliteModel struct {
    width, height int
    focus         dqlitePane
    showHelp      bool
    quitting      bool
    err           string

    preSelectDatabase string
    defaultLimit      int

    databases  []dqliteDatabase  // → []common.DqliteDatabase after 02.1b
    selectedDB int

    kind        string // "table", "view", "trigger"
    objects     []dqliteObject    // → []common.DqliteObject
    selectedObj int

    ddl string

    queryInput     textarea.Model
    queryColumns   []string
    queryRows      [][]string
    queryCount     int
    queryTruncated bool
    queryError     string

    clusterNodes []dqliteNode     // → []common.DqliteNode

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

**Factory**:

```go
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
```

**bubbletea.Model interface**:

```go
func (m *dqliteModel) Init() tea.Cmd {
    return tea.Batch(
        tea.EnterAltScreen,
        loadDatabasesCmd(m.api),
    )
}
```

### 02.3 — `load*` Messages and Cmd Functions

**File**: `cmd/juju/debug/dqlite_messages.go`

**Message types** (one per API call + generic error):

```go
type loadDatabasesMsg struct {
    databases []dqliteDatabase   // → []common.DqliteDatabase after 02.1b
    err       error
}

type loadObjectsMsg struct {
    objects []dqliteObject       // → []common.DqliteObject
    err     error
}

type loadDDLMsg struct {
    ddl string
    err error
}

type loadQueryMsg struct {
    result *dqliteQueryResult    // → *common.DqliteQueryResult
    err    error
}

type loadClusterMsg struct {
    nodes []dqliteNode           // → []common.DqliteNode
    err   error
}

type errMsg struct {
    err error
}
```

**Cmd functions** (each calls the API in a goroutine):

```go
func loadDatabasesCmd(api DqliteAPI) tea.Cmd {
    return func() tea.Msg {
        dbs, err := api.Databases(context.Background())
        return loadDatabasesMsg{databases: dbs, err: err}
    }
}

func loadObjectsCmd(api DqliteAPI, ns, kind string) tea.Cmd {
    return func() tea.Msg {
        objs, err := api.Objects(context.Background(), ns, kind)
        return loadObjectsMsg{objects: objs, err: err}
    }
}

func loadDDLCmd(api DqliteAPI, ns, name string) tea.Cmd {
    return func() tea.Msg {
        ddl, err := api.DDL(context.Background(), ns, name)
        return loadDDLMsg{ddl: ddl, err: err}
    }
}

func loadQueryCmd(api DqliteAPI, ns, sql string, limit int) tea.Cmd {
    return func() tea.Msg {
        result, err := api.Query(context.Background(), ns, sql, limit)
        return loadQueryMsg{result: result, err: err}
    }
}

func loadClusterCmd(api DqliteAPI) tea.Cmd {
    return func() tea.Msg {
        nodes, err := api.Cluster(context.Background())
        return loadClusterMsg{nodes: nodes, err: err}
    }
}
```

### 02.4 — Keyboard Handling (`Update`)

**File**: `cmd/juju/debug/dqlite_model.go` (add `Update` method)

**Key binding table** (from spec phase-02 §2):

| Message | Action |
|---------|--------|
| `ctrl+c` | `quitting = true`, return `tea.Quit` |
| `ctrl+h` | Toggle `showHelp` |
| `ctrl+r` | Fire reload cmd for active pane |
| `tab` | `focus = (focus + 1) % 4` |
| `shift+tab` | `focus = (focus + 3) % 4` |
| `esc` | If help: dismiss. If query: defocus. Else: clear error |
| `↑`/`↓` | Databases pane: `selectedDB = clamp(...)`. Objects pane: `selectedObj = clamp(...)` |
| `ctrl+1` (objects) | `kind = "table"`, reload objects |
| `ctrl+2` (objects) | `kind = "view"`, reload objects |
| `ctrl+3` (objects) | `kind = "trigger"`, reload objects |
| `enter` (databases) | Reload objects for selected DB |
| `enter` (objects) | Reload DDL for selected object |
| `ctrl+enter` (query) | Execute query, reload results |
| `tea.WindowSizeMsg` | Set `width`, `height` |
| `loadDatabasesMsg` | Populate `databases`. If `preSelectDatabase` set, scan for match. Fire `loadObjectsCmd` |
| `loadObjectsMsg` | Populate `objects` |
| `loadDDLMsg` | Set `ddl` |
| `loadQueryMsg` | Set columns/rows/count/truncated/error |
| `loadClusterMsg` | Populate `clusterNodes` |
| `errMsg` | Set `err` |

**Query textarea focus**: When `focus == dqlitePaneQuery`, forward
alphanumeric, backspace, delete, arrows, home, end to
`m.queryInput.Update(msg)`. Intercept only `tab`/`shift+tab`/
`ctrl+enter`/`esc`/`ctrl+r`.

**Pre-selection logic** (in `loadDatabasesMsg` handler):

```go
if m.preSelectDatabase != "" {
    for i, db := range databases {
        if db.Name == m.preSelectDatabase {
            m.selectedDB = i
            break
        }
    }
}
// Then fire loadObjectsCmd for selected (or index 0) database
```

**Self-clearing error**: Errors from `errMsg` clear after 5 seconds or on
`esc`. Use a goroutine with `time.After` returning a clear-error message.

### 02.5 — View Rendering + Help Overlay

**File**: `cmd/juju/debug/dqlite_model.go` (add `View` method)

**Layout** (reproduced from spec):

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

**Rendering rules**:
- Focused pane: highlighted (thicker/colored) border via lipgloss.
- Databases pane: `>` marks cursor position when navigated with arrows.
- Objects pane: `>` marks cursor. Kind label `[Tables]` updated by
  `ctrl+1/2/3`.
- Query pane: upper sub-pane shows DDL; lower sub-pane shows textarea +
  results table.
- `queryError != ""`: display error in red below textarea.
- `queryTruncated`: display `(truncated)` in yellow below table.
- `err != ""` (banner): red banner at top, self-clearing.
- Cluster pane: tabulated list of nodes.
- Status bar: contextual keybindings.

**Help overlay** (when `showHelp`):

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

**Implementation approach**:

1. Build each pane as a separate `lipgloss.Style` render function.
2. Main `View()` method composes them with the correct borders, applying
   focus highlighting to the active pane.
3. Use `m.width`/`m.height` (from `WindowSizeMsg`) to compute pane sizes
   proportionally.
4. The status bar uses `lipgloss.JoinHorizontal`.

### 02.6 — `dbDebugCommand` + Registration

**File**: `cmd/juju/debug/dbdebug.go`

**Struct**:

```go
type dbDebugCommand struct {
    modelcmd.ControllerCommandBase
    database string
    limit    int
}
```

**Constructor**:

```go
func newDbDebugCommand() *dbDebugCommand {
    return &dbDebugCommand{limit: 100}
}
```

**Info**:

```go
func (c *dbDebugCommand) Info() *cmd.Info {
    return jujucmd.Info(&cmd.Info{
        Name:    "db-debug",
        Purpose: "launch an interactive Dqlite database browser",
        Doc:     "Launches a terminal UI for browsing and querying Juju Dqlite databases.",
    })
}
```

**SetFlags**:

```go
func (c *dbDebugCommand) SetFlags(f *gnuflag.FlagSet) {
    c.ControllerCommandBase.SetFlags(f)
    f.StringVar(&c.database, "database", "", "Pre-select target database (controller or model name)")
    f.IntVar(&c.limit, "limit", 100, "Default query row limit (1-1000)")
}
```

**Init**:

```go
func (c *dbDebugCommand) Init(args []string) error {
    if c.limit < 1 || c.limit > 1000 {
        return errors.Errorf("--limit must be between 1 and 1000")
    }
    return cmd.CheckEmpty(args)
}
```

**Run** (needs 01.3 for `common.OpenDqlite`):

```go
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

    p := tea.NewProgram(model, tea.WithAltScreen())
    _, err = p.Run()
    return errors.Trace(err)
}
```

**Registration** — in `cmd/juju/commands/main.go`, in `registerCommands()`,
add near the existing debug commands (after line 373, the
`newDebugLogCommand` registration):

```go
r.Register(newDbDebugCommand())
```

Place it in the "Error resolution and debugging commands" block (line 368
comment).

### 02.7 — Tests

**File**: `cmd/juju/debug/dbdebug_test.go`

**mockDqliteAPI**:

```go
type mockDqliteAPI struct {
    databasesFn func(ctx context.Context) ([]common.DqliteDatabase, error)
    objectsFn   func(ctx context.Context, ns, kind string) ([]common.DqliteObject, error)
    ddlFn       func(ctx context.Context, ns, name string) (string, error)
    queryFn     func(ctx context.Context, ns, sql string, limit int) (*common.DqliteQueryResult, error)
    clusterFn   func(ctx context.Context) ([]common.DqliteNode, error)

    // Optional: record calls for assertions
    databaseCalls int
    objectsCalls  int
    // ...
}
```

**Test cases** (no real terminal — inject key messages via
`m.Update(tea.KeyMsg{...})`):

- Command registration — `juju help` lists `db-debug`.
- `--limit` validation — rejects 0 and 1001, accepts 1 and 1000.
- Non-TTY rejection — error message when `isatty.IsTerminal` returns false.
- `Init()` returns `tea.Batch(tea.EnterAltScreen, loadDatabasesCmd)`.
- `loadDatabasesMsg` populates `databases`, clears loading.
- `loadDatabasesMsg` with `preSelectDatabase` selects matching DB, fires
  `loadObjectsCmd`.
- `loadObjectsMsg` populates objects.
- `loadDDLMsg` sets `ddl`.
- `loadQueryMsg` sets columns/rows/count/truncated.
- `loadClusterMsg` populates cluster nodes.
- `errMsg` sets error banner.
- Tab cycles through 4 panes.
- `shift+tab` cycles backward.
- `ctrl+1/2/3` changes `kind` in objects pane.
- `↑`/`↓` moves cursor in databases/objects panes.
- `enter` in databases pane fires `loadObjectsCmd`.
- `enter` in objects pane fires `loadDDLCmd`.
- `ctrl+enter` in query pane fires `loadQueryCmd`.
- `ctrl+h` toggles help.
- `ctrl+c` sets `quitting`, returns `tea.Quit`.
- Query textarea captures alphanumeric keys when focused.

Run: `go test ./cmd/juju/debug/...`

## Early Demo Milestone

After completing 02.1–02.6 (tests with mock), but before the real
`dbDebugCommand` wiring works against a live controller, a standalone
demo binary can be created:

**Demo file** (optional, `cmd/juju/debug/dbdebug_demo_main.go` or
integration test):

```go
func main() {
    mock := &mockDqliteAPI{
        databasesFn: func(ctx context.Context) ([]common.DqliteDatabase, error) {
            return []common.DqliteDatabase{
                {Name: "controller", UUID: "", Namespace: "controller", Type: "controller"},
                {Name: "model-foo", UUID: "abc123", Namespace: "model-abc123", Type: "model"},
            }, nil
        },
        objectsFn:   func(...) ([]common.DqliteObject, error) { /* sample tables */ },
        ddlFn:       func(...) (string, error) { return "CREATE TABLE ...", nil },
        queryFn:     func(...) (*common.DqliteQueryResult, error) { /* sample rows */ },
        clusterFn:   func(...) ([]common.DqliteNode, error) { /* sample nodes */ },
    }

    m := NewDqliteModel(mock)
    p := tea.NewProgram(m, tea.WithAltScreen())
    if _, err := p.Run(); err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
}
```

Build and run: `go run ./cmd/juju/debug/dbdebug_demo_main.go`

## Acceptance Criteria

- `juju db-debug` launches interactive TUI in a terminal.
- Exits with error when stdout is not a TTY.
- Databases pane lists controller + model databases.
- Objects pane lists tables/views/triggers for selected DB.
- `ctrl+1/2/3` cycles object kind.
- DDL displays for selected object.
- Read-only queries execute and display results.
- Mutation statements return error in TUI.
- Query results show `(truncated)` when limit hit.
- Cluster pane shows node ID, address, role.
- Query textarea captures alphanumeric input when focused.
- Plain keys (`q`, `s`, `r`, `p`, digits) do not trigger actions.
- `go build ./cmd/juju/...` succeeds.
- `go test -race ./cmd/juju/debug/...` passes.
- TUI tests pass with mock API (no controller required).

## Memory File

Write `specs/debug-db/memory/phase-02.md` updating the pre-implementation
notes.

## Deviations from Spec Phase-02

| # | Deviation | Reason |
|---|----------|--------|
| D1 | TUI starts with local types, aligns to `api/common` in 02.1b | Enables parallel development before 01.1 |
| D2 | `View()` rendering broken into per-pane functions | Cleaner composition, easier testing |
| D3 | Error banner auto-clearing via goroutine + `time.After` (vs spec's 5-second magic) | Explicit implementation detail |

## Parallel Work Opportunities within Phase 02

```
Day 1 (Dev B, in parallel with Phase 00):  02.1 + 02.2 + 02.3
Day 2 (Dev B):                              02.4 + 02.5
Day 3 (Dev B + Dev A delivers 01.1):        02.1b + 02.7 (mock tests)
Day 4 (Both devs, after 01.3 complete):     02.6 + integration test
```
