# Phase 01 — Plugable Client Package in `api/common/dqlite.go`

## Goal

Create a reusable client package that dials the `/dqlite` WebSocket and
provides typed methods for the Dqlite browser. Mimics `api/common/logs.go`
wrapping pattern.

## Dependencies

- Phase 00.3 (handler core / wire protocol stable) for the request/response
  schema and version handshake.
- Phase 00.4 (route wiring) for end-to-end testing against a real
  controller.

**Task 01.1 (types) can start as soon as the wire protocol shapes are
finalized (after 00.3), before 00.4 is complete.**

## Task Breakdown

```
01.1 (types) ──► 01.2 (client + methods) ──► 01.3 (tests)
```

### 01.1 — Exported Types and Internal Structs

**File**: `api/common/dqlite.go`

**Exported types** (no JSON tags, self-contained, no dependency on
`rpc/params`):

```go
type DqliteDatabase struct {
    Name      string
    UUID      string
    Namespace string
    Type      string
}

type DqliteObject struct {
    Name string
    Kind string
}

type DqliteNode struct {
    ID      string
    Address string
    Role    string
}

type DqliteQueryResult struct {
    Columns   []string
    Rows      [][]string
    RowCount  int
    Truncated bool
}
```

**Unexported wire types** (same as server-side `dqliteRequest`/
`dqliteResponse` in phase 00, for `encoding/json`):

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

type dqliteResponse struct {
    Version   string          `json:"version"`
    RequestID string          `json:"request_id"`
    Type      string          `json:"type"`
    Error     string          `json:"error,omitempty"`
    Result    json.RawMessage `json:"result,omitempty"`
}
```

**Protocol constant**:

```go
const protocolVersion = "v1"
```

**Request ID generator**:

```go
import (
    "crypto/rand"
    "encoding/binary"
    "fmt"
)

func newRequestID() string {
    var b [4]byte
    _, _ = rand.Read(b[:])
    return fmt.Sprintf("%08x", binary.BigEndian.Uint32(b[:]))
}
```

8 hex characters. Collisions are probabilistically negligible.

### 01.2 — Client Struct, Handshake, and Methods

**File**: `api/common/dqlite.go` (add to file from 01.1)

**Research**: Read `api/base/caller.go:110-132` for `StreamConnector` and
`ControllerStreamConnector` interfaces before implementing.

**§R1 — StreamConnector type correction**: The spec declares `OpenDqlite`
with parameter `src base.StreamConnector`, then immediately calls
`src.ConnectControllerStream(...)`. The `StreamConnector` interface only
exposes `ConnectStream`, not `ConnectControllerStream`. The `/dqlite`
route is controller-scoped (no model UUID), so the caller needs
`ConnectControllerStream`.

**Fix**: Use `base.StreamConnector` (which embeds `ControllerStreamConnector`
via `base.APICaller`) since `api.Connection` embeds `base.APICaller` which
includes both interfaces. Actually:

- `api.Connection` embeds `base.APICaller` (`api/interface.go:391`)
- `base.APICaller` extends `ControllerStreamConnector` (has both
  `ConnectStream` and `ConnectControllerStream`)
- So `api.Connection` satisfies `ControllerStreamConnector`

The concrete type for the `src` parameter should be whatever the caller
has. `dbDebugCommand.Run` will pass `api.Connection`, which embeds
`base.APICaller`. The simplest approach: define a local interface
requiring only `ConnectControllerStream`:

```go
type dqliteStreamConnector interface {
    ConnectControllerStream(ctx context.Context, path string, attrs url.Values, headers http.Header) (base.Stream, error)
}
```

Or use `base.StreamConnector` and call `ConnectStream` with a
model-relative path. But the `/dqlite` route has `noModelUUID: true`,
so it's controller-scoped. Use `ConnectControllerStream`.

**Decision**: The `OpenDqlite` parameter type is `base.StreamConnector`,
but internally casts or uses the broader interface. Since in practice the
caller passes `api.Connection` which satisfies both, this works at
runtime. Record the interface expectation in the doc comment.

**Client struct:**

```go
// DqliteClient provides typed access to the /dqlite websocket.
type DqliteClient struct {
    conn base.Stream
    mu   sync.Mutex
}
```

**Constructor with handshake:**

```go
// OpenDqlite dials the /dqlite websocket and returns a client.
// The src must support ConnectControllerStream (api.Connection does).
// The caller must be logged in to the controller API.
// The caller must call Close when done.
func OpenDqlite(ctx context.Context, src base.StreamConnector) (*DqliteClient, error) {
    stream, err := src.ConnectControllerStream(ctx, "/dqlite", nil, nil)
    if err != nil {
        return nil, errors.Trace(err)
    }
    client := &DqliteClient{conn: stream}
    if err := client.handshake(ctx); err != nil {
        stream.Close()
        return nil, errors.Trace(err)
    }
    return client, nil
}

func (c *DqliteClient) handshake(ctx context.Context) error {
    c.mu.Lock()
    defer c.mu.Unlock()

    handshake := dqliteRequest{Version: protocolVersion}
    if err := c.conn.WriteJSON(handshake); err != nil {
        return errors.Errorf("version handshake write: %w", err)
    }

    var resp dqliteResponse
    if err := c.conn.ReadJSON(&resp); err != nil {
        return errors.Errorf("version handshake read: %w", err)
    }
    if resp.Error != "" {
        return errors.New(resp.Error)
    }
    if resp.Version != protocolVersion {
        return errors.Errorf("unsupported server version: %q", resp.Version)
    }
    return nil
}

func (c *DqliteClient) Close() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.conn.Close()
}
```

**send/receive:**

```go
func (c *DqliteClient) send(ctx context.Context, req dqliteRequest) (dqliteResponse, error) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if err := c.conn.WriteJSON(req); err != nil {
        return dqliteResponse{}, errors.Trace(err)
    }
    var resp dqliteResponse
    if err := c.conn.ReadJSON(&resp); err != nil {
        return dqliteResponse{}, errors.Trace(err)
    }
    return resp, nil
}
```

**Typed methods** — each follows this pattern:
1. Build `dqliteRequest` with `version: "v1"`, `request_id` (8 hex chars),
   `type`, and arguments.
2. Call `c.send(ctx, req)`.
3. If `resp.Error != ""` → return `errors.New(resp.Error)`.
4. If `resp.Version != protocolVersion` → return version mismatch error.
5. Unmarshal `resp.Result` from `json.RawMessage` into the appropriate type.
6. Return typed result.

```go
func (c *DqliteClient) Databases(ctx context.Context) ([]DqliteDatabase, error)
func (c *DqliteClient) Objects(ctx context.Context, ns string, kind string) ([]DqliteObject, error)
func (c *DqliteClient) DDL(ctx context.Context, ns string, name string) (string, error)
func (c *DqliteClient) Query(ctx context.Context, ns string, sql string, limit int) (*DqliteQueryResult, error)
func (c *DqliteClient) Cluster(ctx context.Context) ([]DqliteNode, error)
```

- **Databases**: `type: "databases"`, unmarshal `[]DqliteDatabase`.
- **Objects**: `type: "objects"`, `namespace`, `kind`, unmarshal
  `[]DqliteObject`.
- **DDL**: `type: "ddl"`, `namespace`, `name`, unmarshal `"sql"` field
  from result object.
- **Query**: `type: "query"`, `namespace`, `sql`, `limit`, unmarshal
  `DqliteQueryResult`.
- **Cluster**: `type: "cluster"`, unmarshal `[]DqliteNode`.

The `sync.Mutex` ensures one in-flight request at a time — adequate for
single-goroutine TUI event loop usage.

**DDL helper**: The server returns `{"name": "...", "sql": "CREATE..."}`.
Unmarshal into a temporary struct:

```go
type dqliteDDLResult struct {
    Name string `json:"name"`
    SQL  string `json:"sql"`
}
```

Extract the `SQL` string to return.

### 01.3 — Tests

**File**: `api/common/dqlite_test.go`

Define `mockStream` implementing `base.Stream` with configurable
`ReadJSON`/`WriteJSON` buffers. This allows setting up responses before
reading.

```go
type mockStream struct {
    readJSON  func(v interface{}) error
    writeJSON func(v interface{}) error
    closed    bool
}

func (m *mockStream) ReadJSON(v interface{}) error  { return m.readJSON(v) }
func (m *mockStream) WriteJSON(v interface{}) error { return m.writeJSON(v) }
func (m *mockStream) Close() error                  { m.closed = true; return nil }
```

Define `mockStreamConnector` with a `ConnectControllerStream` method for
`OpenDqlite` tests (note: uses `ConnectControllerStream` because the
`/dqlite` route is controller-scoped):

```go
type mockStreamConnector struct {
    stream *mockStream
    err    error
}

func (m *mockStreamConnector) ConnectControllerStream(
    ctx context.Context, path string, attrs url.Values, headers http.Header,
) (base.Stream, error) {
    return m.stream, m.err
}
```

Cover:
- `OpenDqlite` — version handshake succeeds with `"v1"`.
- `OpenDqlite` — handshake fails when server returns error.
- `OpenDqlite` — handshake fails when server version mismatches.
- `OpenDqlite` — handshake fails on write error.
- `OpenDqlite` — handshake fails on read error.
- `newRequestID` — two successive calls produce different values.
- `Databases` — parses `[]DqliteDatabase`.
- `Objects` — parses `[]DqliteObject`.
- `DDL` — parses `dqliteDDLResult` and returns the SQL string.
- `Query` — parses `DqliteQueryResult` including `Truncated = true`.
- `Cluster` — parses `[]DqliteNode`.
- Server error — `resp.Error` non-empty → Go error.
- Version mismatch on response → Go error.
- Write error → Go error.
- Read error → Go error.
- Concurrent sends — two goroutines calling different methods don't
  deadlock (exercises mutex).

Run: `go test -race ./api/common/...`

## Acceptance Criteria

- `go build ./api/...` succeeds.
- `go test -race ./api/common/...` passes.
- Client importable as `common.DqliteClient`.
- `OpenDqlite` performs version handshake, rejects mismatches.
- Each method returns correctly typed results.
- Server errors propagate as Go errors.
- Mutex prevents data races.

## Memory File

Write `specs/debug-db/memory/phase-01.md` updating the pre-implementation
notes with actual implementation details.

## Deviations from Spec Phase-01

| # | Deviation | Reason |
|---|----------|--------|
| R1 | `OpenDqlite` declared with `base.StreamConnector` but calls `ConnectControllerStream` internally | `/dqlite` is controller-scoped. The passed `api.Connection` satisfies both interfaces. Pattern follows `api/common/logs.go` using `ConnectStream` for model-scoped, but `ConnectControllerStream` needed here for `noModelUUID: true` route. |
