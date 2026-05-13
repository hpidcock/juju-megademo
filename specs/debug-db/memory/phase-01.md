# Phase 01 — Memory (pre-implementation notes)

## States From This Specification

All decisions below were made during spec review before any code was
written. They must be carried forward into implementation.

## File Paths

- `api/common/dqlite.go` — client implementation.
- `api/common/dqlite_test.go` — tests.

## Exported Types

All types are self-contained in `api/common`. No dependency on
`rpc/params`.

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

## Exported Methods

```go
func OpenDqlite(ctx context.Context, src base.StreamConnector) (*DqliteClient, error)
func (c *DqliteClient) Close() error
func (c *DqliteClient) Databases(ctx context.Context) ([]DqliteDatabase, error)
func (c *DqliteClient) Objects(ctx context.Context, ns string, kind string) ([]DqliteObject, error)
func (c *DqliteClient) DDL(ctx context.Context, ns string, name string) (string, error)
func (c *DqliteClient) Query(ctx context.Context, ns string, sql string, limit int) (*DqliteQueryResult, error)
func (c *DqliteClient) Cluster(ctx context.Context) ([]DqliteNode, error)
```

## Protocol Version

Constant `protocolVersion = "v1"` defined in the package.
`OpenDqlite` performs a version handshake before returning the client.
All subsequent requests carry `Version: "v1"`. `send()` validates
`resp.Version == protocolVersion` on every response.

## Request ID Generation

```go
func newRequestID() string {
    var b [4]byte
    _, _ = rand.Read(b[:])
    return fmt.Sprintf("%08x", binary.BigEndian.Uint32(b[:]))
}
```

Imports: `crypto/rand`, `encoding/binary`, `fmt`.

## Internal Request/Response Types (unexported)

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

## Send/Receive Concurrency

Single `sync.Mutex` guards `WriteJSON` + `ReadJSON` pair. One
in-flight request at a time — adequate for TUI event loop usage.

## Deviations From Phase Spec

None — these notes reflect the spec as written after review.
