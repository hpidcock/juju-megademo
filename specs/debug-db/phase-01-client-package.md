# Phase 01 — Plugable Client Package in `api/common/dqlite.go`

## Goal

Create a reusable client package that dials the `/dqlite` WebSocket and
provides typed methods for the Dqlite browser. The package mimics
`api/common/logs.go` — it wraps `ConnectControllerStream` and exchanges
JSON messages over the stream.

This package is importable by the standalone `juju db-debug` TUI (phase 02)
and by any future consumer, including the merged `juju debug` TUI.

## Dependencies

- **Phase 00** — the `/dqlite` WebSocket handler must exist and be
  reachable on the controller.

## Memory Files to Read

- `specs/debug-db/memory/phase-00.md` — exact JSON request/response
  schema, route path `/dqlite`, dbGetter wiring, value formatter
  behaviour.

## Research Required

- `api/common/logs.go` — `StreamDebugLog` wrapping `ConnectStream` and
  reading JSON records.
- `api/apiclient.go` — `ConnectControllerStream` signature, path format,
  `base.Stream` interface (`ReadJSON`, `WriteJSON`, `Close`).
- `api/base/caller.go` — `base.StreamConnector` and `base.Stream`
  interfaces.

## Scope

### 1. `api/common/dqlite.go`

```go
package common

import (
    "context"
    "encoding/json"
    "sync"

    "github.com/juju/errors"
    "github.com/juju/juju/api/base"
)

// DqliteClient provides typed access to the /dqlite websocket.
type DqliteClient struct {
    conn  base.Stream
    mu    sync.Mutex
}

// OpenDqlite dials the /dqlite websocket and returns a client.
// The caller must already be logged in to the controller API.
// The caller is responsible for calling Close when done.
func OpenDqlite(ctx context.Context, src base.StreamConnector) (*DqliteClient, error) {
    stream, err := src.ConnectControllerStream(ctx, "/dqlite", nil, nil)
    if err != nil {
        return nil, errors.Trace(err)
    }
    return &DqliteClient{conn: stream}, nil
}

// Close closes the underlying websocket stream.
func (c *DqliteClient) Close() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.conn.Close()
}
```

### 2. Exported types

```go
// DqliteDatabase describes one selectable database.
type DqliteDatabase struct {
    // Name is "controller" or the model name.
    Name string
    // UUID is empty for the controller and set for model databases.
    UUID string
    // Namespace is the database namespace used internally.
    Namespace string
    // Type is "controller" or "model".
    Type string
}

// DqliteObject describes a table, view, or trigger.
type DqliteObject struct {
    Name string
    Kind string
}

// DqliteNode describes a Dqlite cluster node.
type DqliteNode struct {
    ID      string
    Address string
    Role    string
}

// DqliteQueryResult holds the result of a read-only query.
type DqliteQueryResult struct {
    Columns   []string
    Rows      [][]string
    RowCount  int
    Truncated bool
}
```

No dependency on `rpc/params`. These types are self-contained in
`api/common`.

### 3. Typed methods

```go
// Databases returns the controller database and all model databases.
func (c *DqliteClient) Databases(ctx context.Context) ([]DqliteDatabase, error)

// Objects returns tables, views, or triggers in the given namespace.
// kind is "table", "view", or "trigger".
func (c *DqliteClient) Objects(ctx context.Context, ns string, kind string) ([]DqliteObject, error)

// DDL returns the CREATE statement for a table, view, or trigger.
func (c *DqliteClient) DDL(ctx context.Context, ns string, name string) (string, error)

// Query executes a bounded read-only query and returns formatted results.
// limit is clamped by the server to a maximum of 1000.
func (c *DqliteClient) Query(ctx context.Context, ns string, sql string, limit int) (*DqliteQueryResult, error)

// Cluster returns Dqlite cluster node information.
func (c *DqliteClient) Cluster(ctx context.Context) ([]DqliteNode, error)
```

Each method:

1. Builds an unexported `dqliteRequest` struct with `version: "v1"`, a
   `request_id` (8 hex characters from `crypto/rand` — see §4a below),
   `type`, and arguments.
2. Calls `c.send(ctx, req)`.
3. Checks `resp.Error` — if non-empty, returns `errors.New(resp.Error)`.
4. Checks `resp.Version` matches `"v1"` — if not, returns an error.
5. Unmarshals `resp.Result` from `json.RawMessage` into the appropriate
   type.
6. Returns the typed result.

**`Databases`** sends `type: "databases"` and unmarshals
`[]DqliteDatabase`.

**`Objects`** sends `type: "objects"`, `namespace`, `kind` and unmarshals
`[]DqliteObject`.

**`DDL`** sends `type: "ddl"`, `namespace`, `name` and unmarshals the
`"sql"` field from the result object.

**`Query`** sends `type: "query"`, `namespace`, `sql`, `limit` and
unmarshals `DqliteQueryResult`.

**`Cluster`** sends `type: "cluster"` and unmarshals `[]DqliteNode`.

### 4. Internals

Unexported request/response types:

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

const protocolVersion = "v1"
```

### 4a. Request ID generation

```go
import "crypto/rand"

func newRequestID() string {
    var b [4]byte
    _, _ = rand.Read(b[:])
    return fmt.Sprintf("%08x", binary.BigEndian.Uint32(b[:]))
}
```

### 4b. Version handshake

`OpenDqlite` performs a version handshake after connecting:

```go
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
```

**Note**: The handshake consumes the first message in each direction.
Subsequent requests must also carry `Version: protocolVersion` and the
`send` method validates `resp.Version` on every response.

### 4c. Send/receive with mutex

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

The mutex ensures one in-flight request at a time. The `DqliteClient` is
safe for concurrent use by a single goroutine at a time — typical for a
TUI event loop.

### 5. Tests

Create `api/common/dqlite_test.go`.

Define a `mockStream` implementing `base.Stream` with configurable
`ReadJSON` and `WriteJSON` that pass byte buffers through an internal
channel. This allows the test to set up a response before the client reads
it.

Cover:

- `OpenDqlite` — version handshake succeeds with `"v1"`.
- `OpenDqlite` — version handshake fails when server returns an error.
- `OpenDqlite` — version handshake fails when server version mismatches.
- `request_id` — two successive `newRequestID()` calls produce different
  values.
- `Databases` — parses a `[]DqliteDatabase` from the response.
- `Objects` — parses `[]DqliteObject` for each kind.
- `DDL` — parses a `dqliteDDLResult` and returns the `SQL` string.
- `Query` — parses `DqliteQueryResult` including `Truncated = true`.
- `Cluster` — parses `[]DqliteNode`.
- Server error — `resp.Error` is non-empty, method returns a Go error.
- Version mismatch on response — method returns a Go error.
- Write error — `WriteJSON` fails, method returns a Go error.
- Read error — `ReadJSON` fails, method returns a Go error.
- Concurrent sends — two goroutines calling different methods don't
  deadlock (exercises the mutex).

Run: `go test -race ./api/common/...`.

## Memory File

On completion, write `specs/debug-db/memory/phase-01.md`:

- Exported types as implemented (field names, types, JSON tags if any).
- Exported methods and their full signatures.
- File paths created.
- The internal `request_id` generation strategy (8 hex chars from
  `crypto/rand`).
- The protocol version constant and handshake logic.
- Any deviations from this phase spec and the reason.

## Acceptance Criteria

- `go build ./api/...` succeeds.
- `go test -race ./api/common/...` passes.
- The client is importable as `common.DqliteClient` from any package.
- `OpenDqlite` performs version handshake and rejects mismatches.
- Each method returns correctly typed results from valid JSON responses.
- Server errors are propagated as Go errors.
- Version mismatches on responses are propagated as Go errors.
- Write and read errors are propagated as Go errors.
- The mutex prevents data races on concurrent calls.
