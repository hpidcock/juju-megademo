# Debug DB — Phase Index

## Phase List

| # | File | Description | Depends on |
|---|------|-------------|------------|
| 00 | [phase-00-websocket-backend.md](phase-00-websocket-backend.md) | Controller /dqlite WebSocket handler | — |
| 01 | [phase-01-client-package.md](phase-01-client-package.md) | Plugable client in api/common/dqlite.go | 00 |
| 02 | [phase-02-tui.md](phase-02-tui.md) | juju db-debug command + bubbletea TUI | 01 |

## Dependency Graph

```
00 (WebSocket backend on controller)
    │
    ▼
01 (Client package — api/common/dqlite.go)
    │
    ▼
02 (TUI — juju db-debug)
```

All phases are strictly sequential.

## Suggested Execution Order

1. Phase 00 (must be first).
2. Phase 01 (needs 00).
3. Phase 02 (needs 01).

## Parallel Work Opportunities

None. Each phase depends on the previous one.

## Wire Protocol

All messages over the `/dqlite` WebSocket carry a `"version"` field
(currently `"v1"`). Phase 00 defines the handshake: client sends
version first, server responds with matching version or an error.
All subsequent request/response pairs echo the version. See
`dqliteRequest` and `dqliteResponse` structs in
[phase-00](phase-00-websocket-backend.md).

## Key Architecture Decisions

- **Read-only by design**: The handler applies a SQL keyword whitelist
  AND sets `PRAGMA query_only = ON` on every transaction. Defense in
  depth.
- **Cluster introspection**: Phase 00 defines its own
  `ClusterIntrospector` interface to avoid importing `dbreplaccessor`
  directly into the apiserver package. A thin adapter converts
  `NodeInfo` structs at the call site.
- **Request IDs**: 8 hex chars from `crypto/rand` (phase 01 §4a).
  Collisions are probabilistically negligible at these volumes.
- **Row limits**: Default 100, max 1000. Enforced server-side with
  `LIMIT` clause rewriting. Truncation indicator sent to client.
- **Pre-selection**: `--database` flag on `juju db-debug` selects the
  matching database immediately after initial load without requiring
  user navigation.

## Wire Protocol

All messages over the `/dqlite` WebSocket carry a `"version"` field
(currently `"v1"`). Phase 00 defines the handshake: client sends
version first, server responds with matching version or an error.
All subsequent request/response pairs echo the version. See
`dqliteRequest` and `dqliteResponse` structs in
[phase-00](phase-00-websocket-backend.md).

## Key Architecture Decisions

- **Read-only by design**: The handler applies a SQL keyword whitelist
  AND sets `PRAGMA query_only = ON` on every transaction. Defense in
  depth.
- **Cluster introspection**: Phase 00 defines its own
  `ClusterIntrospector` interface to avoid importing `dbreplaccessor`
  directly into the apiserver package. A thin adapter converts
  `NodeInfo` structs at the call site.
- **Request IDs**: 8 hex chars from `crypto/rand` (phase 01 §4a).
  Collisions are probabilistically negligible at these volumes.
- **Row limits**: Default 100, max 1000. Enforced server-side with
  `LIMIT` clause rewriting. Truncation indicator sent to client.
- **Pre-selection**: `--database` flag on `juju db-debug` selects the
  matching database immediately after initial load without requiring
  user navigation.

## Relationship To debug-tui

The `juju db-debug` TUI is a standalone proof-of-concept. It uses the same
bubbletea/bubbles/lipgloss stack prescribed in `specs/debug-tui.md`. The
`dqliteModel` is designed as a composable bubbletea `Model` that can later
be embedded as a tab in the `juju debug` TUI. The `api/common/dqlite.go`
client package is importable by any future consumer, including the merged
debug TUI.
