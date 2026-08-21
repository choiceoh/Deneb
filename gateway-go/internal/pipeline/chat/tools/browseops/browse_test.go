package browseops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestToolBrowseFormatsSidecarPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/browse" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p["url"] != "https://gw.example/approvals" {
			t.Errorf("url = %v", p["url"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "url": "https://gw.example/approvals", "title": "전자결재",
			"text": "대기 문서 2건", "truncated": false,
		})
	}))
	defer srv.Close()
	t.Setenv("DENEB_BROWSE_URL", srv.URL)

	out, err := ToolBrowse()(context.Background(), []byte(`{"url":"https://gw.example/approvals"}`))
	if err != nil {
		t.Fatalf("ToolBrowse: %v", err)
	}
	if !strings.Contains(out, "[전자결재] https://gw.example/approvals") || !strings.Contains(out, "대기 문서 2건") {
		t.Fatalf("output = %q", out)
	}
}

func TestToolBrowseSidecarDownDegradesToGuidance(t *testing.T) {
	t.Setenv("DENEB_BROWSE_URL", "http://127.0.0.1:1") // refused instantly
	out, err := ToolBrowse()(context.Background(), []byte(`{"url":"https://example.com"}`))
	if err != nil {
		t.Fatalf("sidecar-down must degrade to guidance, got error %v", err)
	}
	if !strings.Contains(out, "사이드카") {
		t.Fatalf("guidance missing: %q", out)
	}
}

func TestToolBrowseRejectsNonHTTPAndSurfacesSidecarError(t *testing.T) {
	if _, err := ToolBrowse()(context.Background(), []byte(`{"url":"file:///etc/passwd"}`)); err == nil {
		t.Fatal("non-http url must be rejected")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "url must be http(s)"})
	}))
	defer srv.Close()
	t.Setenv("DENEB_BROWSE_URL", srv.URL)
	if _, err := ToolBrowse()(context.Background(), []byte(`{"url":"https://example.com"}`)); err == nil ||
		!strings.Contains(err.Error(), "url must be http(s)") {
		t.Fatalf("sidecar error must surface, got %v", err)
	}
}

// Live gate (skipped by default): drives the REAL resident sidecar end to end.
// Run: DENEB_BROWSE_LIVE=1 go test -run TestToolBrowse_Live ./internal/pipeline/chat/tools
func TestToolBrowse_Live(t *testing.T) {
	if os.Getenv("DENEB_BROWSE_LIVE") == "" {
		t.Skip("set DENEB_BROWSE_LIVE=1 with the sidecar running to exercise the real browser")
	}
	out, err := ToolBrowse()(context.Background(), []byte(`{"url":"https://example.com"}`))
	if err != nil {
		t.Fatalf("ToolBrowse live: %v", err)
	}
	if !strings.Contains(out, "Example Domain") {
		t.Fatalf("unexpected page text: %.200s", out)
	}
	t.Logf("live page head: %.120s", out)
}

// Targeted extraction (2026-08-02): a selector-scoped read must pass the
// selector through, label the output as partial, and report zero matches as a
// statement — never silently fall back to the whole page, which would tell the
// model the page HAS no such node while feeding it everything else.

func TestToolBrowseSelectorPassthroughAndPartialLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p["selector"] != "table" {
			t.Errorf("selector = %v", p["selector"])
		}
		matched := 2
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "url": "https://ex.com/pricing", "title": "Pricing",
			"text": "모델 | 입력 | 출력", "truncated": false, "matched": matched,
		})
	}))
	defer srv.Close()
	t.Setenv("DENEB_BROWSE_URL", srv.URL)

	out, err := ToolBrowse()(context.Background(), json.RawMessage(`{"url":"https://ex.com/pricing","selector":"table"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "셀렉터 일치 2건") {
		t.Errorf("a targeted read must say it is partial: %q", out)
	}
	if !strings.Contains(out, "모델 | 입력 | 출력") {
		t.Errorf("matched text must come through: %q", out)
	}
}

func TestToolBrowseSelectorZeroMatchesIsAStatementNotAFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		matched := 0
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "url": "https://ex.com", "title": "홈",
			"text": "", "truncated": false, "matched": matched,
		})
	}))
	defer srv.Close()
	t.Setenv("DENEB_BROWSE_URL", srv.URL)

	out, err := ToolBrowse()(context.Background(), json.RawMessage(`{"url":"https://ex.com","selector":".none"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "0건") || !strings.Contains(out, ".none") {
		t.Errorf("zero matches must be reported with the selector named: %q", out)
	}
}

func TestToolBrowseWithoutSelectorKeepsWholePageShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		if _, has := p["selector"]; has {
			t.Errorf("no selector param must be sent when unset: %v", p)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "url": "https://ex.com", "title": "홈", "text": "전체 본문", "truncated": false,
		})
	}))
	defer srv.Close()
	t.Setenv("DENEB_BROWSE_URL", srv.URL)

	out, err := ToolBrowse()(context.Background(), json.RawMessage(`{"url":"https://ex.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "셀렉터") {
		t.Errorf("an untargeted read must not claim partiality: %q", out)
	}
}
