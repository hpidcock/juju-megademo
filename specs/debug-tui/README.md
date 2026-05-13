# Debug TUI — Phase Index

## Phase List

| # | File | Description | Depends on |
|---|------|-------------|------------|
| 00 | [phase-00-working-tui.md](phase-00-working-tui.md) | Working `juju debug` command with bubbletea TUI, mock data, no API calls | — |
| 01 | [phase-01-debug-logs.md](phase-01-debug-logs.md) | Replace mock log pane with live stream from the Logger facade | 00 |
| 02 | [phase-02-grafana-tempo.md](phase-02-grafana-tempo.md) | Replace mock trace pane with real spans from Grafana Tempo | 00 |
| 03 | [phase-03-model-switching.md](phase-03-model-switching.md) | Support `--all` / `--controller` flags, multi-database tab switching | 00 |
| 04 | [phase-04-database-access.md](phase-04-database-access.md) | Replace mock transaction list with real data via DebugChangeStream facade | 01, 02, 03 |
| 05 | [phase-05-workers-popup.md](phase-05-workers-popup.md) | Popup window showing workers that reacted to a selected transaction | 04 |

## Dependency Graph

```
00 (working TUI with mocks)
 │
 ├── 01 (debug logs)     02 (Grafana Tempo)     03 (model switching)
 │                         │                       │
 └─────────────────────────┼───────────────────────┘
                           ▼
                     04 (database access for txn list)
                           │
                           ▼
                     05 (workers popup)
```

## Parallel Work Opportunities

- **Phases 01, 02, and 03** may begin simultaneously once phase 00 is
  complete. They are independent of each other.
- **Phase 04** requires all of 01, 02, and 03 to be complete so the full
  API surface is available.
- **Phase 05** requires phase 04.

## Suggested Execution Order

1. Phase 00 (must be first).
2. Phases 01, 02, and 03 in parallel.
3. Phase 04 (needs 01, 02, 03).
4. Phase 05 (needs 04).
