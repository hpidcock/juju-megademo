# Phase 00 — Controller `/dqlite` WebSocket Handler

## Goal

Add a `/dqlite` WebSocket route to the controller API server that
authenticates as controller superuser, dispatches JSON requests against
Dqlite databases, enforces a read-only SQL policy, and returns JSON
responses.

## Dependencies

None. Starts immediately.

## Task Breakdown

Five sub-tasks. 00.1 and 00.2 can run in parallel; 00.3 needs both; 00.4
needs 00.3; 00.5 runs after 00.4.

```
00.1 (SQL policy) ──────────────┐
                                ├──► 00.3 (dispatch) ──► 00.4 (wiring) ──► 00.5 (tests)
00.2 (value formatter) ─────────┘
```

### 00.1 — SQL Policy Validator

**File**: `apiserver/dqlite_policy.go`

Create a standalone `sqlPolicy` type with three exported methods:

```go
// validateSQL checks that the SQL is a single read-only statement.
// Returns nil if valid, or an error describing the rejection reason.
func (p *sqlPolicy) validateSQL(sql string) error
```

**Implementation**:

1. Trim whitespace.
2. Extract first SQL keyword: scan to first whitespace, `;`, or end of
   string. Case-fold with `strings.ToUpper`.
3. **Accepted keywords**: `SELECT`, `WITH`, `EXPLAIN`,
   `PRAGMA_TABLE_INFO`, `PRAGMA_FOREIGN_KEY_LIST`, `PRAGMA_INDEX_LIST`,
   `PRAGMA_INDEX_INFO`, `PRAGMA_TABLE_XINFO`.
4. **Rejected**: any mutation keyword (`INSERT`, `UPDATE`, `DELETE`,
   `REPLACE`, `CREATE`, `ALTER`, `DROP`, `TRUNCATE`, `VACUUM`, `ATTACH`,
   `DETACH`, `REINDEX`, `SAVEPOINT`, `RELEASE`, `COMMIT`, `ROLLBACK`,
   `BEGIN`) and any PRAGMA not in the accepted list.
5. **Multi-statement detection**: split on `;`, but skip content inside
   `'...'` single-quoted strings. After split, trim each part.
   Reject if more than one non-empty part exists.
6. Return clear error messages (e.g. `"INSERT not allowed"`,
   `"multiple statements not allowed"`).

**Design rationale**: Extracted into its own file so it can be unit-tested
independently before the handler exists.

**Test file**: `apiserver/dqlite_policy_test.go`

Test cases (each accepted/rejected keyword, edge cases):
- Each accepted keyword passes.
- Each rejected keyword fails.
- `SELECT * FROM foo WHERE x = ';'` passes (semicolons inside strings).
- `SELECT 1; SELECT 2` fails (multi-statement).
- `SELECT ';'; -- comment` fails (two non-empty parts after trimming).
- Empty input, whitespace-only input.
- `SELECT 'hello;world'` passes (single statement, semicolons in string).
- `WITH cte AS (SELECT 1) SELECT * FROM cte` passes.
- `EXPLAIN SELECT * FROM foo` passes.
- `PRAGMA table_info('foo');` passes (semicolons are consumed by split but
  only one non-empty part after `'...'` content handling).

### 00.2 — Value Formatter

**File**: `apiserver/dqlite_format.go`

Create a standalone `formatValue` function:

```go
func formatValue(v any) string
```

Rules (from spec):
- `nil` → `"NULL"`
- `[]byte` → `"0x" + hex.EncodeToString(val)` (truncated to 256 chars
  with `"..."` suffix if longer)
- `time.Time` → `val.Format(time.RFC3339Nano)`
- `fmt.Stringer` → `val.String()`
- Everything else → `fmt.Sprintf("%v", v)`

**Test file**: `apiserver/dqlite_format_test.go`

Test cases:
- `nil` → `"NULL"`
- `[]byte{0x01, 0x02, 0xff}` → `"0x0102ff"`
- `[]byte` of length > 256 → truncated hex with `"..."`
- `time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)` →
  `"2025-01-15T10:30:00Z"`
- `int64(42)` → `"42"`
- `string("hello")` → `"hello"`
- `bool(true)` → `"true"`
- A custom `fmt.Stringer` type.

### 00.3 — Handler Core

**File**: `apiserver/dqlite.go`

Read `apiserver/debuglog.go` for the WebSocket handler pattern before
implementing.

**Struct**:

```go
type dqliteHandler struct {
    ctxt       httpContext
    dbGetter   DBGetter
    authorizer authentication.Authorizer
    policy     *sqlPolicy
    maxLimit   int
}
```

**Constructor**:

```go
func newDqliteHandler(
    ctxt httpContext,
    dbGetter DBGetter,
    authorizer authentication.Authorizer,
) *dqliteHandler
```

**DBGetter type** (defined locally in this file, not exported):

```go
type DBGetter func(namespace string) (database.TxnRunner, error)
```

**ClusterIntrospector interface** (local to this file):

```go
type ClusterIntrospector interface {
    DescribeCluster(context.Context) ([]dbreplaccessor.NodeInfo, error)
}
```

If `dbreplaccessor.NodeInfo` is not importable from the apiserver package,
define a local adapter struct `clusterNodeInfo{ID, Address, Role string}`
and convert in the dispatch block.

**clusterNodeInfo** (if fallback needed):

```go
type clusterNodeInfo struct {
    ID      string
    Address string
    Role    string
}
```

**Constants**:

```go
const (
    dqliteMaxRowLimit    = 1000
    dqliteDefaultRowLimit = 100
    dqliteQueryTimeout    = 10 * time.Second
    dqliteProtocolVersion = "v1"
)
```

**ServeHTTP** (`http.Handler`):

```go
func (h *dqliteHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    websocket.Serve(w, req, h.handleConnection)
}
```

**handleConnection**:

1. Authenticate via `h.ctxt.authenticator.Authenticate(req)`.
2. Authorize via `h.authorizer.Authorize(req.Context(), authInfo)`.
3. Enter read loop — `websocket.Conn.ReadJSON(&reqMsg)` in a loop.
4. First message is the version handshake:
   - Client sends `{"version": "v1", "type": ""}`.
   - If `req.Version != dqliteProtocolVersion`, respond with error
     `"unsupported version <client_version>"`, close connection.
   - Otherwise respond with `{"version": "v1"}`.
5. For each subsequent message, validate `req.Version == dqliteProtocolVersion`,
   then dispatch by `req.Type`:

**Dispatch table**:

| `req.Type` | Handler | Key inputs |
|------------|---------|-----------|
| `"databases"` | Query `SELECT uuid, name FROM model` on controller DB. Prepend controller entry. | — |
| `"objects"` | `SELECT name FROM sqlite_master WHERE type = ?` | `req.Kind` → `kind` |
| `"ddl"` | `SELECT sql FROM sqlite_master WHERE name = ?` | `req.Namespace`, `req.Name` |
| `"query"` | Validate SQL → execute with limit → format rows | `req.Namespace`, `req.SQL`, `req.Limit` |
| `"cluster"` | Cast `TxnRunner` to `ClusterIntrospector`, call `DescribeCluster` | — |

**Query handler details**:

1. Call `h.policy.validateSQL(req.SQL)` — if error, return JSON error.
2. Compute effective limit: `min(req.Limit, maxRowLimit)`, default 100 if
   `req.Limit <= 0`.
3. If the SQL contains `LIMIT <N>`, replace with `LIMIT <effectiveLimit>`.
   Otherwise append `LIMIT <effectiveLimit>`.
4. Create context with `10*time.Second` timeout.
5. Open transaction via `h.dbGetter(req.Namespace)`.
6. Execute `PRAGMA query_only = ON`.
7. Execute user SQL, scan rows. Format each column value with `formatValue`.
8. If more rows than effective limit, set `Truncated = true`. Return only
   `effectiveLimit` rows.

**Request/response types** (unexported, same as spec phase-00 §1):

```go
type dqliteRequest struct {
    Version   string `json:"version"`
    RequestID string `json:"request_id"`
    Type      string `json:"type"`
    Namespace string `json:"namespace,omitempty"`
    Kind      string `json:"kind,omitempty"`
    Name      string `json:"name,omitempty"`
    SQL       string `json:"sql,omitempty"`
    Limit     int    `json:"limit,omitempty"`
}

type dqliteDatabase struct {
    Name      string `json:"name"`
    UUID      string `json:"uuid"`
    Namespace string `json:"namespace"`
    Type      string `json:"type"`
}

type dqliteObject struct {
    Name string `json:"name"`
    Kind string `json:"kind"`
}

type dqliteNode struct {
    ID      string `json:"id"`
    Address string `json:"address"`
    Role    string `json:"role"`
}

type dqliteDDLResult struct {
    Name string `json:"name"`
    SQL  string `json:"sql"`
}

type dqliteQueryResult struct {
    Columns   []string   `json:"columns"`
    Rows      [][]string `json:"rows"`
    RowCount  int        `json:"row_count"`
    Truncated bool       `json:"truncated"`
}
```

**Response** (unexported):

```go
type dqliteResponse struct {
    Version   string      `json:"version"`
    RequestID string      `json:"request_id"`
    Type      string      `json:"type"`
    Error     string      `json:"error,omitempty"`
    Result    interface{} `json:"result,omitempty"`
}
```

### 00.4 — Route Wiring

**File**: `apiserver/apiserver.go`

Two changes:

**A. Add `dqliteDBGetter` to `sharedServerConfig`** in `apiserver/shared.go:90-113`:

```go
dqliteDBGetter DBGetter
```

Insert as the last field of `sharedServerConfig` (after `logDir`), with a
blank line separator and a comment grouping it with the existing
`dbGetter` field.

**B. Set `dqliteDBGetter` in `newServer`** at `apiserver/apiserver.go:379-401`:

```go
dqliteDBGetter: cfg.DqliteDBGetter,
```

Add `DqliteDBGetter DBGetter` to `ServerConfig` (the exported config
struct passed to `NewServer`). Locate its definition in
`apiserver/apiserver.go` and add this field.

**C. Add route entry** to the handler slice in `apiserver/apiserver.go`,
just after line 1045 (before the closing `}}` at line 1046):

```go
    }, {
        pattern:    "/dqlite",
        handler:    srv.monitoredHandler(
            newDqliteHandler(httpCtxt, srv.shared.dqliteDBGetter, controllerAdminAuthorizer),
            "dqlite",
        ),
        authorizer:  controllerAdminAuthorizer,
        tracked:     true,
        noModelUUID: true,
    }}
```

The `controllerAdminAuthorizer` variable is already in scope (line 780).

**D. Wire `DBGetter` from existing Dqlite source**. In the `ServerConfig`
→ `sharedServerConfig` path, obtain the `DBGetter`. The `cfg.DBGetter`
field (`changestream.WatchableDBGetter`) likely provides or wraps a
Dqlite-backed `TxnRunner`. The adapter function:

```go
func dqliteDBGetterFromConfig(cfg ServerConfig) DBGetter {
    return func(namespace string) (database.TxnRunner, error) {
        return cfg.Dqlite.GetDB(namespace)
    }
}
```

This is called in the `newServer` construction of `sharedServerConfig`.
The exact field name on `ServerConfig` to reach the Dqlite database
depends on the current struct definition — inspect `ServerConfig` in
`apiserver/apiserver.go` to find the Dqlite field.

### 00.5 — Tests

**Files**:
- `apiserver/dqlite_policy_test.go`
- `apiserver/dqlite_format_test.go`
- `apiserver/dqlite_test.go`

**dqlite_test.go** uses:

- Mock `DBGetter` returning an in-memory `*sql.DB` (`sql.Open("sqlite3",
  ":memory:")`) with pre-populated `sqlite_master` containing `change_log`,
  `model`, and a test view.
- Mock `controllerAdminAuthorizer` from the existing test suite
  (`authorizercontroller_test.go`).
- `websocket.Conn` via `nettest` or the existing WebSocket test harness.

Cover:
- Version negotiation succeeds with `"v1"`.
- Unknown version returns error and closes.
- `"databases"` returns controller + model entries.
- `"objects"` returns tables/views/triggers for each `kind`.
- `"ddl"` returns the CREATE statement for a known table.
- `"query"` returns formatted rows for valid SELECT.
- Mutation SQL returns error in JSON response.
- Multi-statement SQL returns error.
- Semicolons inside string literals tolerated.
- `NULL` → `"NULL"`, `[]byte` → hex, `time.Time` → RFC3339Nano.
- Row limit enforced; `Truncated` set when exceeded.
- Query timeout enforced.
- Auth failure returns error.
- Graceful disconnect (read error → return nil).

Run: `go test -race ./apiserver/...`

## Acceptance Criteria

- `go build ./...` succeeds.
- `go test -race ./apiserver/...` passes.
- `/dqlite` route reachable on the controller.
- Binary WebSocket client can: handshake, list databases, list objects,
  retrieve DDL, execute SELECT, receive errors for mutations, see
  truncation, describe cluster.

## Memory File

Write `specs/debug-db/memory/phase-00.md` updating the pre-implementation
notes with actual file paths and line numbers after implementation.

## Deviations from Spec Phase-00

| # | Deviation | Reason |
|---|----------|--------|
| D1 | SQL policy extracted to `dqlite_policy.go` | Enables independent testing; cleaner separation |
| D2 | Value formatter extracted to `dqlite_format.go` | Same rationale as D1 |
| D3 | Handler constructor takes `*sqlPolicy` or creates default internally | Cleaner DI, testable with custom policy |

No changes to the wire protocol, accepted/rejected keywords, row limits,
or timeout behavior.
