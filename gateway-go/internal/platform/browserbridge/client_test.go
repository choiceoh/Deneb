package browserbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func stubBridge(t *testing.T) *httptest.Server {
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
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestApprovalEnrichReturnsBodyWhenHubReady(t *testing.T) {
	srv := stubBridge(t)
	got := New(srv.URL, "secret").ApprovalEnrich(context.Background(), "https://tsgw.topsolar.kr", "아마란스10", "종류: 전자결재\n제목: 출장 신청")
	if got != "clicked login" {
		t.Fatalf("enrich = %q, want clicked login body", got)
	}
}

func TestApprovalEnrichEmptyWhenUnconfiguredOrDown(t *testing.T) {
	if got := New("", "").ApprovalEnrich(context.Background(), "https://tsgw.topsolar.kr", "아마란스10", "결재"); got != "" {
		t.Fatalf("empty URL should skip, got %q", got)
	}
	if got := New("http://127.0.0.1:1", "secret").ApprovalEnrich(context.Background(), "https://tsgw.topsolar.kr", "아마란스10", "결재"); got != "" {
		t.Fatalf("unreachable bridge should skip, got %q", got)
	}
}

func TestBuildApprovalReadTaskMentionsSiteAndReadOnly(t *testing.T) {
	task := buildApprovalReadTask("https://tsgw.topsolar.kr", "아마란스10", "종류: 전자결재\n제목: 휴가")
	if !strings.Contains(task, "https://tsgw.topsolar.kr") {
		t.Fatalf("task missing groupware URL: %q", task)
	}
	if !strings.Contains(task, "읽기만") || !strings.Contains(task, "휴가") {
		t.Fatalf("task missing read-only / title cues: %q", task)
	}
}
