package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// bodyRecorder is a test server that records request bodies and answers every
// request with the given SSE stream.
func bodyRecorder(t *testing.T, sse string) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var (
		mu     sync.Mutex
		bodies []map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		if stream, _ := body["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sse)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	return server, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), bodies...)
	}
}

const okStream = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"

// Behind OpenRouter, reasoning_effort "low" keeps nemotron reasoning, and the
// reasoning lands in the content — the JSON a stage-1 extractor parses breaks.
func TestReasoningParamSendsReasoningFieldForDisabledThinking(t *testing.T) {
	server, bodies := bodyRecorder(t, okStream)
	disabled := &ThinkingConfig{Type: "disabled"}
	msgs := []Message{NewTextMessage("user", "hi")}

	or := NewClient(server.URL, "k", WithReasoningParam())
	if events, err := or.StreamChat(context.Background(), ChatRequest{Model: "m", Messages: msgs, Thinking: disabled}); err != nil {
		t.Fatal(err)
	} else {
		for range events {
		}
	}
	if _, err := or.Complete(context.Background(), ChatRequest{Model: "m", Messages: msgs, Thinking: disabled}); err != nil {
		t.Fatal(err)
	}
	plain := NewClient(server.URL, "k")
	if events, err := plain.StreamChat(context.Background(), ChatRequest{Model: "m", Messages: msgs, Thinking: disabled}); err != nil {
		t.Fatal(err)
	} else {
		for range events {
		}
	}

	got := bodies()
	if len(got) != 3 {
		t.Fatalf("requests = %d, want 3", len(got))
	}
	for i, body := range got[:2] {
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok || reasoning["enabled"] != false {
			t.Errorf("reasoning-param request %d = %v, want reasoning.enabled=false", i, body)
		}
		if _, has := body["reasoning_effort"]; has {
			t.Errorf("reasoning-param request %d still sends reasoning_effort: %v", i, body)
		}
		if _, has := body["chat_template_kwargs"]; has {
			t.Errorf("reasoning-param request %d sends chat_template_kwargs: %v", i, body)
		}
	}
	if got[2]["reasoning_effort"] != "low" || got[2]["reasoning"] != nil {
		t.Errorf("a client without the option changed shape: %v", got[2])
	}
}

// OpenRouter puts the usage in a final chunk that repeats the finishing choice.
// Dropping it (a choice chunk after finish_reason) reported every OpenRouter
// call as zero tokens, so the free fallback never showed in usage accounting.
func TestUsageAfterFinishReasonIsKept(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"안녕"},"finish_reason":null}]}`,
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":"stop"}]}`,
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":26,"completion_tokens":3,"total_tokens":29}}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"
	server, _ := bodyRecorder(t, sse)

	events, err := NewClient(server.URL, "k").StreamChat(context.Background(), ChatRequest{Model: "m", Messages: []Message{NewTextMessage("user", "hi")}})
	if err != nil {
		t.Fatal(err)
	}
	var (
		text          string
		input, output int
		stopReasons   []string
	)
	for ev := range events {
		switch ev.Type {
		case "content_block_delta":
			var d struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			_ = json.Unmarshal(ev.Payload.Bytes(), &d)
			text += d.Delta.Text
		case "message_start":
			var ms MessageStart
			if json.Unmarshal(ev.Payload.Bytes(), &ms) == nil && ms.Message.Usage.InputTokens > 0 {
				input = ms.Message.Usage.InputTokens
			}
		case "message_delta":
			var md MessageDelta
			if json.Unmarshal(ev.Payload.Bytes(), &md) == nil {
				if md.Usage.OutputTokens > 0 {
					output = md.Usage.OutputTokens
				}
				if md.Delta.StopReason != "" {
					stopReasons = append(stopReasons, md.Delta.StopReason)
				}
			}
		}
	}
	if text != "안녕" {
		t.Errorf("text = %q, want the content once", text)
	}
	if input != 26 || output != 3 {
		t.Errorf("usage = input %d / output %d, want 26 / 3", input, output)
	}
	if len(stopReasons) != 1 || stopReasons[0] != "end_turn" {
		t.Errorf("stop reasons = %v, want one end_turn (the usage chunk must not add another)", stopReasons)
	}
}

func TestInBandErrorCode(t *testing.T) {
	for raw, want := range map[string]int{
		`{"type":"","message":"x","code":502}`:         502,
		`{"error":{"code":429,"message":"slow down"}}`: 429,
		`{"error":{"code":"1302","message":"quota"}}`:  0,
		`{"message":"generation failed"}`:              0,
		`not json`:                                     0,
	} {
		if got := InBandErrorCode(FlexibleFromRaw([]byte(raw))); got != want {
			t.Errorf("InBandErrorCode(%s) = %d, want %d", raw, got, want)
		}
	}
}

func TestClassifyInBandError(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		code                 int
		message              string
		partial              bool
		transient, retrySame bool
	}{
		{"502 before text", 502, "x", false, true, true},
		{"503 before text", 503, "x", false, true, true},
		{"429 before text", 429, "x", false, true, false},
		{"400 before text", 400, "bad request", false, false, false},
		{"502 after text", 502, "x", true, false, false},
		{"deterministic, no code", 0, "generation failed", false, false, false},
		{"overloaded wording, no code", 0, "Service temporarily overloaded", false, false, false},
		{"rate limit, no code", 0, "Rate limit exceeded, please retry", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transient, retrySame := ClassifyInBandError(tc.code, tc.message, tc.partial)
			if transient != tc.transient || retrySame != tc.retrySame {
				t.Fatalf("ClassifyInBandError = (%v, %v), want (%v, %v)", transient, retrySame, tc.transient, tc.retrySame)
			}
		})
	}
}
