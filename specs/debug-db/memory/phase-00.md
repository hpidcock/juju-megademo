# Phase 00 — Memory (pre-implementation notes)

## States From This Specification

- All decisions below were made during spec review before any code was
  written. They must be carried forward into implementation.

## Constructor Signature

```go
func newDqliteHandler(
    ctxt httpContext,
    dbGetter DBGetter,
    authorizer authentication.Authorizer,
) *dqliteHandler
```

The authorizer is `controllerAdminAuthorizer` from the caller (route
registration). The handler struct field is `authorizer
authentication.Authorizer`.

## DBGetter Type

```go
type DBGetter func(namespace string) (database.TxnRunner, error)
```

Defined locally in `apiserver/dqlite.go`. Not exported.

## Wiring Location

`dqliteDBGetter` field added to `sharedServerConfig` (likely in
`apiserver/apiserver.go`). Set during `newServer` / `NewServer` from the
same source the `dbreplaccessor` worker uses.

Implementer must record:
- Exact file path and line number where `dqliteDBGetter` is added to the
  shared config struct.
- Exact file path and line number where it is assigned.

## Route Registration

Entry added as last element in the handler slice in
`apiserver/apiserver.go`:

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

Record the exact line number where this block is inserted.

## Protocol Version

`"v1"`. All JSON messages carry a `"version"` field. Handshake on
connect: client sends `{"version": "v1"}` with `type: ""`, server
echoes back. Unknown version causes server to respond with error and
close the connection.

## ClusterIntrospector Interface

Defined in `apiserver/dqlite.go` to avoid direct import of
`dbreplaccessor`:

```go
type ClusterIntrospector interface {
    DescribeCluster(context.Context) ([]dbreplaccessor.NodeInfo, error)
}
```

If importing `dbreplaccessor.NodeInfo` is not feasible from the
apiserver package, define a local `clusterNodeInfo` struct with
`ID`, `Address`, `Role` string fields, and convert in the dispatch
block.

## Read-Only SQL Policy

**Accepted keywords** (case-folded via `strings.ToUpper`):
`SELECT`, `WITH`, `EXPLAIN`, `PRAGMA_TABLE_INFO`,
`PRAGMA_FOREIGN_KEY_LIST`, `PRAGMA_INDEX_LIST`, `PRAGMA_INDEX_INFO`,
`PRAGMA_TABLE_XINFO`.

**Rejected**: `INSERT`, `UPDATE`, `DELETE`, `REPLACE`, `CREATE`,
`ALTER`, `DROP`, `TRUNCATE`, `VACUUM`, `ATTACH`, `DETACH`, `REINDEX`,
`SAVEPOINT`, `RELEASE`, `COMMIT`, `ROLLBACK`, `BEGIN`, any PRAGMA
not in accepted list.

**Multi-statement detection**: split on `;`, trim, reject if >1
non-empty part.

**Semicolons in string literals**: the `;`-split must skip content
inside `'...'` single-quoted strings to avoid false positives.

**Execution-time guard**: `PRAGMA query_only = ON` before each user
query. Defense in depth.

## Row Limits

- Default: 100
- Maximum: 1000
- Effective: `min(limit, 1000)`, defaulting to 100 if `limit <= 0`.
- `LIMIT` clause in user SQL is replaced with effective limit.
- Truncation flag set when `rows.Scan` produces more than effective
  limit.

## Query Timeout

10 seconds per query via `context.WithTimeout(ctx, 10*time.Second)`.

## Deviations From Phase Spec

None — these notes reflect the spec as written after review.
