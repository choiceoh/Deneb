package evenapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

type stubChat struct {
	ready bool
	delay time.Duration
	text  string
	err   error
	calls atomic.Int32
	last  chatport.SyncRequest
}

func (s *stubChat) ChatReady() bool { return s.ready }

func (s *stubChat) RunSync(ctx context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
	s.calls.Add(1)
	s.last = req
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return &chatport.SyncResult{Text: s.text, BestText: s.text, Model: "test-model"}, nil
}

func TestChatCompletionsHappyPath(t *testing.T) {
	chat := &stubChat{ready: true, text: "## 답\n**짧게** https://x.test"}
	h := New(Config{Chat: chat, Token: "secret-token", Now: func() time.Time {
		return time.Unix(1700000000, 0)
	}})
	body := `{"model":"openclaw","messages":[{"role":"user","content":"다음 일정?"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp chatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Object != "chat.completion" || len(resp.Choices) != 1 {
		t.Fatalf("bad shape: %+v", resp)
	}
	content := resp.Choices[0].Message.Content
	if strings.Contains(content, "**") || strings.Contains(content, "https://") {
		t.Fatalf("not cleaned: %q", content)
	}
	if chat.last.SessionKey != "glasses:main" {
		t.Fatalf("session=%q", chat.last.SessionKey)
	}
	if chat.last.SystemPrompt == "" {
		t.Fatal("expected glasses system hint")
	}
	if chat.last.SoftDeadline != chatport.InteractiveTurnSoftDeadline {
		t.Fatalf("soft deadline=%s want %s", chat.last.SoftDeadline, chatport.InteractiveTurnSoftDeadline)
	}
}

func TestChatCompletionsUnauthorizedAndDisabled(t *testing.T) {
	chat := &stubChat{ready: true, text: "ok"}
	h := New(Config{Chat: chat, Token: "secret"})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}

	t.Setenv(EnvBridgeToken, "")
	off := New(Config{Chat: chat, Token: ""})
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	req2.Header.Set("Authorization", "Bearer x")
	off.ChatCompletions(rec2, req2)
	if rec2.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 disabled, got %d", rec2.Code)
	}
}

func TestChatCompletionsAckOnDeadline(t *testing.T) {
	chat := &stubChat{ready: true, text: "늦은 답", delay: 200 * time.Millisecond}
	h := New(Config{
		Chat:             chat,
		Token:            "secret",
		ResponseDeadline: 20 * time.Millisecond,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"긴 작업"}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp chatCompletionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Choices[0].Message.Content != ackLongRunning {
		t.Fatalf("want ack, got %q", resp.Choices[0].Message.Content)
	}
}

func TestChatCompletionsBlocksLongFollowUpDuringActiveTurn(t *testing.T) {
	chat := &stubChat{ready: true, text: "늦은 답", delay: 200 * time.Millisecond}
	h := New(Config{
		Chat:             chat,
		Token:            "secret",
		ResponseDeadline: 20 * time.Millisecond,
	})
	long := strings.Repeat("가", steerMaxRunes+1)
	first := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"`+long+`"}]}`))
	first.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, first)
	if rec.Code != http.StatusOK {
		t.Fatalf("first status=%d", rec.Code)
	}
	if chat.calls.Load() != 1 {
		t.Fatalf("first calls=%d want 1", chat.calls.Load())
	}

	secondLong := long + " 다른"
	second := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"`+secondLong+`"}]}`))
	second.Header.Set("Authorization", "Bearer secret")
	rec2 := httptest.NewRecorder()
	h.ChatCompletions(rec2, second)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second status=%d", rec2.Code)
	}
	if chat.calls.Load() != 1 {
		t.Fatalf("second calls=%d want 1 (long follow-up must not start parallel run)", chat.calls.Load())
	}
}

func TestChatCompletionsDedupe(t *testing.T) {
	chat := &stubChat{ready: true, text: "한번만"}
	h := New(Config{Chat: chat, Token: "secret"})
	body := `{"messages":[{"role":"user","content":"중복?"}]}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		h.ChatCompletions(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d", rec.Code)
		}
	}
	if chat.calls.Load() != 1 {
		t.Fatalf("calls=%d want 1", chat.calls.Load())
	}
}

func TestChatCompletionsDedupeUpgradesAckAfterBackgroundTurn(t *testing.T) {
	// First POST hits the deadline ack; Even retries while the detached turn is
	// still running or just finished. Dedupe must serve the real answer once the
	// background RunSync completes — not the stale ack forever.
	chat := &stubChat{ready: true, text: "실제 답변", delay: 80 * time.Millisecond}
	h := New(Config{
		Chat:             chat,
		Token:            "secret",
		ResponseDeadline: 20 * time.Millisecond,
	})
	body := `{"messages":[{"role":"user","content":"긴 작업"}]}`

	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req1.Header.Set("Authorization", "Bearer secret")
	rec1 := httptest.NewRecorder()
	h.ChatCompletions(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first status=%d", rec1.Code)
	}
	var first chatCompletionResponse
	_ = json.Unmarshal(rec1.Body.Bytes(), &first)
	if first.Choices[0].Message.Content != ackLongRunning {
		t.Fatalf("first want ack, got %q", first.Choices[0].Message.Content)
	}

	// Wait for the detached turn to finish and upgrade the dedupe cache.
	time.Sleep(120 * time.Millisecond)

	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req2.Header.Set("Authorization", "Bearer secret")
	rec2 := httptest.NewRecorder()
	h.ChatCompletions(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("retry status=%d", rec2.Code)
	}
	var second chatCompletionResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &second)
	if second.Choices[0].Message.Content != "실제 답변" {
		t.Fatalf("retry want real answer, got %q", second.Choices[0].Message.Content)
	}
	if chat.calls.Load() != 1 {
		t.Fatalf("calls=%d want 1 (dedupe must not re-run chat)", chat.calls.Load())
	}
}

func TestParseBearer(t *testing.T) {
	if got := ParseBearer("Bearer abc"); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := ParseBearer("bearer xyz"); got != "xyz" {
		t.Fatalf("got %q", got)
	}
	if got := ParseBearer("Token abc"); got != "" {
		t.Fatalf("got %q", got)
	}
}
