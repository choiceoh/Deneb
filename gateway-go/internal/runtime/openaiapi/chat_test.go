package openaiapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// fakeStreamer yields a canned StreamEvent sequence then closes.
// captured holds the request the handler forwarded so tests can
// assert the OpenAI → internal translation.
type fakeStreamer struct {
	events   []llm.StreamEvent
	startErr error
	captured llm.ChatRequest
}

func (f *fakeStreamer) StreamChat(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	f.captured = req
	if f.startErr != nil {
		return nil, f.startErr
	}
	ch := make(chan llm.StreamEvent, len(f.events)+1)
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// must marshal helpers — clearer than scattered json.Marshal in tests.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func newChatStreamerFromText(text, stopReason string, inputTokens, outputTokens int) *fakeStreamer {
	return &fakeStreamer{events: []llm.StreamEvent{
		{Type: "message_start", Payload: mustJSONStart("msg_test", "claude-sonnet-4-6", inputTokens)},
		{Type: "content_block_start", Payload: mustJSONBlockStart(0, "text", "")},
		{Type: "content_block_delta", Payload: mustJSONTextDelta(0, text)},
		{Type: "content_block_stop", Payload: mustJSONBlockStop(0)},
		{Type: "message_delta", Payload: mustJSONMsgDelta(stopReason, outputTokens)},
		{Type: "message_stop"},
	}}
}

// stream payload helpers (panic on error since these are test-only literals).
func mustJSONStart(id, model string, inputTokens int) json.RawMessage {
	var p llm.MessageStart
	p.Message.ID = id
	p.Message.Model = model
	p.Message.Usage.InputTokens = inputTokens
	b, _ := json.Marshal(p)
	return b
}
func mustJSONBlockStart(idx int, blockType, text string) json.RawMessage {
	p := llm.ContentBlockStart{Index: idx, ContentBlock: llm.ContentBlock{Type: blockType, Text: text}}
	b, _ := json.Marshal(p)
	return b
}
func mustJSONTextDelta(idx int, text string) json.RawMessage {
	var p llm.ContentBlockDelta
	p.Index = idx
	p.Delta.Type = "text_delta"
	p.Delta.Text = text
	b, _ := json.Marshal(p)
	return b
}
func mustJSONBlockStop(idx int) json.RawMessage {
	b, _ := json.Marshal(llm.ContentBlockStop{Index: idx})
	return b
}
func mustJSONMsgDelta(stopReason string, outputTokens int) json.RawMessage {
	var p llm.MessageDelta
	p.Delta.StopReason = stopReason
	p.Usage.OutputTokens = outputTokens
	b, _ := json.Marshal(p)
	return b
}

func newChatTestMux(t *testing.T, streamer *fakeStreamer) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	Mount(mux, Deps{
		ModelRegistry: &fakeRegistry{models: map[modelrole.Role]string{
			modelrole.RoleMain:        "anthropic/claude-sonnet-4-6",
			modelrole.RoleLightweight: "anthropic/claude-haiku-4-5",
			modelrole.RoleFallback:    "anthropic/claude-haiku-4-5",
		}},
		ChatClient: func(_ modelrole.Role) ChatStreamer { return streamer },
	})
	return mux
}

func postChat(t *testing.T, mux *http.ServeMux, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw := mustJSON(t, body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestChatCompletions_RoundTripText(t *testing.T) {
	streamer := newChatStreamerFromText("Hello, Peter!", "end_turn", 12, 5)
	mux := newChatTestMux(t, streamer)

	rec := postChat(t, mux, map[string]any{
		"model": "deneb-main",
		"messages": []map[string]any{
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Say hello"},
		},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got chatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", got.Object)
	}
	if got.Model != "deneb-main" {
		t.Errorf("model = %q, want deneb-main (echoed from request)", got.Model)
	}
	if len(got.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(got.Choices))
	}
	if got.Choices[0].Message.Content != "Hello, Peter!" {
		t.Errorf("content = %q, want %q", got.Choices[0].Message.Content, "Hello, Peter!")
	}
	if got.Choices[0].Message.Role != "assistant" {
		t.Errorf("role = %q, want assistant", got.Choices[0].Message.Role)
	}
	if got.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", got.Choices[0].FinishReason)
	}
	if got.Usage.PromptTokens != 12 || got.Usage.CompletionTokens != 5 || got.Usage.TotalTokens != 17 {
		t.Errorf("usage = %+v, want {12 5 17}", got.Usage)
	}

	// Verify translation: system extracted, model resolved to bare name,
	// stream=true forwarded to upstream.
	if streamer.captured.Model != "claude-sonnet-4-6" {
		t.Errorf("upstream model = %q, want claude-sonnet-4-6 (bare)", streamer.captured.Model)
	}
	if !streamer.captured.Stream {
		t.Error("upstream stream should be true (handler always streams internally)")
	}
	if sys := llm.ExtractSystemText(streamer.captured.System); sys != "You are a helpful assistant." {
		t.Errorf("upstream system = %q, want %q", sys, "You are a helpful assistant.")
	}
	if len(streamer.captured.Messages) != 1 {
		t.Errorf("upstream messages len = %d, want 1 (system removed)", len(streamer.captured.Messages))
	}
}

func TestChatCompletions_FinishReasonMapping(t *testing.T) {
	cases := []struct {
		anthropic string
		openai    string
	}{
		{"end_turn", "stop"},
		{"max_tokens", "length"},
		{"tool_use", "tool_calls"},
		{"stop_sequence", "stop"},
		{"", "stop"},
	}
	for _, tc := range cases {
		t.Run(tc.anthropic, func(t *testing.T) {
			streamer := newChatStreamerFromText("ok", tc.anthropic, 1, 1)
			mux := newChatTestMux(t, streamer)
			rec := postChat(t, mux, map[string]any{
				"model":    "deneb-main",
				"messages": []map[string]any{{"role": "user", "content": "hi"}},
			})
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			var got chatCompletionResponse
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
			if got.Choices[0].FinishReason != tc.openai {
				t.Errorf("finish_reason = %q, want %q", got.Choices[0].FinishReason, tc.openai)
			}
		})
	}
}

func TestChatCompletions_StreamRejected(t *testing.T) {
	mux := newChatTestMux(t, &fakeStreamer{})
	rec := postChat(t, mux, map[string]any{
		"model":    "deneb-main",
		"stream":   true,
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501 (stream not yet supported)", rec.Code)
	}
}

func TestChatCompletions_UnknownModel(t *testing.T) {
	mux := newChatTestMux(t, &fakeStreamer{})
	rec := postChat(t, mux, map[string]any{
		"model":    "gpt-4-turbo",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body ErrorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !strings.Contains(body.Error.Message, "unknown model") {
		t.Errorf("error message = %q, want contains 'unknown model'", body.Error.Message)
	}
}

func TestChatCompletions_RoleNotConfigured(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		ModelRegistry: &fakeRegistry{models: map[modelrole.Role]string{
			// only main; light/fallback unconfigured
			modelrole.RoleMain: "anthropic/claude-sonnet-4-6",
		}},
		ChatClient: func(_ modelrole.Role) ChatStreamer { return &fakeStreamer{} },
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewReader(mustJSON(t, map[string]any{
			"model":    "deneb-light",
			"messages": []map[string]any{{"role": "user", "content": "hi"}},
		})))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestChatCompletions_EmptyMessages(t *testing.T) {
	mux := newChatTestMux(t, &fakeStreamer{})
	rec := postChat(t, mux, map[string]any{
		"model":    "deneb-main",
		"messages": []map[string]any{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestChatCompletions_ContentArrayParts(t *testing.T) {
	streamer := newChatStreamerFromText("ack", "end_turn", 1, 1)
	mux := newChatTestMux(t, streamer)

	rec := postChat(t, mux, map[string]any{
		"model": "deneb-main",
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{
				{"type": "text", "text": "Hello "},
				{"type": "text", "text": "world"},
				{"type": "image_url", "image_url": map[string]string{"url": "https://example.com/x.png"}},
			}},
		},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(streamer.captured.Messages) != 1 {
		t.Fatalf("upstream messages len = %d, want 1", len(streamer.captured.Messages))
	}
	var content string
	if err := json.Unmarshal(streamer.captured.Messages[0].Content, &content); err != nil {
		t.Fatalf("unmarshal upstream content: %v", err)
	}
	if content != "Hello world" {
		t.Errorf("upstream user content = %q, want %q (text parts joined, image_url dropped)", content, "Hello world")
	}
}

func TestChatCompletions_PassesSamplingParams(t *testing.T) {
	streamer := newChatStreamerFromText("ok", "end_turn", 1, 1)
	mux := newChatTestMux(t, streamer)

	temp := 0.3
	topP := 0.9
	maxTok := 256
	rec := postChat(t, mux, map[string]any{
		"model":       "deneb-main",
		"messages":    []map[string]any{{"role": "user", "content": "hi"}},
		"temperature": temp,
		"top_p":       topP,
		"max_tokens":  maxTok,
		"stop":        []string{"END", "STOP"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if streamer.captured.Temperature == nil || *streamer.captured.Temperature != temp {
		t.Errorf("temperature not forwarded: %+v", streamer.captured.Temperature)
	}
	if streamer.captured.TopP == nil || *streamer.captured.TopP != topP {
		t.Errorf("top_p not forwarded: %+v", streamer.captured.TopP)
	}
	if streamer.captured.MaxTokens != maxTok {
		t.Errorf("max_tokens = %d, want %d", streamer.captured.MaxTokens, maxTok)
	}
	if len(streamer.captured.StopSequences) != 2 {
		t.Errorf("stop_sequences = %v, want [END STOP]", streamer.captured.StopSequences)
	}
}

func TestChatCompletions_DefaultMaxTokens(t *testing.T) {
	streamer := newChatStreamerFromText("ok", "end_turn", 1, 1)
	mux := newChatTestMux(t, streamer)

	rec := postChat(t, mux, map[string]any{
		"model":    "deneb-main",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
		// max_tokens omitted
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if streamer.captured.MaxTokens != defaultMaxTokens {
		t.Errorf("max_tokens = %d, want default %d", streamer.captured.MaxTokens, defaultMaxTokens)
	}
}

func TestChatCompletions_BearerEnforced(t *testing.T) {
	streamer := newChatStreamerFromText("ok", "end_turn", 1, 1)
	mux := http.NewServeMux()
	Mount(mux, Deps{
		AuthToken: "secret",
		ModelRegistry: &fakeRegistry{models: map[modelrole.Role]string{
			modelrole.RoleMain: "anthropic/claude-sonnet-4-6",
		}},
		ChatClient: func(_ modelrole.Role) ChatStreamer { return streamer },
	})

	body := mustJSON(t, map[string]any{
		"model":    "deneb-main",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})

	cases := []struct {
		name, header string
		wantStatus   int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"wrong token", "Bearer wrong", http.StatusUnauthorized},
		{"correct token", "Bearer secret", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}
