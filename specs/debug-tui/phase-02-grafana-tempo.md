# Phase 02 — Show Spans: Grafana Tempo Client

## Goal

Replace mock trace data with real spans fetched from a Grafana Tempo HTTP
API, using the trace IDs from the transaction list. The Tempo URL is read
from the existing controller config key `open-telemetry-endpoint`.

## Dependencies

- **Phase 00** — the working TUI skeleton must exist.

## Memory Files to Read

Before writing any code, read:

- `specs/debug-tui/memory/phase-00.md` — file paths, model structure,
  deviations.

## Research Required

Before writing any code, read the following:

- `controller/config.go` — understand the `OpenTelemetryEndpoint` config
  key and how it is accessed via `Config.OpenTelemetryEndpoint()`.
- `api/client/controller.go` or equivalent — how controller config is
  fetched from the API (look for `ControllerConfig` or similar facade).
- `cmd/juju/debug/trace.go` — the existing mock `traceModel` from
  phase 00.
- Grafana Tempo HTTP API documentation: the `GET
  /api/traces/{traceID}` endpoint and its JSON response format (search
  for Tempo trace query docs online if needed).

## Scope

### 1. Define `TempoAPI` interface in `cmd/juju/debug/api.go`

```go
type TempoAPI interface {
    FetchTrace(ctx context.Context, traceID string) (*TraceData, error)
    IsConfigured() bool
}

type TraceData struct {
    TraceID string
    Spans   []SpanEntry
}

type SpanEntry struct {
    SpanID     string
    Operation  string
    Service    string
    Duration   string
    ParentID   string
}
```

`IsConfigured` returns `true` only when the `open-telemetry-endpoint`
config key has a non-empty value.

### 2. Implement `TempoClient` in `cmd/juju/debug/tempo.go`

```go
type TempoClient struct {
    baseURL    string
    httpClient *http.Client
}
```

- `NewTempoClient(endpoint string) *TempoClient` constructs the client.
  The `endpoint` value comes from `Config.OpenTelemetryEndpoint()`.
- `IsConfigured()` returns `endpoint != ""`.
- `FetchTrace(ctx, traceID)` sends `GET {baseURL}/api/traces/{traceID}`,
  parses the JSON response, and returns a `*TraceData`. The exact
  response structure depends on the Tempo API version; parse
  `data.spans` and map each to a `SpanEntry`.
- If the HTTP request fails or the trace is not found (404), return an
  error with a clear message.

### 3. Define `ControllerConfigAPI` interface in `cmd/juju/debug/api.go`

```go
type ControllerConfigAPI interface {
    ControllerConfig(ctx context.Context) (controller.Config, error)
    Close() error
}
```

### 4. Implement concrete `ControllerConfigAPI` client

Wrap the existing API client that fetches controller config (look for
`ControllerConfig` facade in `api/client/`).

### 5. Update `traceModel` in `cmd/juju/debug/trace.go`

Replace the static mock spans with real Tempo data:

- Add `tempoAPI TempoAPI` and `traceCache map[string]*TraceData` fields.
- When a `selectTxnMsg` arrives with a transaction that has a non-empty
  trace ID:
  - If the trace ID is in `traceCache`, render the cached spans.
  - Otherwise, start an async fetch by returning a
    `tea.Batch([]tea.Cmd{fetchTraceCmd})` command. While fetching,
    render a `bubbles/spinner` in the pane.
  - When the `fetchTraceCmd` completes, store the result in
    `traceCache` and render the spans.
- If `tempoAPI.IsConfigured()` is `false`, render the trace ID and
  span ID from the transaction entry, plus a message:
  `"Tempo not configured. Set open-telemetry-endpoint in controller config."`
- If `FetchTrace` returns an error, render the trace ID and span ID
  plus the error message.
- Span rendering: indent each span according to its parent-child
  depth, show `→ <service>:<operation>  <duration>`.
- When no transaction is selected, keep the existing placeholder.

### 6. Update `debugCommand.Run()` in `cmd/juju/debug/command.go`

- Create the `ControllerConfigAPI` client.
- Call `ControllerConfig(ctx)` to get the config.
- If `OpenTelemetryEndpoint` is non-empty, create a `TempoClient` and
  pass it into `newDebugModel()`.
- If empty, pass a `nil` or no-op `TempoAPI` so `traceModel` knows
  Tempo is not configured.
- Close both clients on exit.

### 7. Remove mock trace data

Delete the hardcoded mock span entries from `traceModel`. The initial
state shows the placeholder message until a transaction is selected.

## Memory File

On completion, write `specs/debug-tui/memory/phase-02.md` containing:

- The `TempoAPI` interface and `TempoClient` constructor signature.
- The exact Tempo API endpoint path used.
- The trace cache strategy (map key, eviction policy if any).
- How the "not configured" state is communicated to `traceModel`.
- All file paths modified.
- Any deviations from this phase spec and the reason.

## Acceptance Criteria

- Running `juju debug` against a controller with
  `open-telemetry-endpoint` set shows real spans in the Trace pane
  when a transaction with a trace ID is selected.
- If `open-telemetry-endpoint` is empty, the Trace pane shows the
  trace/span IDs plus a "not configured" message.
- If the Tempo HTTP request fails, the Trace pane shows the error
  alongside the trace/span IDs.
- A spinner is displayed while the trace is being fetched; the TUI
  never blocks on a network call.
- Re-selecting the same transaction uses the cache (no duplicate HTTP
  request).
