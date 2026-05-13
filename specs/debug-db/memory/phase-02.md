# Phase 02 — Memory (pre-implementation notes)

## States From This Specification

All decisions below were made during spec review before any code was
written. They must be carried forward into implementation.

## File Paths

- `cmd/juju/debug/dqlite_api.go` — `DqliteAPI` interface + adapter.
- `cmd/juju/debug/dqlite_model.go` — `dqliteModel` bubbletea model.
- `cmd/juju/debug/dqlite_messages.go` — `load*Msg` types + cmds.
- `cmd/juju/debug/dbdebug.go` — `dbDebugCommand`.
- `cmd/juju/debug/dbdebug_test.go` — tests.

## Command Registration

`registerCommands()` in `cmd/juju/commands/main.go`:

```go
r.Register(newDbDebugCommand())
```

Record exact line number where this call is inserted.

## Command Name

`db-debug` — appears in `juju help` under this name.

## DqliteAPI Interface

```go
type DqliteAPI interface {
    Databases(ctx context.Context) ([]common.DqliteDatabase, error)
    Objects(ctx context.Context, ns, kind string) ([]common.DqliteObject, error)
    DDL(ctx context.Context, ns, name string) (string, error)
    Query(ctx context.Context, ns, sql string, limit int) (*common.DqliteQueryResult, error)
    Cluster(ctx context.Context) ([]common.DqliteNode, error)
}
```

Concrete adapter `dqliteAPIImpl` wraps `*common.DqliteClient`.
Constructor: `func NewDqliteAPI(client *common.DqliteClient) DqliteAPI`.

## dqliteModel — Key Fields

```go
type dqliteModel struct {
    width, height int
    focus         dqlitePane
    showHelp      bool
    quitting      bool
    err           string

    preSelectDatabase string   // set from --database flag
    defaultLimit      int      // set from --limit flag (default 100)

    databases  []common.DqliteDatabase
    selectedDB int

    kind        string // "table", "view", "trigger"
    objects     []common.DqliteObject
    selectedObj int

    ddl string

    queryInput     textarea.Model
    queryColumns   []string
    queryRows      [][]string
    queryCount     int
    queryTruncated bool
    queryError     string

    clusterNodes []common.DqliteNode

    api DqliteAPI
}
```

## Panes

```
dqlitePaneDatabases
dqlitePaneObjects
dqlitePaneQuery
dqlitePaneCluster
```

Tab cycles `(focus + 1) % 4`, shift+tab cycles `(focus + 3) % 4`.

## Key Bindings

| Key          | Action                              |
|-------------|-------------------------------------|
| Tab         | Next pane                           |
| Shift+Tab   | Previous pane                       |
| Ctrl+1/2/3  | Object kind (objects pane)          |
| Enter       | Select DB / select object           |
| Ctrl+Enter  | Execute query (query pane)          |
| Ctrl+H      | Toggle help overlay                 |
| Ctrl+R      | Reload active pane                  |
| Esc         | Dismiss help / defocus query / clr err |
| Ctrl+C      | Quit                                |
| ↑/↓         | Navigate list (databases/objects)   |

## Pre-Selection Logic

When `loadDatabasesMsg` is handled:
1. Populate `m.databases`.
2. If `m.preSelectDatabase != ""`, scan `m.databases` for a matching
   `Name`, set `m.selectedDB` to its index.
3. Fire `loadObjectsCmd` for the selected (or first) database.

## Wiring Path

```
dbDebugCommand.Run
  → cmd.NewAPIRoot() -> conn
  → common.OpenDqlite(ctx, conn) -> *DqliteClient
  → debug.NewDqliteAPI(client) -> DqliteAPI
  → debug.NewDqliteModel(api) -> *dqliteModel
  → tea.NewProgram(model).Run()
```

## load* Messages (in dqlite_messages.go)

```go
type loadDatabasesMsg struct { databases []common.DqliteDatabase; err error }
type loadObjectsMsg   struct { objects   []common.DqliteObject;   err error }
type loadDDLMsg       struct { ddl string;                       err error }
type loadQueryMsg     struct { result *common.DqliteQueryResult;  err error }
type loadClusterMsg   struct { nodes []common.DqliteNode;        err error }
type errMsg           struct { err error }
```

Each `load*Cmd` calls `api.<Method>(context.Background())` and returns
the corresponding message. `errMsg` is used for unexpected errors.

## CLI Flags

```
--database string   Pre-select target database by name
--limit int         Default query row limit (1-1000, default 100)
```

## Deviations From Phase Spec

None — these notes reflect the spec as written after review.
