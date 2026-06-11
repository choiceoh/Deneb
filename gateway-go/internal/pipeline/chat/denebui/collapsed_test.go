package denebui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCollapsedReportFence(t *testing.T) {
	t.Run("wraps title and body in a valid accordion fence", func(t *testing.T) {
		body := "## 분석\n- **중요도**: 높음\n\n```go\nfmt.Println(\"code inside\")\n```\n끝."
		got := CollapsedReportFence("📬 탑솔라 견적 요청", body)

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

		var root struct {
			Type     string `json:"type"`
			Title    string `json:"title"`
			Children []struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"children"`
		}
		if err := json.Unmarshal([]byte(fences[0]), &root); err != nil {
			t.Fatalf("fence body is not JSON: %v", err)
		}
		if root.Type != "accordion" || root.Title != "📬 탑솔라 견적 요청" {
			t.Errorf("unexpected root: %+v", root)
		}
		if len(root.Children) != 1 || root.Children[0].Type != "markdown" || root.Children[0].Value != body {
			t.Errorf("body not preserved verbatim: %+v", root.Children)
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

func TestValidate_MarkdownNode(t *testing.T) {
	issues, err := Validate(`{"type":"markdown","value":"## 제목\n본문"}`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(issues) > 0 {
		t.Errorf("markdown node should be a known type, issues=%v", issues)
	}
}
