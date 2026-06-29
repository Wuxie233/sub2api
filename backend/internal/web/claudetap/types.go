package claudetap

import "encoding/json"

type TraceRecord struct {
	Timestamp       string         `json:"timestamp"`
	RequestID       string         `json:"request_id"`
	Turn            int            `json:"turn"`
	DurationMs      int64          `json:"duration_ms"`
	Request         TraceRequest   `json:"request"`
	Response        TraceResponse  `json:"response"`
	UpstreamBaseURL string         `json:"upstream_base_url,omitempty"`
	Capture         map[string]any `json:"capture,omitempty"`
}

type TraceRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

type TraceResponse struct {
	Status    int               `json:"status"`
	Headers   map[string]string `json:"headers"`
	Body      json.RawMessage   `json:"body"`
	SseEvents []json.RawMessage `json:"sse_events,omitempty"`
}
