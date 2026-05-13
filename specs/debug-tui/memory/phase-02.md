# Phase 02 — Memory File

## Interfaces and Constructor Signatures

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
    SpanID      string
    Operation   string
    Service     string
    Duration    string
    ParentID    string
    startNano   int64  // unexported, used for sorting
}

type ControllerConfigAPI interface {
    ControllerConfig(ctx context.Context) (controller.Config, error)
    Close() error
}

func NewTempoClient(endpoint string) *TempoClient
```

## Tempo API Endpoint

`GET {baseURL}/api/traces/{traceID}`

The `baseURL` is derived from the `open-telemetry-endpoint` controller
config value with the port replaced from 4317 (OTLP gRPC ingest) to 3200
(Tempo HTTP query API). A `http://` scheme is prepended if the endpoint
lacks one.

## Response Parsing

- Top-level key is `batches` (not `resourceSpans`).
- Both `scopeSpans` and `instrumentationLibrarySpans` are supported.
- Trace IDs, span IDs, and parent span IDs are base64-encoded in the
  Tempo JSON response and are converted to hex via `base64ToHex()`.

## Trace Cache Strategy

- Map key: trace ID string (hex format as used in the URL).
- No eviction policy; cache grows unboundedly for the TUI session
  lifetime, which is short and interactive.
- Re-selecting the same transaction uses the cache (no duplicate HTTP
  request).

## "Not Configured" State

Communicated via `tempoAPI` field on `traceModel`:
- If `tempoAPI == nil` (passed as nil from `command.go` when
  `open-telemetry-endpoint` is empty), `IsConfigured()` is never called;
  the nil check short-circuits to the not-configured path.
- If `tempoAPI.IsConfigured()` returns `false`, the not-configured
  message is shown alongside the trace/span IDs.

## File Paths Modified

- `cmd/juju/debug/api.go` — Added `TempoAPI`, `TraceData`, `SpanEntry`,
  `ControllerConfigAPI`, and `controllerConfigAPIClient`.
- `cmd/juju/debug/tempo.go` — New file. `TempoClient` with HTTP fetch,
  JSON parsing, base64→hex conversion, port replacement, duration
  formatting, span sorting by start time.
- `cmd/juju/debug/trace.go` — Replaced mock spans with async Tempo
  fetch, spinner, trace cache, scrollable viewport, header outside
  viewport, service name grouping.
- `cmd/juju/debug/messages.go` — Added `fetchTraceResultMsg`.
- `cmd/juju/debug/command.go` — Added controller config fetch and
  `TempoClient` creation; passes `TempoAPI` into `newDebugModel()`.
- `cmd/juju/debug/model.go` — Updated `newDebugModel()` signature to
  accept `TempoAPI`; wired `fetchTraceCmd` on transaction selection;
  trace pane gets remaining height after changestream (capped at 12
  lines) and log panes.
- `cmd/juju/debug/changestream.go` — Mock transactions now use
  specified trace IDs (`f0250316350d16f308b71ab93cbf7510`,
  `c23d861742e1815509564a7d176d3590`,
  `6aa4a72c364edbd109cbf40d3520c1ae`) for testability.
- `cmd/juju/debug/doc.go` — Added missing copyright header.

## Deviations from Spec

1. **Port replacement**: The spec says the Tempo URL comes from
   `open-telemetry-endpoint`, but that value points to the OTLP gRPC
   ingest port (4317), not the Tempo query API (3200). `NewTempoClient`
   automatically replaces port 4317 with 3200 as a workaround. A future
   dedicated `open-telemetry-query-endpoint` config key would be
   cleaner.

2. **Top-level JSON key**: The spec references `resourceSpans` but
   Tempo returns `batches` at the top level. The parser uses `batches`.

3. **Base64-encoded IDs**: The spec assumed hex-encoded trace/span IDs
   in the Tempo response, but Tempo returns base64-encoded IDs. Added
   `base64ToHex()` conversion.

4. **Service name display**: The spec says
   `→ <service>:<operation>  <duration>`. Changed to show service name
   inline in bold bright color only when it differs from the previous
   span's service, reducing visual noise.

5. **Changestream pane height**: Capped at 12 lines (spec used 40% of
   remaining height). Trace pane absorbs remaining space.

6. **Scrollable trace pane**: Added `bubbles/viewport` to the trace
   pane so spans don't overflow the terminal. Header (title + txn/trace
   IDs) stays fixed above the viewport.

7. **Span ordering**: Spans are sorted by `startTimeUnixNano` ascending
   (not specified in the original spec but added for readability).

8. **`SpanEntry.startNano`**: Added unexported `startNano int64` field
   to `SpanEntry` for sorting; not part of the original spec struct.
