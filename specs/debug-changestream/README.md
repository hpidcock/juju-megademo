# Debug Change Stream — Task Index

## Task List

| # | File | Description | Depends on |
|---|------|-------------|------------|
| 01 | [task-01-schema-migrations.md](task-01-schema-migrations.md) | Schema: edit changelog SQL files directly, add tables/columns/trigger | — |
| 02 | [task-02-core-interfaces.md](task-02-core-interfaces.md) | Core: `TracedChangeEvent`, `Term` txn range | — |
| 03 | [task-03-transaction-wrapper.md](task-03-transaction-wrapper.md) | DB txn hooks: trace ctx + txn_id stamp | 01 |
| 04 | [task-04-stream-txn-trace.md](task-04-stream-txn-trace.md) | Stream: query + `changeEvent` trace fields | 01, 02, 03 |
| 05 | [task-05-stream-debug-state.md](task-05-stream-debug-state.md) | Stream: pause/step state polling | 01, 04 |
| 06 | [task-06-eventmux-trace.md](task-06-eventmux-trace.md) | EventMux: propagate trace scope to subscribers | 02, 04 |
| 07 | [task-07-domain-debugchangestream.md](task-07-domain-debugchangestream.md) | Domain: `debugchangestream` service + state | 01, 05 |
| 08 | [task-08-api-facade.md](task-08-api-facade.md) | API facade: `DebugChangeStream` | 07 |
| 09 | [task-09-cli-commands.md](task-09-cli-commands.md) | CLI: `debug-pause`, `debug-step`, `debug-resume` | 08 |
| 10 | [task-10-watcher-trace-transport.md](task-10-watcher-trace-transport.md) | Watcher `ChangeContext` + RPC trace transport | 02, 06 |
| 11 | [task-11-watcher-consumer-spans.md](task-11-watcher-consumer-spans.md) | Worker consumer loops: `trace.Start` from `ChangeContext` | 10 |
| 12 | [task-12-core-trace-addlink.md](task-12-core-trace-addlink.md) | Extend `core/trace.Span` with `AddLink` for causal linking | — |

## Dependency Graph

```
01 (schema)   02 (core interfaces)   12 (trace AddLink)
    │                │                       │
    ▼                │                       │
03 (txn wrapper)     │                       │
    │                │                       │
    └────────┬────────┘                      │
             ▼                               │
       04 (stream query + trace fields)      │
             │                               │
     ┌───────┴───────┐                       │
     ▼               ▼                       │
05 (stream debug  06 (eventmux ◄─────────────┘
   state polling)    trace prop.)
     │                   │
     ▼                   ▼
07 (domain layer)   10 (watcher ChangeContext
     │                  + RPC trace transport)
     ▼                       │
08 (api facade)              ▼
     │                  11 (worker consumer
     ▼                      spans)
09 (cli commands)
```

## Parallel Work Opportunities

- **Tasks 01, 02, and 12** may start simultaneously.
- **Tasks 05 and 06** may run in parallel once task 04 is complete.
  Task 06 also requires task 12.
- **Tasks 07 and 10** may run in parallel once task 06 is complete.
- All other tasks are sequential within their chain.
- Task 10 has a large blast radius; use sub-agents for parallel
  mechanical watcher updates (see task 10 for details).

## Suggested Execution Order

1. Tasks 01, 02, and 12 in parallel.
2. Task 03 (needs 01).
3. Task 04 (needs 01, 02, 03).
4. Tasks 05 and 06 in parallel (05 needs 04; 06 needs 04 and 12).
5. Tasks 07 and 10 in parallel (07 needs 01, 05; 10 needs 02, 06).
6. Task 08 (needs 07); task 11 (needs 10). These may also run in parallel.
7. Task 09 (needs 08).
