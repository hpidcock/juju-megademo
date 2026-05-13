# Implementation Plan — `juju db-debug`

## Summary

Three-phase implementation adding a read-only Dqlite database browser to Juju:

| # | Phase | What | Effort | Can start |
|---|-------|------|--------|-----------|
| 00 | WebSocket backend | `/dqlite` route on the controller API server | 2-3 days | Immediately |
| 01 | Client package | Reusable `api/common/dqlite.go` client | 1-2 days | After 00.3 |
| 02 | TUI | `juju db-debug` command + bubbletea TUI | 2-3 days | Immediately (mock-based) |

## Revised Dependency Graph

The original spec says phases are strictly sequential. This plan maximizes
parallelism by splitting Phase 02 into two tracks:

```
TRACK A (backend):
  00.1 ──► 00.2 ──► 00.3 ──► 00.4 ──► 00.5
                                       │
                                       ▼
                              01.1 ──► 01.2 ──► 01.3
                                                   │
                                                   ▼
                                          02.6 (wiring)

TRACK B (TUI, parallel from day 1):
  02.1 ──► 02.2 ──► 02.3 ──► 02.4 ──► 02.5
                                          │
                                          ▼
                                  EARLY DEMO with mock data
                                          │
                                          ▼
                                  02.6 (wire to real API)
```

**Key insight**: Phase 02 tasks 02.1–02.5 (TUI model, panes, keyboard
handling, rendering, mock-backed tests) can start on day 1 in parallel
with Phase 00. The only serial dependency is 02.6 (wiring the real client
in `dbDebugCommand.Run`), which needs 01.3 to be complete.

## Early Demo Milestone

After completing 02.1–02.5 (TUI + mock API), the TUI is fully functional
with static sample data. This milestone can be achieved **before** any
backend code exists:

```bash
# Demo: run TUI with mock data (no controller needed)
go test -run TestDqliteModelFullFlow ./cmd/juju/debug/... -v
# Or: write a small demo main that injects mockDqliteAPI
```

The demo shows all four panes, keyboard navigation, help overlay, query
results, and cluster display — everything except a live `juju db-debug`
connection to a real controller.

## Key Architecture Decisions

| Decision | Detail |
|----------|--------|
| **Read-only by design** | SQL keyword whitelist + `PRAGMA query_only = ON` on every transaction |
| **Cluster introspection** | Phase 00 defines its own `ClusterIntrospector` interface |
| **Request IDs** | 8 hex chars from `crypto/rand` |
| **Row limits** | Default 100, max 1000 |
| **Pre-selection** | `--database` flag selects matching database after initial load |
| **Protocol version** | `"v1"` — handshake on connect |
| **TUI composition** | `dqliteModel` implements `bubbletea.Model`, embeddable as tab |
| **Mock-first TUI** | TUI built and testable with `mockDqliteAPI` from day 1 |
| **DBGetter** | `func(namespace string) (database.TxnRunner, error)` — local type |
| **Wiring** | `dqliteDBGetter` field on `sharedServerConfig` |

## Wire Protocol

All messages over `/dqlite` carry `"version": "v1"`.

| `type` | Purpose | Key fields |
|--------|---------|------------|
| `"databases"` | List databases | none |
| `"objects"` | List tables/views/triggers | `namespace`, `kind` |
| `"ddl"` | Get CREATE statement | `namespace`, `name` |
| `"query"` | Execute read-only query | `namespace`, `sql`, `limit` |
| `"cluster"` | Describe cluster topology | none |

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| `dbreplaccessor.NodeInfo` not importable from apiserver | Medium | Low | Pre-defined `clusterNodeInfo` struct + adapter |
| Semicolons in SQL strings cause false multi-statement rejection | Medium | Low | Single-quote scanning in split logic |
| PRAGMA whitelist too restrictive | Low | Medium | Accepted set covers 5 introspection pragmas |
| `base.StreamConnector` vs `ControllerStreamConnector` mismatch in spec | High | Medium | Phase 01 uses `base.ControllerStreamConnector`; see plan/phase-01.md §R1 |
| TUI mock-first approach diverges from spec in order but not in outcome | None | None | This is a scheduling decision, not a spec deviation |

## Total Estimated Effort

**Sequential** (single developer, original spec): 5-8 days

**Parallel** (two developers): 3-4 calendar days

| Phase | Dev 1 (Backend) | Dev 2 (TUI) |
|-------|-----------------|-------------|
| Day 1 | 00.1 SQL policy + 00.2 value formatter | 02.1 types + 02.2 model + 02.3 messages |
| Day 2 | 00.3 dispatch + 00.4 route wiring | 02.4 keyboard + 02.5 rendering |
| Day 3 | 00.5 tests → 01.1 types → 01.2 methods | 02.5 mock tests → **EARLY DEMO** |
| Day 4 | 01.3 tests → 02.6 wire together | 02.6 integration test |

## Files to Read First

- `apiserver/debuglog.go` — WebSocket handler pattern (auth after upgrade, `websocket.Serve`)
- `apiserver/apiserver.go:700-1046` — Handler slice structure, `monitoredHandler`
- `apiserver/shared.go:90-113` — `sharedServerConfig` struct
- `apiserver/apiserver.go:379-401` — `newServer` shared config construction
- `api/base/caller.go:110-132` — `StreamConnector`, `ControllerStreamConnector`
- `api/common/logs.go:135-177` — `StreamDebugLog` wrapping `ConnectControllerStream`
- `cmd/juju/commands/debuglog.go` — Command structure, TTY check
- `cmd/juju/commands/main.go:331-374` — `registerCommands`
- `cmd/modelcmd/controller.go:106-121,257-290` — `ControllerCommandBase`, `NewAPIRoot`

## Reviewer Feedback

Reviewer writes to `specs/debug-db/impl-plan-agent.md`. After writing each
plan phase file, check that file and incorporate feedback before writing
the next.
