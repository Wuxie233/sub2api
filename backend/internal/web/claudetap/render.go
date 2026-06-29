package claudetap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

const (
	viewerVersion              = "0.1.124"
	styleTemplateAnchor        = "<!-- CLAUDE_TAP_VIEWER_STYLE -->"
	scriptTemplateAnchor       = "<!-- CLAUDE_TAP_VIEWER_SCRIPT -->"
	mainScriptAnchor           = "<script>\nconst $ = s =>"
	traceJSONLPathAssignment   = `const __TRACE_JSONL_PATH__ = "";`
	traceHTMLPathAssignment    = `const __TRACE_HTML_PATH__ = "";`
	cspMetaTag                 = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'">`
	viewerI18NVariablePrefix   = "const __CLAUDE_TAP_I18N__ = "
	embeddedTraceDataPrefix    = "const EMBEDDED_TRACE_DATA = [\n"
	scriptClosingEscapePattern = "</"
	scriptClosingEscapeValue   = "<\\/"
)

var viewerJSPaths = [...]string{
	"viewer_assets/state.js",
	"viewer_assets/responses.js",
	"viewer_assets/lazy_loading.js",
	"viewer_assets/i18n_ui.js",
	"viewer_assets/live_bootstrap.js",
	"viewer_assets/filters_search.js",
	"viewer_assets/sidebar.js",
	"viewer_assets/detail_trace.js",
	"viewer_assets/renderers.js",
	"viewer_assets/sections_json.js",
	"viewer_assets/diff.js",
	"viewer_assets/utilities_mobile.js",
}

func RenderViewer(records []TraceRecord) ([]byte, error) {
	html, err := readViewerTemplate()
	if err != nil {
		return nil, err
	}

	dataJS, err := buildDataScript(records)
	if err != nil {
		return nil, err
	}

	html = strings.Replace(html, mainScriptAnchor, "<script>\n"+dataJS+"</script>\n"+mainScriptAnchor, 1)
	return []byte(html), nil
}

func readViewerTemplate() (string, error) {
	templateBytes, err := Assets.ReadFile("viewer.html")
	if err != nil {
		return "", fmt.Errorf("read viewer template: %w", err)
	}

	html := string(templateBytes)
	if !strings.Contains(html, styleTemplateAnchor) {
		return "", fmt.Errorf("viewer.html is missing the style asset anchor")
	}
	if !strings.Contains(html, scriptTemplateAnchor) {
		return "", fmt.Errorf("viewer.html is missing the script asset anchor")
	}

	cssBytes, err := Assets.ReadFile("viewer_assets/viewer.css")
	if err != nil {
		return "", fmt.Errorf("read viewer css: %w", err)
	}
	css := trimRightWhitespace(string(cssBytes))

	js, err := readViewerJS()
	if err != nil {
		return "", err
	}

	i18nScript, err := buildI18NScript()
	if err != nil {
		return "", err
	}

	styleBlock := cspMetaTag + "\n<style>\n" + css + "\n</style>"
	html = strings.Replace(html, styleTemplateAnchor, styleBlock, 1)
	if strings.Count(html, cspMetaTag) != 1 {
		return "", fmt.Errorf("viewer template must contain exactly one inline CSP meta tag")
	}

	scriptBlock := "<script>\n" + i18nScript + "</script>\n<script>\n" + js + "\n</script>"
	html = strings.Replace(html, scriptTemplateAnchor, scriptBlock, 1)
	if !strings.Contains(html, mainScriptAnchor) {
		return "", fmt.Errorf("viewer asset script is missing the main script anchor")
	}

	return html, nil
}

func readViewerJS() (string, error) {
	var builder strings.Builder
	for _, path := range viewerJSPaths {
		contents, err := Assets.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read viewer js %s: %w", path, err)
		}
		builder.Write(contents)
	}
	return trimRightWhitespace(builder.String()), nil
}

func buildI18NScript() (string, error) {
	contents, err := Assets.ReadFile("viewer_i18n.json")
	if err != nil {
		return "", fmt.Errorf("read viewer i18n: %w", err)
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal(contents, &payload); err != nil {
		return "", fmt.Errorf("parse viewer i18n: %w", err)
	}

	encoded, err := marshalCompactNoEscape(payload)
	if err != nil {
		return "", fmt.Errorf("encode viewer i18n: %w", err)
	}

	return viewerI18NVariablePrefix + string(encoded) + ";\n", nil
}

func buildDataScript(records []TraceRecord) (string, error) {
	encodedRecords := make([]string, 0, len(records))
	for index, record := range records {
		encoded, err := marshalCompactNoEscape(record)
		if err != nil {
			return "", fmt.Errorf("encode trace record %d: %w", index, err)
		}
		escaped := strings.ReplaceAll(string(encoded), scriptClosingEscapePattern, scriptClosingEscapeValue)
		encodedRecords = append(encodedRecords, escaped)
	}

	return embeddedTraceDataPrefix +
		strings.Join(encodedRecords, ",\n") +
		"\n];\n" +
		traceJSONLPathAssignment + "\n" +
		traceHTMLPathAssignment + "\n" +
		`const __CLAUDE_TAP_VERSION__ = "` + viewerVersion + `";` + "\n", nil
}

func marshalCompactNoEscape(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func trimRightWhitespace(value string) string {
	return strings.TrimRightFunc(value, unicode.IsSpace)
}
