# Task 02 — Memory

## Method signatures added

### `ChangeEvent` (in `core/changestream/change.go`)

```go
TraceID() string
SpanID() string
```

### `Term` (in `core/changestream/change.go`)

```go
TxnMinID() int64
TxnMaxID() int64
```

## Files modified

| File | Change |
|------|--------|
| `core/changestream/change.go` | Added `TraceID`/`SpanID` to `ChangeEvent`; added `TxnMinID`/`TxnMaxID` to `Term`; added `//go:generate` directive for mock |
| `core/watcher/eventsource/package_test.go` | Stub `changeEvent` |
| `domain/application/service/package_test.go` | Stub `*changeEvent` |
| `domain/lifewatcher_test.go` | Stub `changeEvent` |
| `domain/secret/service/service_test.go` | Stub `*changeEvent` |
| `internal/changestream/eventmultiplexer/package_test.go` | Stub `changeEvent` |
| `internal/changestream/eventmultiplexer/benchmarks_test.go` | Stub `term` |
| `internal/changestream/stream/stream.go` | Temporary stubs on `changeEvent` and `*Term` (task 04 will replace) |
| `core/changestream/mocks/changestream_mock.go` | Regenerated (adds `TraceID`/`SpanID` mock methods) |
| `internal/changestream/eventmultiplexer/change_mock_test.go` | Regenerated (adds `TxnMinID`/`TxnMaxID` mock methods) |

## Existing implementations found and updated

### `ChangeEvent` stubs (return `""`)

| File | Type | Receiver |
|------|------|----------|
| `core/watcher/eventsource/package_test.go` | `changeEvent` | value |
| `domain/application/service/package_test.go` | `changeEvent` | pointer |
| `domain/lifewatcher_test.go` | `changeEvent` | value |
| `domain/secret/service/service_test.go` | `changeEvent` | pointer |
| `internal/changestream/eventmultiplexer/package_test.go` | `changeEvent` | value |

### `Term` stubs (return `0`)

| File | Type | Receiver |
|------|------|----------|
| `internal/changestream/eventmultiplexer/benchmarks_test.go` | `term` | value |
| `internal/changestream/stream/stream.go` | `Term` | pointer (temporary stub) |

## `go generate` commands run

```
go generate ./core/changestream/...
go generate ./internal/changestream/eventmultiplexer/...
```

## Deviations from spec

- **`internal/changestream/stream/stream.go` was updated with stubs.**
  The spec says this file is covered by task 04, but the interface
  extension requires at least stub implementations for the build to
  compile. Stubs return `""` and `0`; task 04 should replace them
  with real DB-backed values.
- The `go generate` for the `Term` mock in
  `internal/changestream/eventmultiplexer/` is not mentioned in the
  spec's `go generate ./core/changestream/...` command, but it was
  also required to keep that package's generated mock in sync.
