package claudetap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureHTMLPath = "/flyshop/opencode/tmp/claudetap-fixture.html"

func TestRenderViewerAnthropicFixture(t *testing.T) {
	record := TraceRecord{
		Timestamp:  "2026-05-13T13:20:00+00:00",
		RequestID:  "req_anthropic_contract",
		Turn:       1,
		DurationMs: 100,
		Request: TraceRequest{
			Method:  "POST",
			Path:    "/v1/messages",
			Headers: map[string]string{},
			Body: mustRawJSON(t, map[string]any{
				"model":  "claude-3-5-sonnet-20241022",
				"system": "Claude Code contract system prompt with SYSTEM_MARKER_CLTAP and closing marker </div>.",
				"messages": []any{
					map[string]any{
						"role": "user",
						"content": []any{
							map[string]any{
								"type": "text",
								"text": "Read pyproject.toml and mention MESSAGE_MARKER_CLTAP.",
							},
						},
					},
				},
				"tools": []any{
					map[string]any{
						"name":        "ContractReadTool",
						"description": "Read a file.",
						"input_schema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"file_path": map[string]any{"type": "string"},
							},
							"required": []string{"file_path"},
						},
					},
				},
			}),
		},
		Response: TraceResponse{
			Status:  200,
			Headers: map[string]string{},
			Body: mustRawJSON(t, map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": "Anthropic response OK."},
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_read",
						"name":  "ContractReadTool",
						"input": map[string]any{"file_path": "pyproject.toml"},
					},
				},
				"usage": map[string]any{"input_tokens": 120, "output_tokens": 9},
			}),
		},
	}

	rendered, err := RenderViewer([]TraceRecord{record})
	if err != nil {
		t.Fatalf("RenderViewer returned error: %v", err)
	}

	html := string(rendered)
	assertContains(t, html, "const EMBEDDED_TRACE_DATA = [")
	assertContains(t, html, cspMetaTag)
	assertContains(t, html, "<style>")
	assertContains(t, html, "const __CLAUDE_TAP_I18N__ = ")
	assertContains(t, html, "SYSTEM_MARKER_CLTAP")
	assertContains(t, html, "MESSAGE_MARKER_CLTAP")
	assertContains(t, html, "ContractReadTool")
	assertContains(t, html, `<\/div>`)
	assertNotContains(t, html, styleTemplateAnchor)
	assertNotContains(t, html, scriptTemplateAnchor)

	dataRegion := embeddedDataRegion(t, html)
	assertNotContains(t, dataRegion, "</script>")
	assertNotContains(t, dataRegion, `</div>`)

	if err := os.MkdirAll(filepath.Dir(fixtureHTMLPath), 0o755); err != nil {
		t.Fatalf("create fixture output directory: %v", err)
	}
	if err := os.WriteFile(fixtureHTMLPath, rendered, 0o644); err != nil {
		t.Fatalf("write fixture html: %v", err)
	}
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := marshalCompactNoEscape(value)
	if err != nil {
		t.Fatalf("marshal fixture JSON: %v", err)
	}
	return json.RawMessage(encoded)
}

func embeddedDataRegion(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, embeddedTraceDataPrefix)
	if start == -1 {
		t.Fatalf("rendered html is missing %q", embeddedTraceDataPrefix)
	}

	endMarker := "\n];\n" + traceJSONLPathAssignment
	end := strings.Index(html[start:], endMarker)
	if end == -1 {
		t.Fatalf("rendered html is missing embedded data terminator")
	}

	return html[start : start+end]
}

func assertContains(t *testing.T, value string, substring string) {
	t.Helper()
	if !strings.Contains(value, substring) {
		t.Fatalf("expected rendered html to contain %q", substring)
	}
}

func assertNotContains(t *testing.T, value string, substring string) {
	t.Helper()
	if strings.Contains(value, substring) {
		t.Fatalf("expected rendered html not to contain %q", substring)
	}
}
