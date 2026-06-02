package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

// parseSSEEvents splits an SSE body into (event, dataJSON) pairs, skipping
// comment (keepalive) lines. Mirrors the minimal parser the native client uses.
func parseSSEEvents(t *testing.T, body string) []struct{ Event, Data string } {
	t.Helper()
	var out []struct{ Event, Data string }
	var event string
	var data strings.Builder
	flush := func() {
		if event == "" && data.Len() == 0 {
			return
		}
		out = append(out, struct{ Event, Data string }{event, data.String()})
		event = ""
		data.Reset()
	}
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, ":"):
			// comment / keepalive — ignore
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case line == "":
			flush()
		}
	}
	flush()
	return out
}

func TestWriteChatStreamSSE_DeltasThenDone(t *testing.T) {
	rec := httptest.NewRecorder()
	run := func(_ context.Context, onDelta func(string)) (*chatStreamResult, error) {
		onDelta("안녕")
		onDelta("하세요")
		return &chatStreamResult{Text: "안녕하세요", Model: "step3p7", FellBack: true}, nil
	}
	writeChatStreamSSE(context.Background(), rec, "client:test", run, nil)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	events := parseSSEEvents(t, rec.Body.String())
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3 (2 delta + 1 done): %q", len(events), rec.Body.String())
	}
	if events[0].Event != "delta" || events[1].Event != "delta" {
		t.Errorf("first two events = %q/%q, want delta/delta", events[0].Event, events[1].Event)
	}
	var d0 struct {
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[0].Data), &d0); err != nil || d0.Delta != "안녕" {
		t.Errorf("delta[0] = %q (err %v), want 안녕", d0.Delta, err)
	}
	if events[2].Event != "done" {
		t.Fatalf("last event = %q, want done", events[2].Event)
	}
	var done struct {
		Text     string `json:"text"`
		Model    string `json:"model"`
		FellBack bool   `json:"fellBack"`
	}
	if err := json.Unmarshal([]byte(events[2].Data), &done); err != nil {
		t.Fatalf("done payload: %v", err)
	}
	if done.Text != "안녕하세요" || done.Model != "step3p7" || !done.FellBack {
		t.Errorf("done = %+v, want {안녕하세요 step3p7 true}", done)
	}
}

func TestWriteChatStreamSSE_ErrorFrame(t *testing.T) {
	rec := httptest.NewRecorder()
	run := func(_ context.Context, _ func(string)) (*chatStreamResult, error) {
		return nil, errors.New("boom")
	}
	writeChatStreamSSE(context.Background(), rec, "client:test", run, nil)

	events := parseSSEEvents(t, rec.Body.String())
	if len(events) != 1 || events[0].Event != "error" {
		t.Fatalf("events = %+v, want single error frame", events)
	}
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(events[0].Data), &e); err != nil || e.Error != "boom" {
		t.Errorf("error payload = %q (err %v), want boom", e.Error, err)
	}
}

// postMiniappChatStream drives the streaming handler with a client token.
func postMiniappChatStream(t *testing.T, s *Server, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/miniapp/chat/stream", bytes.NewReader(raw))
	req.Header.Set(clientauth.Header, token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleMiniappChatStream(rec, req)
	return rec
}

func TestHandleMiniappChatStream_GuardPaths(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatalf("generate client token: %v", err)
	}
	s := newTestServer(t)

	// Bad token → 401 (handled before any SSE bytes).
	rec := postMiniappChatStream(t, s, token+"x", map[string]any{"message": "hi"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token: code = %d, want 401", rec.Code)
	}

	// Empty message → 400.
	rec = postMiniappChatStream(t, s, token, map[string]any{"message": "   "})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty message: code = %d, want 400", rec.Code)
	}

	// Valid request but chat handler not wired → 503 (not a stream). Null it
	// explicitly so the guard is exercised without driving a real LLM turn.
	s.chatHandler = nil
	rec = postMiniappChatStream(t, s, token, map[string]any{"message": "hi"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil chat handler: code = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "chat handler not ready") {
		t.Errorf("nil chat handler: body = %q, want 'chat handler not ready'", rec.Body.String())
	}
}
