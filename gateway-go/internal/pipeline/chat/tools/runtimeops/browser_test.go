package runtimeops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

func stubBrowser(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		_, _ = w.Write([]byte(`{"connected":true,"busy":false}`))
	})
	mux.HandleFunc("/v1/execute", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Task string `json:"task"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Task == "fail" {
			_, _ = w.Write([]byte(`{"success":false,"data":"element not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":"clicked login"}`))
	})
	mux.HandleFunc("/v1/stop", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func browserDepsFor(rawURL, token string) *tooldeps.BrowserDeps {
	return &tooldeps.BrowserDeps{
		BaseURL: func() string { return rawURL },
		Token:   func() string { return token },
	}
}

func runBrowser(t *testing.T, d *tooldeps.BrowserDeps, args map[string]any) string {
	t.Helper()
	in, _ := json.Marshal(args)
	out, err := ToolBrowser(d)(context.Background(), in)
	if err != nil {
		t.Fatalf("browser(%v): %v", args, err)
	}
	return out
}

func TestBrowserTool_ReturnsOffMessageWhenUnconfigured(t *testing.T) {
	out := runBrowser(t, &tooldeps.BrowserDeps{}, map[string]any{"action": "status"})
	if !strings.Contains(out, "꺼져") {
		t.Errorf("expected off message, got %q", out)
	}
}

func TestBrowserTool_StatusWhenConnected(t *testing.T) {
	out := runBrowser(t, browserDepsFor(stubBrowser(t).URL, "secret"), map[string]any{"action": "status"})
	if !strings.Contains(out, "연결됨") {
		t.Errorf("expected connected status, got %q", out)
	}
}

func TestBrowserTool_ExecuteSuccess(t *testing.T) {
	out := runBrowser(t, browserDepsFor(stubBrowser(t).URL, "secret"), map[string]any{
		"action": "execute",
		"task":   "Click the login button",
	})
	if !strings.Contains(out, "완료") || !strings.Contains(out, "clicked login") {
		t.Errorf("expected success result, got %q", out)
	}
}

func TestBrowserTool_ExecuteRequiresTask(t *testing.T) {
	out := runBrowser(t, browserDepsFor(stubBrowser(t).URL, "secret"), map[string]any{"action": "execute"})
	if !strings.Contains(out, "task") {
		t.Errorf("expected task-required message, got %q", out)
	}
}

func TestBrowserTool_ExecuteFailure(t *testing.T) {
	out := runBrowser(t, browserDepsFor(stubBrowser(t).URL, "secret"), map[string]any{
		"action": "execute",
		"task":   "fail",
	})
	if !strings.Contains(out, "실패") || !strings.Contains(out, "element not found") {
		t.Errorf("expected failure result, got %q", out)
	}
}

func TestBrowserTool_Stop(t *testing.T) {
	out := runBrowser(t, browserDepsFor(stubBrowser(t).URL, "secret"), map[string]any{"action": "stop"})
	if !strings.Contains(out, "중지") {
		t.Errorf("expected stop confirmation, got %q", out)
	}
}

func TestBrowserTool_Unauthorized(t *testing.T) {
	out := runBrowser(t, browserDepsFor(stubBrowser(t).URL, "wrong"), map[string]any{"action": "status"})
	if !strings.Contains(out, "인증") {
		t.Errorf("expected auth failure, got %q", out)
	}
}

func TestApprovalBrowserEnrich_ReturnsBodyWhenHubReady(t *testing.T) {
	srv := stubBrowser(t)
	got := ApprovalBrowserEnrich(context.Background(), srv.URL, "secret", "https://tsgw.topsolar.kr", "아마란스10", "종류: 전자결재\n제목: 출장 신청")
	if got != "clicked login" {
		t.Fatalf("enrich = %q, want clicked login body", got)
	}
}

func TestApprovalBrowserEnrich_EmptyWhenUnconfiguredOrDown(t *testing.T) {
	if got := ApprovalBrowserEnrich(context.Background(), "", "", "https://tsgw.topsolar.kr", "아마란스10", "결재"); got != "" {
		t.Fatalf("empty URL should skip, got %q", got)
	}
	if got := ApprovalBrowserEnrich(context.Background(), "http://127.0.0.1:1", "secret", "https://tsgw.topsolar.kr", "아마란스10", "결재"); got != "" {
		t.Fatalf("unreachable bridge should skip, got %q", got)
	}
}

func TestBuildApprovalReadTask_MentionsSiteAndReadOnly(t *testing.T) {
	task := buildApprovalReadTask("https://tsgw.topsolar.kr", "아마란스10", "종류: 전자결재\n제목: 휴가")
	if !strings.Contains(task, "https://tsgw.topsolar.kr") {
		t.Fatalf("task missing groupware URL: %q", task)
	}
	if !strings.Contains(task, "읽기만") || !strings.Contains(task, "휴가") {
		t.Fatalf("task missing read-only / title cues: %q", task)
	}
}
