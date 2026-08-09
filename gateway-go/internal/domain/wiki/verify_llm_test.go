package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestDetectMisclassificationsTemplateOffSwitchOmitsReasoningEffort(t *testing.T) {
	var body map[string]any
	var handlerErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			handlerErr = fmt.Errorf("path = %q, want /chat/completions", r.URL.Path)
			http.Error(w, handlerErr.Error(), http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			handlerErr = fmt.Errorf("decode request: %w", err)
			http.Error(w, handlerErr.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[]"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	wd := &WikiDreamer{
		client: llm.NewClient(srv.URL, ""),
		model:  "deepseek-v4-flash",
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		llmExtraBody: jsonObject{
			"chat_template_kwargs": map[string]any{"thinking": false},
		},
	}

	findings := wd.detectMisclassifications(context.Background(), map[string]IndexEntry{
		"인물/호수.md": {Title: "호수", Category: "인물", Summary: "장소"},
	})
	if handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none from [] response", findings)
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("request included reasoning_effort despite template off-switch: %#v", body)
	}
	kwargs, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("chat_template_kwargs = %#v, want object", body["chat_template_kwargs"])
	}
	if got := kwargs["thinking"]; got != false {
		t.Fatalf("chat_template_kwargs.thinking = %#v, want false", got)
	}
}
