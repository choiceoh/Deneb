package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestMergeJSONFields(t *testing.T) {
	base := []byte(`{"model":"test","stream":true}`)
	extra := map[string]any{
		"timeout": 30.0,
	}
	got := testutil.Must(mergeJSONFields(base, extra))
	s := string(got)
	if !strings.Contains(s, `"timeout":30`) {
		t.Errorf("expected timeout:30 in result, got: %s", s)
	}
	if !strings.Contains(s, `"model":"test"`) {
		t.Errorf("expected original fields preserved, got: %s", s)
	}
}

func TestIsOpenAIReasoningModel(t *testing.T) {
	reasoning := []string{"o1", "o1-mini", "o3", "o3-mini", "o4-mini", "gpt-5", "gpt-5-mini", "openai/o3-mini", "O3-Mini"}
	for _, m := range reasoning {
		if !isOpenAIReasoningModel(m) {
			t.Errorf("isOpenAIReasoningModel(%q) = false, want true", m)
		}
	}
	// OpenAI-compatible servers (vLLM, etc.) and non-reasoning OpenAI models
	// must NOT be remapped — they keep max_tokens.
	compatible := []string{"step3p7", "qwen3.6-35b-a3b", "mimo-v2.5-pro", "kimi-for-coding", "gemini-3.5-flash", "gpt-4o", "vllm/step3p7", ""}
	for _, m := range compatible {
		if isOpenAIReasoningModel(m) {
			t.Errorf("isOpenAIReasoningModel(%q) = true, want false", m)
		}
	}
}

// TestApplySamplingParams_MaxTokensRemap guards the regression where extended
// thinking remapped max_tokens to max_completion_tokens (and zeroed max_tokens)
// for every OpenAI-mode request — breaking self-hosted vLLM, which 400s on
// "max_tokens must be at least 1, got 0". The remap must apply ONLY to genuine
// OpenAI reasoning models.
func TestApplySamplingParams_MaxTokensRemap(t *testing.T) {
	thinking := &ThinkingConfig{Type: "enabled", BudgetTokens: 8192}

	t.Run("vllm preserves max_tokens", func(t *testing.T) {
		oai := &openAIRequest{MaxTokens: 32768}
		applySamplingParams(oai, &ChatRequest{Model: "step3p7", Thinking: thinking})
		if oai.MaxTokens != 32768 {
			t.Errorf("MaxTokens = %d, want 32768 (preserved for vLLM)", oai.MaxTokens)
		}
		if oai.MaxCompletionTokens != nil {
			t.Errorf("MaxCompletionTokens = %d, want nil for vLLM", *oai.MaxCompletionTokens)
		}
	})

	t.Run("openai reasoning remaps to max_completion_tokens", func(t *testing.T) {
		oai := &openAIRequest{MaxTokens: 32768}
		applySamplingParams(oai, &ChatRequest{Model: "o3-mini", Thinking: thinking})
		if oai.MaxTokens != 0 {
			t.Errorf("MaxTokens = %d, want 0 for reasoning model", oai.MaxTokens)
		}
		if oai.MaxCompletionTokens == nil || *oai.MaxCompletionTokens != 32768 {
			t.Errorf("MaxCompletionTokens = %v, want 32768", oai.MaxCompletionTokens)
		}
	})
}

func TestComplete_OmitsAuthorizationHeaderWhenAPIKeyEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization header = %q, want empty", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	got, err := client.Complete(context.Background(), ChatRequest{
		Model:     "local-model",
		Messages:  []Message{NewTextMessage("user", "hello")},
		MaxTokens: 16,
	})
	testutil.NoError(t, err)
	if got != "ok" {
		t.Fatalf("CompleteOpenAI = %q, want %q", got, "ok")
	}
}
