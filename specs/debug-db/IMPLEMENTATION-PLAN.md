# Debug-DB Implementation Plan

Each task is self-contained and delegable to an independent agent.
Tasks are ordered to produce a **runnable demo as early as possible**.

## Progress

| Task | Status | Commit |
|------|--------|--------|
| 1 — TUI model with hardcoded mock data | ✅ Complete | `d4e6d19` |
| 2 — `dbDebugCommand` + registration | ✅ Complete | `06dd39c` (initial), `6aeead5` (test fixes) |
| 3 — TUI model tests | ✅ Complete | Pending commit |
| 4 — Client package `api/common/dqlite.go` | ✅ Complete | `b60c6d1` (initial), `1b09706` (review fixes) |
| 5 — Backend handler `apiserver/dqlite.go` | ✅ Complete | `f21faac1f6` (initial), `34231097` (review fixes) |
| 6 — Wire real client | ✅ Complete | `cc6e0cdb3b` |
| 7 — Memory files | ⬜ Not started | — |
| 8 — TUI layout refinement | ✅ Complete | `480d56b5f4` |
| 9 — TUI visual polish and UX fixes | ⬜ Not started | — |

## Deviations from spec

- **`OpenDqlite` parameter type** (Task 4): The Phase 01 spec uses
  `base.StreamConnector` but calls `ConnectControllerStream`, which only exists
  on `base.ControllerStreamConnector`. The implementation uses
  `base.ControllerStreamConnector` as the parameter type, which is the correct
  interface for controller-scoped endpoints like `/dqlite`.
- **ctrl+enter / ctrl+1/2/3 keybind tests** (Task 3): Omitted because bubbletea v1.3.10
  does not expose `KeyCtrlEnter`, `KeyCtrl1`, `KeyCtrl2`, `KeyCtrl3` as `KeyType`
  constants. These key combinations cannot be constructed as `tea.KeyMsg` in tests.
  The underlying code paths (`reloadObjects`, `loadQueryCmd`) are covered via
  `TestReloadObjectsUsesKind` and `TestLoadQueryCmdCallsAPI`.
- **`WrapControllerSkipControllerFlags`** (Task 2): The `dbDebugCommand` used
  `modelcmd.WrapController(c, modelcmd.WrapControllerSkipControllerFlags)` to
  avoid requiring a live controller for the mock-based demo. Removed in Task 6
  when real API wiring was added.
- **Local mirror types** (Task 1): `DqliteDatabase`, `DqliteObject`, `DqliteNode`,
  `DqliteQueryResult` were defined in `cmd/juju/debug/dqlite_api.go` (not imported
  from `api/common`). Replaced with `api/common` imports in Task 6.
- **Extra `authenticator` field** (Task 5): The `dqliteHandler` struct stores an
  `authentication.HTTPAuthenticator` field alongside the spec's 3-field struct
  (`ctxt`, `dbGetter`, `authorizer`). This follows the `debugLogHandler` pattern
  where the authenticator is stored directly on the handler rather than accessed
  through `httpContext`. Functionally equivalent.
- **`dqliteDBGetter` vs `dbGetter`** (Task 5): The route registration uses
  `srv.shared.dqliteDBGetter` (a new `DBGetter` field on `sharedServerContext`)
  instead of the spec's `srv.shared.dbGetter`. The existing `dbGetter` is
  `changestream.WatchableDBGetter`, not `func(namespace string) (database.TxnRunner, error)`.
  An adapter (`adaptWatchableDBGetter`) bridges the types.
- **`clusterNodeInfo.ID` type** (Task 5): Uses `uint64` to match
  `dbreplaccessor.Node.ID` for correct type assertion at runtime.
- **`unauthenticated: true`** (Task 5): The `/dqlite` route uses
  `unauthenticated: true` to skip middleware-level auth and rely solely on the
  handler's own authentication inside the websocket connection (matching the
  `debuglog` pattern).

---

## Task 1 — TUI model with hardcoded mock data (visual demo) ✅ ✅

**Goal**: Build `dqliteModel` that renders the full 4-pane layout with
hardcoded data. No API calls, no `DqliteAPI` interface yet — just a
working bubbletea program you can run and interact with.

**Demo after this task**: `go run` the model directly and see the
databases list, objects list, DDL viewer, query textarea, cluster
table, keybindings all working with fake data.

**Files to create**:

| File | Purpose |
|------|---------|
| `cmd/juju/debug/dqlite_model.go` | `dqliteModel` struct, `NewDqliteModel(api DqliteAPI)`, `Init()`, `Update()`, `View()` |
| `cmd/juju/debug/dqlite_messages.go` | All `load*Msg` types + `errMsg` + `load*Cmd` factory functions |
| `cmd/juju/debug/dqlite_api.go` | `DqliteAPI` interface (5 methods) + `dqliteAPIImpl` adapter (can be empty for now) |

**Spec reference**: `specs/debug-db/phase-02-tui.md` sections 1–3

**Implementation details**:

1. **`cmd/juju/debug/dqlite_api.go`** — Define the interface first so the
   model compiles against it:

   ```go
   type DqliteAPI interface {
       Databases(ctx context.Context) ([]common.DqliteDatabase, error)
       Objects(ctx context.Context, ns, kind string) ([]common.DqliteObject, error)
       DDL(ctx context.Context, ns, name string) (string, error)
       Query(ctx context.Context, ns, sql string, limit int) (*common.DqliteQueryResult, error)
       Cluster(ctx context.Context) ([]common.DqliteNode, error)
   }
   ```

   Also define the concrete adapter `dqliteAPIImpl` + `NewDqliteAPI` (defer
   method bodies with `panic("not implemented")` — the real wiring is
   Task 7).

   Import `github.com/juju/juju/api/common` for the exported types
   (`DqliteDatabase`, `DqliteObject`, `DqliteNode`, `DqliteQueryResult`).
   These types don't exist yet (Phase 01) — for now, define local mirror
   types in this same file with a `// TODO: replace with common.Dqlite* when Phase 01 lands` comment.

2. **`cmd/juju/debug/dqlite_messages.go`** — All message types and cmd
   factories per spec section 3. Each `load*Cmd` takes `api DqliteAPI` +
   relevant args and returns a `tea.Cmd` that calls the API in a
   goroutine.

3. **`cmd/juju/debug/dqlite_model.go`** — Full model per spec section 2:

   - Struct fields: `width`, `height`, `focus`, `showHelp`, `quitting`,
     `err`, `preSelectDatabase`, `defaultLimit`, `databases`,
     `selectedDB`, `kind`, `objects`, `selectedObj`, `ddl`,
     `queryInput` (textarea.Model), `queryColumns`, `queryRows`,
     `queryCount`, `queryTruncated`, `queryError`, `clusterNodes`, `api`.
   - `NewDqliteModel(api DqliteAPI) *dqliteModel` — initialize with
     defaults (`focus: dqlitePaneDatabases`, `kind: "table"`,
     `defaultLimit: 100`), configure textarea.
   - `Init()` — return `tea.Batch(tea.EnterAltScreen, loadDatabasesCmd)`.
     For this task only: `loadDatabasesCmd` returns a hardcoded
     `loadDatabasesMsg` instead of calling the API (so we get an instant
     visual demo without a controller).
   - `Update()` — implement the full key-routing table from spec section 2.
     When the query pane is focused, forward alphanumeric keys to
     `queryInput.Update(msg)`.
   - `View()` — render the 4-pane layout from spec section 2 using
     lipgloss borders, the cluster table, and the status bar. Focused
     pane gets a highlighted border.

   Populate hardcoded mock data in `Init` (or via the hardcoded
   `loadDatabasesCmd`):
   - `databases`: `[{Name:"controller", Namespace:"controller", Type:"controller"}, {Name:"lxd-pilot", UUID:"deadbeef-…", Namespace:"deadbeef-…", Type:"model"}]`
   - `objects`: `[{Name:"change_log", Kind:"table"}, {Name:"model", Kind:"table"}, {Name:"v_model_status", Kind:"view"}]`
   - `ddl`: `CREATE TABLE change_log (id INTEGER PRIMARY KEY, edit_type_id INTEGER NOT NULL, …)`
   - `clusterNodes`: `[{ID:"00ab…", Address:"10.0.0.1:12345", Role:"voter"}, {ID:"00cd…", Address:"10.0.0.2:12345", Role:"stand-by"}]`
   - `queryColumns`: `["id", "edit_type_id", "ns_id"]`
   - `queryRows`: `[["1","1","1"], ["2","2","2"], ["3","3","3"]]`
   - `queryCount`: 3, `queryTruncated`: true

**Dependencies**: bubbletea, bubbles, lipgloss (already in go.mod from
`specs/debug-tui` Phase 00 — verify; if missing, `go get` them).

**Research files to read first**:
- `cmd/juju/commands/debuglog.go:209-260` — command struct pattern
- `cmd/modelcmd/base.go` — `ControllerCommandBase` embedding
- `specs/debug-tui/phase-00-working-tui.md:46-78` — sub-model composition pattern

**Verification**: `go build ./cmd/juju/debug/...` compiles. Optionally add a
`func main()` in a `_test.go` or standalone file to `tea.NewProgram` the
model and visually verify the layout.

---

## Task 2 — `dbDebugCommand` + registration (CLI demo) ✅

**Goal**: Wire the TUI into a real `juju db-debug` command so it can be
launched from the CLI. Still uses mock data (no real API connection).

**Demo after this task**: `juju db-debug` launches the TUI in a terminal,
rejects non-TTY, accepts `--database` and `--limit` flags.

**Files to create/modify**:

| File | Action |
|------|--------|
| `cmd/juju/debug/dbdebug.go` | Create — `dbDebugCommand` struct + methods |
| `cmd/juju/commands/main.go:556` | Add `r.Register(debug.NewDbDebugCommand())` (after last `r.Register` before closing `}`) |

**Spec reference**: `specs/debug-db/phase-02-tui.md` sections 4–5

**Implementation details**:

1. **`cmd/juju/debug/dbdebug.go`**:
   ```go
   type dbDebugCommand struct {
       modelcmd.ControllerCommandBase
       database string
       limit    int
   }
   ```
   - `newDbDebugCommand()` — default `limit: 100`
   - `Info()` — name `"db-debug"`, purpose, doc
   - `SetFlags()` — `--database`, `--limit` (1–1000)
   - `Init()` — validate `--limit`, `cmd.CheckEmpty(args)`
   - `Run()`:
     - TTY check: reuse pattern from
       `cmd/juju/commands/debuglog.go:429-435` (`isatty.IsTerminal`)
     - For now, skip the real API connection. Create a mock `DqliteAPI`
       (a struct with hard-coded return values) and pass it to
       `NewDqliteModel`.
     - Set `model.preSelectDatabase = c.database`,
       `model.defaultLimit = c.limit`
     - `tea.NewProgram(model, tea.WithAltScreen()).Run()`

2. **`cmd/juju/commands/main.go`**: Add import for
   `"github.com/juju/juju/cmd/juju/debug"` and register:
   ```go
   r.Register(debug.NewDbDebugCommand())
   ```
   Add this near line 556 (after the last `r.Register(...)` call before the
   closing `}` of `registerCommands`).

**Research files**:
- `cmd/juju/commands/debuglog.go:186-260` — command structure pattern
- `cmd/juju/commands/main.go:332-556` — registration pattern
- `cmd/modelcmd/base.go` — `ControllerCommandBase`

**Verification**: `go build ./cmd/juju/...` succeeds. `juju help | grep db-debug` shows the command.

---

## Task 3 — TUI model tests (solid foundation before real backend) ✅ ✅

**Goal**: Comprehensive unit tests for the `dqliteModel` using a
`mockDqliteAPI`. This ensures the model logic is correct before we wire
the real backend.

**Demo after this task**: `go test ./cmd/juju/debug/...` passes — model
behaviour is verified without a controller.

**Files to create**:

| File | Purpose |
|------|---------|
| `cmd/juju/debug/dbdebug_test.go` | All model + command tests |

**Spec reference**: `specs/debug-db/phase-02-tui.md` section 7

**Test cases to cover** (use `tc` / `c.Assert` / `c.Check` per AGENTS.md):

- Command: `juju help` lists `db-debug`
- `--limit` validation: 0 → error, 1001 → error
- Non-TTY: error message
- `Init()` fires `loadDatabasesCmd`
- `loadDatabasesMsg` populates `databases`, clears loading
- `loadDatabasesMsg` with `preSelectDatabase` selects matching DB + fires `loadObjectsCmd`
- `loadObjectsMsg` populates `objects` for selected DB + kind
- `loadDDLMsg` populates `ddl`
- `loadQueryMsg` populates columns/rows/count/truncated/error
- `loadClusterMsg` populates `clusterNodes`
- `errMsg` sets `err`
- Tab cycling: `focus` rotates through 4 panes
- `ctrl+1/2/3` changes `kind` + fires reload
- `↑`/`↓` moves cursor in databases and objects panes
- `enter` in databases pane fires `loadObjectsCmd`
- `enter` in objects pane fires `loadDDLCmd`
- `ctrl+enter` in query pane fires `loadQueryCmd`
- `ctrl+h` toggles `showHelp`
- `ctrl+c` sets `quitting`, returns `tea.Quit`
- Query textarea captures `q`, `s`, `r`, `p` when focused (no action triggered)

**Mock API**: Define `mockDqliteAPI struct` with configurable return
values and call-recording fields (e.g. `calledWithNamespace string`).

**Verification**: `go test ./cmd/juju/debug/...` passes.

---

## Task 4 — Client package `api/common/dqlite.go` (Phase 01)

**Goal**: Create the reusable WebSocket client that dials `/dqlite`,
performs the `v1` handshake, and exposes typed methods.

**Demo after this task**: `go test -race ./api/common/...` passes —
client is importable and works with a mock stream.

**Files to create**:

| File | Purpose |
|------|---------|
| `api/common/dqlite.go` | `DqliteClient`, `OpenDqlite`, typed methods, request/response structs |
| `api/common/dqlite_test.go` | Mock stream tests |

**Spec reference**: `specs/debug-db/phase-01-client-package.md` sections 1–5

**Implementation details**:

1. **Exported types** (section 2): `DqliteDatabase`, `DqliteObject`,
   `DqliteNode`, `DqliteQueryResult` with JSON tags matching the wire
   protocol from Phase 00.

2. **`DqliteClient`** (sections 1, 3): struct with `conn base.Stream` +
   `mu sync.Mutex`. Methods: `Databases`, `Objects`, `DDL`, `Query`,
   `Cluster`. Each builds a `dqliteRequest`, calls `send`, checks
   `resp.Error` and `resp.Version`, unmarshals `resp.Result`.

3. **`OpenDqlite`** (section 4b): dials
   `src.ConnectControllerStream(ctx, "/dqlite", nil, nil)`, then performs
   version handshake. On failure, closes stream.

4. **Request ID generation** (section 4a): 8 hex chars from
   `crypto/rand`.

5. **`send`** (section 4c): mutex-guarded `WriteJSON` + `ReadJSON`.

6. **Tests** (section 5): `mockStream` with channel-based read/write,
   covering handshake, each method's JSON round-trip, server error,
   version mismatch, read/write errors, concurrent sends under `-race`.

**Research files**:
- `api/common/logs.go:1-80` — `StreamDebugLog` pattern
- `api/apiclient.go` — `ConnectControllerStream` signature
- `api/base/caller.go` — `base.StreamConnector`, `base.Stream`

**Verification**: `go build ./api/...` and `go test -race ./api/common/...` pass.

---

## Task 5 — Backend handler `apiserver/dqlite.go` (Phase 00)

**Goal**: Create the `/dqlite` WebSocket handler on the controller that
dispatches JSON requests against Dqlite databases with read-only
enforcement.

**Demo after this task**: `go test ./apiserver/...` passes — handler
responds correctly to all request types with an in-memory DB.

**Files to create/modify**:

| File | Action |
|------|--------|
| `apiserver/dqlite.go` | Create — handler, dispatch, SQL policy, value formatter |
| `apiserver/dqlite_test.go` | Create — handler tests |
| `apiserver/apiserver.go:1046` | Add `/dqlite` route to handler slice |

**Spec reference**: `specs/debug-db/phase-00-websocket-backend.md` sections 1–9

**Implementation details**:

1. **Handler struct** (section 1): `dqliteHandler` with `ctxt httpContext`,
   `dbGetter DBGetter`, `authorizer authentication.Authorizer`. Local
   `DBGetter` type: `func(namespace string) (database.TxnRunner, error)`.

2. **`ServeHTTP`** (section 1): `websocket.Serve(w, req, handlerFunc)`.
   Inside: authenticate, authorize, read JSON loop, dispatch, write JSON.
   On read error/EOF: return nil.

3. **Version handshake** (section 1): first message must carry
   `version: "v1"`. Server echoes or rejects.

4. **Request dispatch** (section 2): 5 types — `databases`, `objects`,
   `ddl`, `query`, `cluster`. Each dispatches against `dbGetter` using
   `database.StdTxn`.

5. **Read-only SQL policy** (section 3): keyword whitelist + `PRAGMA
   query_only = ON` + multi-statement rejection via `;` split outside
   single-quoted strings.

6. **Row limit** (section 4): default 100, max 1000. Append/replace
   `LIMIT` clause. Set `Truncated = true` if extra rows exist.

7. **Query timeout** (section 5): `context.WithTimeout(ctx, 10*time.Second)`.

8. **Value formatter** (section 6): `formatValue` — nil→NULL, []byte→hex
   (truncated at 256 chars), time.Time→RFC3339Nano, fmt.Stringer→String,
   else `%v`.

9. **Cluster dispatch** (section 2): check if `TxnRunner` implements
   `ClusterIntrospector`; call `DescribeCluster` if so. Define local
   `ClusterIntrospector` interface to avoid importing `dbreplaccessor`.

   Reference: `internal/worker/dbreplaccessor/worker.go:222-241`
   (`DescribeCluster` returns `[]Node` with `ID`, `Address`, `Role`).

10. **Route registration** in `apiserver/apiserver.go`: add before the
    closing `}}` of the handler slice (around line 1046):

    ```go
    {
        pattern:    "/dqlite",
        handler:    srv.monitoredHandler(
            newDqliteHandler(httpCtxt, srv.shared.dbGetter, controllerAdminAuthorizer),
            "dqlite",
        ),
        authorizer: controllerAdminAuthorizer,
        tracked:    true,
        noModelUUID: true,
    },
    ```

    `controllerAdminAuthorizer` is already defined at line 780.
    `srv.shared.dbGetter` is already wired at line 391.

**Research files**:
- `apiserver/debuglog.go:36-67` — WebSocket handler pattern
- `apiserver/debuglog_tailer.go:20-27` — `newDebugLogTailerHandler` constructor
- `apiserver/apiserver.go:776-800` — handler registration area
- `apiserver/apiserver.go:1042-1046` — end of handler slice (insertion point)
- `apiserver/authorizer.go:44-46` — `controllerAdminAuthorizer`
- `internal/worker/dbreplaccessor/worker.go:51-56` — `DBGetter` interface
- `internal/worker/dbreplaccessor/worker.go:222-241` — `DescribeCluster`

**Verification**: `go build ./apiserver/...` and `go test ./apiserver/...` pass.

---

## Task 6 — Wire real client into `dbDebugCommand` (end-to-end demo) ✅

**Goal**: Replace the mock API in `dbDebugCommand.Run` with the real
`common.OpenDqlite` → `NewDqliteAPI` chain. The TUI now talks to a live
controller.

**Demo after this task**: `juju db-debug` against a real controller
browses live Dqlite data — **full end-to-end demo**.

**Files modified**:

| File | Change |
|------|--------|
| `cmd/juju/debug/dqlite_api.go` | Implement `dqliteAPIImpl` method bodies (delegate to `common.DqliteClient`) |
| `cmd/juju/debug/dbdebug.go` | Replace mock API with real client wiring |

**Implementation details**:

1. **`cmd/juju/debug/dqlite_api.go`**: Replace `panic("not implemented")`
   in each method with delegation:
   ```go
   func (a *dqliteAPIImpl) Databases(ctx context.Context) ([]common.DqliteDatabase, error) {
       return a.client.Databases(ctx)
   }
   ```
   Same pattern for all 5 methods.

2. **`cmd/juju/debug/dbdebug.go` `Run()`**:
   ```go
   conn, err := c.NewAPIRoot()
   if err != nil { return errors.Trace(err) }
   defer conn.Close()

   client, err := common.OpenDqlite(ctx, conn)
   if err != nil { return errors.Trace(err) }
   defer client.Close()

   model := NewDqliteModel(NewDqliteAPI(client))
   model.preSelectDatabase = c.database
   model.defaultLimit = c.limit

   p := tea.NewProgram(model, tea.WithAltScreen())
   _, err = p.Run()
   return err
   ```

3. **Remove local mirror types** from Task 1 — replaced with imports from
   `api/common` (now available after Phase 01 landed).

**Research files**:
- `cmd/juju/commands/debuglog.go:421-427` — `NewAPIRoot` usage pattern

**Verification**: `go build ./cmd/juju/...` succeeds. Manual test:
`juju db-debug` against a bootstrapped controller shows real databases,
objects, DDL, query results, cluster info.

---

## Task 7 — Memory files

**Goal**: Document what was built for future phases.

**Files to create**:

| File | Content |
|------|---------|
| `specs/debug-db/memory/phase-00.md` | Per `specs/debug-db/phase-00-websocket-backend.md` Memory File section |
| `specs/debug-db/memory/phase-01.md` | Per `specs/debug-db/phase-01-client-package.md` Memory File section |
| `specs/debug-db/memory/phase-02.md` | Per `specs/debug-db/phase-02-tui.md` Memory File section |

Each memory file must include: file paths created, wiring locations, protocol details, deviations from spec.

---

## Task 8 — TUI layout refinement (2-column + sub-model decomposition) ✅

**Goal**: Replace the original 3-column horizontal layout with a 2-column
stacked layout following the `debug-tui` patterns. Decompose the monolithic
`dqliteModel` into composable sub-models with viewports.

**Demo after this task**: TUI renders in 2-column layout, databases and
objects lists scroll when content exceeds viewport, cluster bar is compact,
active pane has green border, context bar shows controller name.

**Files created/modified**:

| File | Action |
|------|--------|
| `cmd/juju/debug/dqlite_databases.go` | Create — `dqliteDBListModel` with viewport, cursor tracking |
| `cmd/juju/debug/dqlite_objects.go` | Create — `dqliteObjListModel` with viewport, kind cycling |
| `cmd/juju/debug/dqlite_detail.go` | Create — `dqliteDetailModel` with DDL/results viewports, query textarea |
| `cmd/juju/debug/dqlite_cluster.go` | Create — `dqliteClusterModel` compact bar |
| `cmd/juju/debug/dqlite_model.go` | Rewrite — thin compositor, 2-column layout, context bar, key routing |
| `cmd/juju/debug/dbdebug_test.go` | Update — field access paths, focus enum, 3-zone Tab cycling |
| `specs/debug-db/phase-02-tui.md` | Update — new layout spec, sub-model decomposition, focus zones |

**Bugs fixed**:

1. **Navigation broken**: up/down keys were intercepted by the top-level
   model which only updated the cursor integer without re-rendering viewport
   content or syncing scroll. Fix: delegate up/down to focused sub-models
   which call `refreshViewport()` (re-render + sync) on cursor move.

2. **Controller DB duplicated**: backend `dispatchDatabases()` returns
   controller model from `SELECT uuid, name FROM model` AND prepends a
   special controller entry. Fix: `deduplicateDatabases()` filters out
   model-type entries sharing a name with an existing controller-type entry.

**Verification**: `go build ./cmd/juju/...` and `go test ./cmd/juju/debug/...` pass.

---

## Task 9 — TUI visual polish and UX fixes

**Goal**: Fix remaining visual and UX issues in the `juju db-debug` TUI.
This task is intended to be delegated to an independent agent who will
run the TUI against a real controller, identify issues, and fix them.

**Scope**: The agent should run `juju db-debug` against a bootstrapped
controller and iterate on any issues found. The list below is a starting
point — the agent should add items as they discover them.

**Known issues to investigate**:

- **Layout overflow**: Pane borders and content may overflow terminal
  width when the left column (30%) + right column (70%) plus borders
  exceed the actual terminal width. The `lipgloss.JoinHorizontal` call
  in `View()` does not account for border characters consumed by each
  pane.
- **Viewport height calculation**: The `Height()` methods on sub-models
  return a minimum of 4, but the actual rendered height includes the
  header bar and border (3 extra lines). If the computed height is too
  small, the viewport gets 0 or negative lines and renders nothing.
- **Context bar hardcoded "mycontroller"**: The context bar currently
  renders a hardcoded string "mycontroller" instead of the real
  controller name from the API connection. Wire the actual controller
  name through `dbDebugCommand.Run()` into `dqliteModel`.
- **Detail pane separator width**: The `strings.Repeat("─", ...)` in
  `dqliteDetailModel.View()` uses `m.width - 4` which may not match
  the actual interior width after borders are applied.
- **Query textarea height**: The textarea is configured with
  `SetHeight(1)` in `WindowSizeMsg` but this may not render correctly
  inside the bordered pane — verify the textarea is visible and
  usable.
- **Cluster bar width**: The cluster bar uses `m.width` for the top
  border but the content line may exceed the terminal width when
  multiple nodes have long addresses.
- **Error banner positioning**: The error message is appended after
  the cluster bar as a separate line, which may push content off-screen.
  Consider integrating the error into the context bar or a dedicated
  status line.

**Files likely to modify**:

| File | Expected changes |
|------|-----------------|
| `cmd/juju/debug/dqlite_model.go` | Layout math, context bar, error display |
| `cmd/juju/debug/dqlite_databases.go` | Height/border calculation |
| `cmd/juju/debug/dqlite_objects.go` | Height/border calculation |
| `cmd/juju/debug/dqlite_detail.go` | Separator width, textarea height, viewport sizing |
| `cmd/juju/debug/dqlite_cluster.go` | Width clamping, node formatting |
| `cmd/juju/debug/dbdebug.go` | Pass controller name to model |

**Research files to read first**:
- `cmd/juju/debug/dqlite_model.go` — layout math in `layoutSubviews()`
- `cmd/juju/debug/dqlite_databases.go` — `View()` border/height math
- `cmd/juju/debug/dqlite_detail.go` — `View()` separator and textarea sizing
- `cmd/juju/debug/dqlite_cluster.go` — `View()` width handling
- `cmd/juju/debug/model.go` (debug-tui) — `viewContextBar()` and
  `paneHeight()` patterns for reference

**Verification**: `go build ./cmd/juju/...` and `go test ./cmd/juju/debug/...`
pass. Manual test: `juju db-debug` against a bootstrapped controller renders
correctly at 80x24, 120x40, and full-screen terminal sizes.

---

## Dependency graph

```
Task 1 (TUI mock) ──► Task 2 (command) ──► Task 3 (tests)
                                               │
                                               │  (can run in parallel after Task 1)
                                               ▼
                               Task 4 (client) ──► Task 5 (backend)
                                                       │
                                                       ▼
                                               Task 6 (wire real) ──► Task 7 (memory)
                                                       │
                                                       ▼
                                               Task 8 (TUI refine) ──► Task 9 (visual polish)
```

- **Tasks 1–3** are sequential and produce a working TUI demo with mock data.
- **Tasks 4 and 5** can run in parallel (client and backend are independent).
- **Task 6** requires both 4 and 5 to be complete.
- **Task 7** is final bookkeeping.
- **Task 8** refines the TUI layout and fixes two bugs.
- **Task 9** is visual polish and UX fixes, delegable to an independent agent.

## Demo milestones

| After task | What you can demo |
|------------|-------------------|
| 1 | Visual TUI with mock data (run model directly) |
| 2 | `juju db-debug` CLI launches the TUI |
| 3 | All TUI interactions verified by tests |
| 4–5 | Client + backend pass their own test suites |
| 6 | Full end-to-end: `juju db-debug` against a live controller |
| 8 | 2-column layout with scrollable viewports, no duplicate controller |
| 9 | Polished TUI that renders correctly at all terminal sizes |
