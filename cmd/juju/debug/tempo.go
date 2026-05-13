// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package debug

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TempoClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewTempoClient(endpoint string) *TempoClient {
	host := strings.TrimRight(endpoint, "/")
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	host = replacePort(host, 4317, 3200)
	return &TempoClient{
		baseURL: host,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func replacePort(u string, oldPort, newPort int) string {
	i := strings.LastIndex(u, ":")
	if i < 0 {
		return u
	}
	port, err := strconv.Atoi(u[i+1:])
	if err != nil || port != oldPort {
		return u
	}
	return u[:i+1] + strconv.Itoa(newPort)
}

func (c *TempoClient) IsConfigured() bool {
	return c.baseURL != ""
}

func (c *TempoClient) FetchTrace(ctx context.Context, traceID string) (*TraceData, error) {
	url := fmt.Sprintf("%s/api/traces/%s", c.baseURL, traceID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching trace %s: %w", traceID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("trace %s not found", traceID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tempo returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return parseTempoResponse(traceID, body)
}

type tempoResponse struct {
	Batches []tempoResourceSpan `json:"batches"`
}

type tempoResourceSpan struct {
	ScopeSpans                  []tempoScopeSpan `json:"scopeSpans"`
	InstrumentationLibrarySpans []tempoScopeSpan `json:"instrumentationLibrarySpans"`
	Resource                    *tempoResource   `json:"resource,omitempty"`
}

func (rs *tempoResourceSpan) allScopeSpans() []tempoScopeSpan {
	if len(rs.ScopeSpans) > 0 {
		return rs.ScopeSpans
	}
	return rs.InstrumentationLibrarySpans
}

type tempoResource struct {
	Attributes []tempoKeyValue `json:"attributes,omitempty"`
}

type tempoScopeSpan struct {
	Scope *tempoScope `json:"scope,omitempty"`
	Spans []tempoSpan `json:"spans"`
}

type tempoScope struct {
	Name string `json:"name"`
}

type tempoSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano"`
	Attributes        []tempoKeyValue `json:"attributes,omitempty"`
}

type tempoKeyValue struct {
	Key   string       `json:"key"`
	Value tempoAnyValue `json:"value"`
}

type tempoAnyValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *int64  `json:"intValue,omitempty"`
}

func parseTempoResponse(traceID string, body []byte) (*TraceData, error) {
	var resp tempoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing tempo response: %w", err)
	}

	var spans []SpanEntry
	for _, rs := range resp.Batches {
		serviceName := extractServiceName(rs.Resource)
		for _, ss := range rs.allScopeSpans() {
			for _, span := range ss.Spans {
				duration := computeDuration(span.StartTimeUnixNano, span.EndTimeUnixNano)
				operation := span.Name
				if operation == "" && ss.Scope != nil {
					operation = ss.Scope.Name
				}
				spans = append(spans, SpanEntry{
					SpanID:    base64ToHex(span.SpanID),
					Operation: operation,
					Service:   serviceName,
					Duration:  duration,
					ParentID:  base64ToHex(span.ParentSpanID),
					startNano: parseNano(span.StartTimeUnixNano),
				})
			}
		}
	}

	sort.Slice(spans, func(i, j int) bool {
		return spans[i].startNano < spans[j].startNano
	})

	return &TraceData{
		TraceID: traceID,
		Spans:   spans,
	}, nil
}

func extractServiceName(res *tempoResource) string {
	if res == nil {
		return ""
	}
	for _, attr := range res.Attributes {
		if attr.Key == "service.name" && attr.Value.StringValue != nil {
			return *attr.Value.StringValue
		}
	}
	return ""
}

func parseNano(s string) int64 {
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v
}

func base64ToHex(b64 string) string {
	if b64 == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return b64
	}
	return fmt.Sprintf("%x", raw)
}

func computeDuration(startNano, endNano string) string {
	if startNano == "" || endNano == "" {
		return "?"
	}
	var start, end int64
	if _, err := fmt.Sscanf(startNano, "%d", &start); err != nil {
		return "?"
	}
	if _, err := fmt.Sscanf(endNano, "%d", &end); err != nil {
		return "?"
	}
	diff := end - start
	if diff < 0 {
		return "?"
	}
	ms := float64(diff) / 1_000_000.0
	switch {
	case ms < 1:
		return fmt.Sprintf("%.1fµs", float64(diff)/1_000.0)
	case ms < 1000:
		return fmt.Sprintf("%.1fms", ms)
	default:
		return fmt.Sprintf("%.1fs", ms/1000.0)
	}
}
