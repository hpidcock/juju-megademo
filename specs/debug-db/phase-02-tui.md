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
- `cmd/juju/debug/changestream.go` — per-pane header bar pattern,
  viewport cursor tracking, active border color switching.
- `cmd/modelcmd/base.go` — `ControllerCommandBase` and `Wrap` pattern.
- `github.com/charmbracelet/bubbles/textarea` — multiline text input.
- `github.com/charmbracelet/bubbles/viewport` — scrollable list.
- `github.com/charmbracelet/lipgloss` — styling, borders, layout.
- `github.com/charmbracelet/bubbletea` — `Model`, `Cmd`, `KeyMsg`,
  `WindowSizeMsg`, `tea.EnterAltScreen`/`tea.Quit`.

## TUI Layout

The terminal is divided into a context bar, a 2-column main area, and a
compact cluster bar:

```
┌──────────────────────────────────────────────────────────────────┐
│ Controller: mycontroller  [Tab] focus [^H] help [^C] quit        │
├─────────────┬────────────────────────────────────────────────────┤
│ Databases   │ DDL / Query                                       │
│  ▸controller│ CREATE TABLE change_log (                          │
│   lxd-pilot │   id INTEGER PRIMARY KEY,                          │
│   model-bar │   edit_type_id INTEGER NOT NULL, …                 │
│             ├────────────────────────────────────────────────────┤
│             │ SELECT * FROM change_log                  [^ENTER]│
│ Objects     ├────────────────────────────────────────────────────┤
│ [Tables]    │ id │ edit_type_id │ ns_id │ …                      │
│  ▸change_log│ 1  │ 1            │ 1     │ …                      │
│   model     │ 3 rows                    (truncated)              │
│   v_status  │                                                    │
├─────────────┴────────────────────────────────────────────────────┤
│ Cluster  00ab… 10.0.0.1:12345 voter │ 00cd… 10.0.0.2:12345 stand │
└──────────────────────────────────────────────────────────────────┘
```

### Context Bar (top, 1 row)

A persistent bar at the very top showing context and global shortcuts,
following the `debugModel.viewContextBar()` pattern from `debug-tui`:

- Left: `Controller: <name>` (label gray-252, value bold green-86).
- Right: `[Tab] focus  [^H] help  [^C] quit` (bold yellow-228).
- Background: dark gray-235, full terminal width.

### Left Column (~30% width)

Stacks two panes vertically:

**Databases pane** (top, ~40% of left column):

- Header bar: title "Databases" left-aligned, rendered with bold
  white-on-purple (color 15/62) matching `changestreamModel`.
- Scrollable list via `bubbles/viewport` with cursor tracking
  (`syncViewportToCursor` pattern from `changestream.go`).
- Each row: `▸` marks the currently selected database, cursor row
  highlighted with reverse video.
- Active border: green (86). Inactive: gray (62).

**Objects pane** (bottom, ~60% of left column):

- Header bar: title "Objects" left, kind label `[Tables]`/`[Views]`/
  `[Triggers]` center, shortcuts `[^1/^2/^3] kind` right.
- Scrollable list via `bubbles/viewport` with cursor tracking.
- Each row: `▸` marks the cursor position.
- `ctrl+1/2/3` cycles the object kind and reloads.
- Active border: green (86). Inactive: gray (62).

### Right Column (~70% width)

**Detail pane** — single bordered pane with three internal sub-regions:

- **DDL sub-region** (top, ~35% of detail pane): `bubbles/viewport`
  showing the CREATE statement for the selected object.
- **Separator**: horizontal rule (`─` repeated).
- **Query sub-region** (middle, fixed ~3 rows): `textarea.Model` for
  SQL input. Header bar: title "DDL / Query" left, `[^ENTER]` right.
- **Separator**: horizontal rule.
- **Results sub-region** (bottom, ~55% of detail pane): `bubbles/viewport`
  showing the results table. When `queryError != ""`, show the error
  in red instead of the table. When `queryTruncated`, show
  `(truncated)` in yellow below the table.

Active border: green (86). Inactive: gray (62).

### Cluster Bar (bottom, compact)

A compact 1–2 row bar below the main area, not a full bordered pane:

- Top border line only (thin, gray-62).
- Horizontally laid out node entries: abbreviated ID, address, role,
  separated by ` │ `.
- No focus zone — auto-refreshes on `ctrl+r` when databases or objects
  pane is active.

## Focus Zones

Three focus zones (not four — cluster is never focused):

| Zone | Activates | Key routing |
|------|-----------|-------------|
| Databases | `Tab`/`Shift+Tab` cycles here | `↑`/`↓` move cursor, `enter` reloads objects |
| Objects | `Tab`/`Shift+Tab` cycles here | `↑`/`↓` move cursor, `ctrl+1/2/3` cycle kind, `enter` reloads DDL |
| Detail | `Tab`/`Shift+Tab` cycles here | `↑`/`↓` scroll DDL or results viewport, `ctrl+enter` runs query |

## Keybindings

| Key | Context | Action |
|-----|---------|--------|
| `tab` | Global | Cycle focus: databases → objects → detail |
| `shift+tab` | Global | Cycle focus: detail → objects → databases |
| `ctrl+c` | Global | Quit |
| `ctrl+h` | Global | Toggle help overlay |
| `ctrl+r` | Global | Refresh active pane (or cluster if databases/objects) |
| `esc` | Global | Dismiss help / blur query textarea / clear error |
| `↑`/`↓` | Databases | Move cursor, sync viewport |
| `enter` | Databases | Reload objects for selected DB |
| `↑`/`↓` | Objects | Move cursor, sync viewport |
| `enter` | Objects | Reload DDL for selected object |
| `ctrl+1` | Objects | Set kind = "table", reload |
| `ctrl+2` | Objects | Set kind = "view", reload |
| `ctrl+3` | Objects | Set kind = "trigger", reload |
| `ctrl+enter` | Detail | Execute query |
| `↑`/`↓` | Detail (DDL/results focused) | Scroll active viewport |

When the detail pane is focused and the query textarea has focus,
alphanumeric keys are forwarded to `textarea.Update(msg)`. Only
`tab`/`shift+tab`/`ctrl+enter`/`esc`/`ctrl+r` are intercepted.

## TUI Architecture

### Framework: charmbracelet/bubbletea

Same stack as `debug-tui`:

| Package | Purpose |
|---------|---------|
| `github.com/charmbracelet/bubbletea` | Core TUI framework (Model/Update/View) |
| `github.com/charmbracelet/lipgloss` | Terminal styling, layout, borders |
| `github.com/charmbracelet/bubbles` | Pre-built components (viewport, textarea) |

### Model composition

The top-level `dqliteModel` composes four sub-models following the
`debugModel` pattern from `debug-tui`:

```go
type dqliteModel struct {
    width, height int
    focus         dqliteFocus
    showHelp      bool
    quitting      bool
    err           string

    preSelectDatabase string
    defaultLimit      int

    dbList    dqliteDBListModel
    objList   dqliteObjListModel
    detail    dqliteDetailModel
    cluster   dqliteClusterModel

    api DqliteAPI
}

type dqliteFocus int

const (
    dbFocusDatabases dqliteFocus = iota
    dbFocusObjects
    dbFocusDetail
    numDBFocusZones
)
```

Each sub-model implements its own `Update()` and `View()` methods.
The top-level model routes key messages to the focused sub-model and
propagates `WindowSizeMsg` to all sub-models with computed dimensions.

### Sub-models

**`dqliteDBListModel`** — `cmd/juju/debug/dqlite_databases.go`

- Fields: `width`, `height`, `active`, `databases`, `cursor`,
  `viewport`, `ready`.
- `Update()`:
  - `↑`/`↓` → move cursor, `syncViewportToCursor()`.
  - `enter` → return `loadObjectsCmd`.
  - `loadDatabasesMsg` → populate `databases`, set content, sync
    viewport.
- `View()`:
  - Header bar: bold white-on-purple "Databases".
  - Scrollable viewport content with `▸` current marker, reverse-video
    cursor highlight.
  - Active border: green (86). Inactive: gray (62).

**`dqliteObjListModel`** — `cmd/juju/debug/dqlite_objects.go`

- Fields: `width`, `height`, `active`, `kind`, `objects`, `cursor`,
  `viewport`, `ready`, `namespace`.
- `Update()`:
  - `↑`/`↓` → move cursor, `syncViewportToCursor()`.
  - `ctrl+1/2/3` → change `kind`, return `loadObjectsCmd`.
  - `enter` → return `loadDDLCmd`.
  - `loadObjectsMsg` → populate `objects`, set content, sync viewport.
- `View()`:
  - Header bar: bold white-on-purple title, kind label `[Tables]`,
    shortcuts `[^1/^2/^3]`.
  - Scrollable viewport with `▸` cursor marker, reverse-video
    highlight.
  - Active border: green (86). Inactive: gray (62).

**`dqliteDetailModel`** — `cmd/juju/debug/dqlite_detail.go`

- Fields: `width`, `height`, `active`, `ddl`, `queryInput`,
  `queryColumns`, `queryRows`, `queryCount`, `queryTruncated`,
  `queryError`, `ddlViewport`, `resultsViewport`, `ready`.
- `Update()`:
  - `ctrl+enter` → return `loadQueryCmd`.
  - `↑`/`↓` → scroll DDL or results viewport depending on sub-focus.
  - `loadDDLMsg` → populate `ddl`, set DDL viewport content.
  - `loadQueryMsg` → populate result fields, set results viewport
    content.
- `View()`:
  - Header bar: bold white-on-purple "DDL / Query" left,
    `[^ENTER] run` right.
  - DDL viewport (top sub-region).
  - Separator line.
  - Query textarea.
  - Separator line.
  - Results viewport or error message.
  - Active border: green (86). Inactive: gray (62).

**`dqliteClusterModel`** — `cmd/juju/debug/dqlite_cluster.go`

- Fields: `width`, `nodes`, `ready`.
- No `Update()` — state set directly by top-level model on
  `loadClusterMsg`.
- `View()`:
  - Top border line (thin, gray-62).
  - Compact horizontal layout: abbreviated node ID, address, role,
  separated by ` │ `.

### Message types

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
goroutine and returns the corresponding message.

### Viewport cursor tracking

Both `dqliteDBListModel` and `dqliteObjListModel` implement the
`syncViewportToCursor` pattern from `changestream.go`:

```go
func (m *dqliteDBListModel) syncViewportToCursor() {
    if !m.ready || m.viewport.Height == 0 {
        return
    }
    visibleStart := m.viewport.YOffset
    visibleEnd := visibleStart + m.viewport.Height
    if m.cursor < visibleStart {
        m.viewport.SetYOffset(m.cursor)
    } else if m.cursor >= visibleEnd {
        m.viewport.SetYOffset(m.cursor - m.viewport.Height + 1)
    }
}
```

### Per-pane header bar pattern

All sub-models use the same header bar styling as `changestreamModel`:

```go
headerStyle := lipgloss.NewStyle().
    Bold(true).
    Foreground(lipgloss.Color("15")).
    Background(lipgloss.Color("62")).
    Padding(0, 1)

shortcutStyle := lipgloss.NewStyle().
    Bold(true).
    Foreground(lipgloss.Color("228"))
```

### Layout math (on `WindowSizeMsg`)

```
totalHeight   = height
contextBar    = 1
clusterBar    = 3  (top-border + content + small pad)
mainArea      = totalHeight - contextBar - clusterBar

leftColWidth  = width * 30 / 100
rightColWidth = width - leftColWidth

dbListHeight  = mainArea * 40 / 100
objListHeight = mainArea - dbListHeight

detailHeight     = mainArea
ddlHeight        = detailHeight * 35 / 100
queryEditorHeight = 3  (textarea + separator)
resultsHeight     = detailHeight - ddlHeight - queryEditorHeight
```

### Help overlay

When `showHelp` is true, render a centered bordered overlay using
`lipgloss.Place`:

```
  Tab          Switch pane                Ctrl+1..3  Object kind
  Shift+Tab    Previous pane              Ctrl+H     This help
  ↑/↓          Navigate / scroll          Ctrl+R     Refresh pane
  Enter        Select database / object   Esc        Dismiss
  Ctrl+Enter   Execute query             Ctrl+C     Quit
```

Matches the `helpModel` pattern from `debug-tui/help.go`.

## Scope

### 1. `DqliteAPI` interface — `cmd/juju/debug/dqlite_api.go`

Unchanged from previous implementation. See section 1 of the original
spec.

### 2. Sub-model files

| File | Sub-model |
|------|-----------|
| `cmd/juju/debug/dqlite_databases.go` | `dqliteDBListModel` |
| `cmd/juju/debug/dqlite_objects.go` | `dqliteObjListModel` |
| `cmd/juju/debug/dqlite_detail.go` | `dqliteDetailModel` |
| `cmd/juju/debug/dqlite_cluster.go` | `dqliteClusterModel` |

### 3. `dqliteModel` — `cmd/juju/debug/dqlite_model.go`

Thin compositor. Routes keys, computes layout, renders context bar and
help overlay.

### 4. `load*` messages — `cmd/juju/debug/dqlite_messages.go`

Unchanged.

### 5. `dbDebugCommand` — `cmd/juju/debug/dbdebug.go`

Unchanged.

### 6. Registration

Unchanged.

### 7. Tests

See `cmd/juju/debug/dbdebug_test.go`. Tests operate on the top-level
`dqliteModel` via `Update()` — internal sub-model decomposition is
transparent to tests.

## Memory File

On completion, write `specs/debug-db/memory/phase-02.md`:

- All file paths created/modified.
- The sub-model decomposition as implemented.
- The layout math (proportions, constants).
- The `registerCommands` location.
- The `DqliteClient` wiring path.
- Any deviations from this phase spec and the reason.

## Acceptance Criteria

- `juju db-debug` launches an interactive TUI when run in a terminal.
- `juju db-debug` prints an error and exits when stdout is not a TTY.
- The context bar shows the controller name and global shortcuts.
- The left column shows databases (top) and objects (bottom) with
  scrollable viewports.
- The right column shows DDL (top), query textarea (middle), and
  results (bottom) with scrollable viewports.
- The cluster bar shows compact node information.
- `ctrl+1`/`ctrl+2`/`ctrl+3` cycle the object kind.
- DDL displays for a selected object.
- Read-only SQL queries execute and display results as a table.
- Mutation statements return an error displayed in the TUI.
- Query results show `(truncated)` indicator when the row limit is hit.
- The query textarea captures all alphanumeric input when focused.
- Plain keys (`q`, `s`, `r`, `p`, digits) do not trigger actions when
  the detail pane is focused.
- `Tab`/`Shift+Tab` cycle the 3 focus zones.
- Active pane has green border, inactive panes have gray border.
- `ctrl+h` toggles the help overlay.
- `ctrl+r` refreshes the active pane.
- `go build ./cmd/juju/...` succeeds.
- `go test ./cmd/juju/debug/...` passes.
- TUI tests pass with mock API (no real controller required).
