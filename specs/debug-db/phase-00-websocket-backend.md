# Phase 00 — Controller /dqlite WebSocket Handler

## Goal

Add a `/dqlite` WebSocket route to the controller API server. The handler
authenticates as controller superuser, dispatches JSON requests against
the Dqlite databases, enforces a read-only SQL policy, and returns JSON
responses.

## Dependencies

None. This phase may begin immediately.

## Research Required

Before writing any code, read the following:

- `apiserver/debuglog.go` — WebSocket handler pattern, `websocket.Serve`,
  auth after upgrade, `debugLogSocketImpl`.
- `apiserver/apiserver.go` — handler slice with `pattern`, `handler`,
  `authorizer`, `tracked`, `noModelUUID`, `monitoredHandler`.
- `apiserver/authentication/` — `controllerAdminAuthorizer`.
- `internal/worker/dbreplaccessor/worker.go` — `DBGetter.GetDB`,
  `DescribeCluster`.

## Existing Behaviour To Preserve

- `juju_db_repl` shell function and `jujud db-repl` command are unchanged.
- `httpContext` is not modified.
- No DI registration changes.

## Scope

### 1. `apiserver/dqlite.go` — handler

Create a single file containing the handler implementation.

The handler receives a `DBGetter` closure through its constructor:

```go
// DBGetter returns a transaction runner for a database namespace.
type DBGetter func(namespace string) (database.TxnRunner, error)

// newDqliteHandler creates the /dqlite websocket handler.
func newDqliteHandler(
    ctxt httpContext,
    dbGetter DBGetter,
    authorizer authentication.Authorizer,
) *dqliteHandler
```

The handler implements `http.Handler`:

```go
type dqliteHandler struct {
    ctxt       httpContext
    dbGetter   DBGetter
    authorizer authentication.Authorizer
}
```

`ServeHTTP`:

1. Calls `websocket.Serve(w, req, handlerFunc)`.
2. Inside the handler func:
   - Authenticate via `h.ctxt.authenticator.Authenticate(req)` — same
     pattern as `debuglog.go`.
   - Authorize via `h.authorizer.Authorize(req.Context(), authInfo)` —
     uses `controllerAdminAuthorizer` from the route registration.
   - Read JSON request messages in a loop via `websocket.Conn.ReadJSON`.
   - For each request, dispatch by `type` and write a response via
     `websocket.Conn.WriteJSON`.
   - On read error or EOF, return nil (client disconnected gracefully).

**JSON request** (unexported):

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
```

**JSON response** (unexported):

```go
type dqliteResponse struct {
    Version   string      `json:"version"`
    RequestID string      `json:"request_id"`
    Type      string      `json:"type"`
    Error     string      `json:"error,omitempty"`
    Result    interface{} `json:"result,omitempty"`
}
```

**Version negotiation**: The first message in each direction is a handshake.
The client sends `{"version": "v1"}` with `type: ""` and the server
responds with `{"version": "v1"}`. If the server receives an unknown
version, it responds with `{"version": "<server_version>", "error":
"unsupported version <client_version>"}` and closes the connection.
After a successful handshake, subsequent messages must include
`"version": "v1"` and the server echoes it back. This ensures forward
compatibility when new fields or request types are added.

**JSON response** (unexported):

```go
type dqliteResponse struct {
    Version   string      `json:"version"`
    RequestID string      `json:"request_id"`
    Type      string      `json:"type"`
    Error     string      `json:"error,omitempty"`
    Result    interface{} `json:"result,omitempty"`
}
```

**Version negotiation**: The first message in each direction is a handshake.
The client sends `{"version": "v1"}` with `type: ""` and the server
responds with `{"version": "v1"}`. If the server receives an unknown
version, it responds with `{"version": "<server_version>", "error":
"unsupported version <client_version>"}` and closes the connection.
After a successful handshake, subsequent messages must include
`"version": "v1"` and the server echoes it back. This ensures forward
compatibility when new fields or request types are added.

### 2. Request dispatch

| type | action |
|------|--------|
| `"databases"` | Query `SELECT uuid, name FROM model` on the controller db. Prepend a controller entry. Return `[]dqliteDatabase`. |
| `"objects"` | Query `SELECT name FROM sqlite_master WHERE type = ?` using `kind` on the target namespace. Return `[]dqliteObject`. |
| `"ddl"` | Query `SELECT sql FROM sqlite_master WHERE name = ?` on the target namespace. Return `dqliteDDLResult`. |
| `"query"` | Validate SQL, execute with limit on the target namespace, format rows. Return `dqliteQueryResult`. |
| `"cluster"` | Call `dbGetter` for the controller namespace. If the returned `TxnRunner` implements `ClusterIntrospector`, call `DescribeCluster` and return `[]dqliteNode`. Otherwise return an empty list. |

The handler package defines its own `ClusterIntrospector` interface
to avoid a direct import of `dbreplaccessor`:

```go
// ClusterIntrospector describes the cluster topology of a dqlite node.
type ClusterIntrospector interface {
    DescribeCluster(context.Context) ([]dbreplaccessor.NodeInfo, error)
}
```

If `dbreplaccessor.NodeInfo` is not importable from the apiserver package,
define a small local struct:

```go
type clusterNodeInfo struct {
    ID      string
    Address string
    Role    string
}
```

with a converter from `dbreplaccessor.NodeInfo` to `clusterNodeInfo`
inside the handler's cluster dispatch block.

Result types for JSON serialization (unexported):

```go
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

### 3. Read-only SQL policy

After trimming whitespace, extract the first SQL keyword (stopping at
the first whitespace, `;`, or end of string). Case-fold the keyword with
`strings.ToUpper`.

**Accepted:**
- `select`
- `with`
- `explain`
- `pragma_table_info`
- `pragma_foreign_key_list`
- `pragma_index_list`
- `pragma_index_info`
- `pragma_table_xinfo`

**Rejected:**
- `insert`, `update`, `delete`, `replace`, `create`, `alter`, `drop`,
  `truncate`, `vacuum`, `attach`, `detach`, `reindex`, `savepoint`,
  `release`, `commit`, `rollback`, `begin`
- Any `pragma` not in the accepted list above
- Any statement with a second non-empty part after splitting on `;`

Multiple statements are detected by splitting on `;`, trimming each part,
and rejecting if more than one non-empty part exists.

**Semicolons inside string literals**: The `;`-split approach may
produce false positives when semicolons appear inside single-quoted
strings (e.g. `SELECT 'hello;world'`). Implementers must minimally handle
this by scanning outside of single-quoted regions. A SQL tokenizer is not
strictly required for the initial proof-of-concept, but the test suite
must include a benign semicolon-inside-string case to document the
behaviour.

The handler must also enforce the SQL policy at execution time. Wrap the
transaction so it cannot mutate:

```go
_, err := tx.ExecContext(ctx, "PRAGMA query_only = ON")
if err != nil {
    return err
}
// ... then execute the user query ...
```

### 4. Row limit

Effective limit: `min(requestedLimit, maxRowLimit)`, with:

```text
default limit:  100
maximum limit:  1000
```

If `requestedLimit <= 0`, use the default. Append `LIMIT <effectiveLimit>`
to the user's SQL before execution. If the SQL already contains a `LIMIT`
clause, replace it with `LIMIT <effectiveLimit>`.

### 5. Query timeout

Wrap the query context with a deadline:

```go
ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()
```

Pass this context to `db.StdTxn(ctx, ...)`.

### 6. Value formatter

```go
func formatValue(v any) string {
    if v == nil {
        return "NULL"
    }
    switch val := v.(type) {
    case []byte:
        s := hex.EncodeToString(val)
        if len(s) > 256 {
            return "0x" + s[:256] + "..."
        }
        return "0x" + s
    case time.Time:
        return val.Format(time.RFC3339Nano)
    case fmt.Stringer:
        return val.String()
    default:
        return fmt.Sprintf("%v", v)
    }
}
```

Truncation detection: if `rows.Scan` yields more rows than the effective
limit, set `Truncated = true`. Only return up to `effectiveLimit` rows.

### 7. Route registration

In `apiserver/apiserver.go`, add to the handler slice (as the last entry
before the closing `}}`):

```go
{
    pattern:    "/dqlite",
    handler:    srv.monitoredHandler(
        newDqliteHandler(httpCtxt, srv.shared.dqliteDBGetter, controllerAdminAuthorizer),
        "dqlite",
    ),
    authorizer:    controllerAdminAuthorizer,
    tracked:       true,
    noModelUUID:   true,
},
```

### 8. `srv.shared.dqliteDBGetter`

Add a field to the shared server config struct (look for the struct that
holds `*lease.Manager`, `*pubsub.Controller`, etc. — typically named
`sharedServerConfig` in `apiserver/apiserver.go`):

```go
dqliteDBGetter DBGetter
```

Set it during server construction (likely in `newServer` or
`NewServer`) from the same dqlite connection that the `dbreplaccessor`
worker uses. The implementation will call `NewDBGetter(worker)` where
`worker` provides access to Dqlite namespaces. Record the exact file
path and line number in the memory file.

This is a wiring change only — no new DI registration, no new
interfaces on the shared config beyond the `DBGetter` function type.
The `DBGetter` type is defined locally in `apiserver/dqlite.go`.

### 9. Tests

Create `apiserver/dqlite_test.go`.

Use a mock `DBGetter` closure that returns an in-memory `*sql.DB` (opened
with `sql.Open("sqlite3", ":memory:")`) with a pre-populated
`sqlite_master` table containing `change_log`, `model`, and a test view.

Cover:

- Version negotiation handshake succeeds with `"v1"`.
- Unknown version returns error and closes.
- `"databases"` returns controller + model entries.
- `"objects"` returns table/view/trigger names for valid `kind` values.
- `"ddl"` returns the `CREATE` statement for a known table.
- `"query"` returns formatted rows for a valid `SELECT`.
- Mutation SQL returns an error in the JSON response.
- Multi-statement SQL returns an error.
- Semicolons inside single-quoted string literals are tolerated
  (e.g. `SELECT ';'` is accepted).
- `NULL` values formatted as `"NULL"`.
- `[]byte` values formatted as hex.
- `time.Time` values formatted as RFC3339Nano.
- Row limit is enforced and `Truncated` is set.
- Query timeout is enforced (cancelled context).
- Auth failure returns an error — mock `controllerAdminAuthorizer` to
  reject.
- Graceful disconnect — read error returns nil, no panic.

Run: `go test ./apiserver/...`.

## Memory File

On completion, write `specs/debug-db/memory/phase-00.md`:

- Exact file paths created.
- The `dqliteDBGetter` wiring location (shared struct field + assignment
  point).
- The route entry as registered (exact handler slice addition and line
  number).
- The read-only SQL policy as implemented (accepted/rejected words).
- The default and maximum row limits.
- Any deviations from this phase spec and the reason.

## Acceptance Criteria

- `go build ./...` succeeds.
- `go test ./apiserver/...` passes.
- The `/dqlite` route is reachable on the controller.
- A binary WebSocket client can:
  - Perform version negotiation (handshake).
  - List databases (controller + models).
  - List tables, views, triggers for a database.
  - Retrieve DDL for a table/view/trigger.
  - Execute a `SELECT` and receive formatted rows.
  - Receive an error for `INSERT`/`UPDATE`/`DELETE` statements.
  - Receive an error for multi-statement queries.
  - Receive an error for unsupported protocol version.
  - See truncation when results exceed the row limit.
  - Describe cluster nodes.