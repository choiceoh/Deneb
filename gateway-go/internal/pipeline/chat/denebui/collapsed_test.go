package denebui

import (
	"strings"
	"testing"
)

func TestCollapsedReportFenceReturnsRawBodyAsFallback(t *testing.T) {
	t.Run("wraps title and body in a valid accordion fence", func(t *testing.T) {
		body := "## 분석\n- **중요도**: 높음\n\n```go\nfmt.Println(\"code inside\")\n```\n끝."
		got := CollapsedReportFence("📬 탑솔라 <견적> \"요청\"", body)

		fences := ExtractFences(got)
		if len(fences) != 1 {
			t.Fatalf("want exactly 1 deneb-ui fence, got %d:\n%s", len(fences), got)
		}
		// The embedded code fence must not terminate the outer fence early:
		// nothing may remain outside the fence.
		if rest := strings.TrimSpace(strings.ReplaceAll(got, "```deneb-ui\n"+fences[0]+"\n```", "")); rest != "" {
			t.Errorf("content leaked outside the fence: %q", rest)
		}
		if issues, err := Validate(fences[0]); err != nil || len(issues) > 0 {
			t.Fatalf("fence should validate, err=%v issues=%v", err, issues)
		}

		root := mustParseHTML(t, fences[0])
		if root["type"] != "accordion" || root["title"] != `📬 탑솔라 <견적> "요청"` {
			t.Errorf("unexpected root: %v", root)
		}
		ch, _ := root["children"].([]any)
		if len(ch) != 1 {
			t.Fatalf("want 1 child, got %v", ch)
		}
		md := ch[0].(map[string]any)
		if md["type"] != "markdown" || md["value"] != body {
			t.Errorf("body not preserved verbatim through escape+decode round-trip:\n got %q\nwant %q", md["value"], body)
		}
	})

	t.Run("blank title or body falls back to raw body", func(t *testing.T) {
		if got := CollapsedReportFence("", "본문"); got != "본문" {
			t.Errorf("blank title: want raw body, got %q", got)
		}
		if got := CollapsedReportFence("  ", "본문"); got != "본문" {
			t.Errorf("whitespace title: want raw body, got %q", got)
		}
		if got := CollapsedReportFence("제목", "  "); got != "  " {
			t.Errorf("blank body: want raw body back, got %q", got)
		}
	})
}

func TestValidateAllowsLegacyJSONMarkdownNode(t *testing.T) {
	// Old transcripts carry JSON bodies; the strict legacy path must keep
	// accepting them (display compatibility), even though authoring is HTML.
	issues, err := Validate(`{"type":"markdown","value":"## 제목\n본문"}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(issues) > 0 {
		t.Errorf("markdown node should be a known type, issues=%v", issues)
	}
}
