package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func TestPrewarmModel_NoConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// Should not panic when no config file exists.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	PrewarmModel(ctx, logger)
}

func TestPrewarmModel_ContextCanceled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.
	PrewarmModel(ctx, logger)
}

func TestDoPrewarmRequest_OpenAI(t *testing.T) {
	var called atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		// Return a minimal OpenAI streaming response.
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"index":0}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", llm.WithRetry(0, 0, 0))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := doPrewarmRequest(ctx, client, "test-model")
	if err != nil {
		t.Fatalf("doPrewarmRequest failed: %v", err)
	}
	if called.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", called.Load())
	}
}

func TestSplitModelID(t *testing.T) {
	tests := []struct {
		input     string
		wantProv  string
		wantModel string
	}{
		{"zai/glm-5-turbo", "zai", "glm-5-turbo"},
		{"anthropic/claude-3-haiku", "anthropic", "claude-3-haiku"},
		{"just-model", "", "just-model"},
		{"", "", ""},
	}
	for _, tt := range tests {
		prov, model := splitModelID(tt.input)
		if prov != tt.wantProv || model != tt.wantModel {
			t.Errorf("splitModelID(%q) = (%q, %q), want (%q, %q)",
				tt.input, prov, model, tt.wantProv, tt.wantModel)
		}
	}
}

func TestExtractModelFromDefaults(t *testing.T) {
	// String form.
	raw := json.RawMessage(`{"model":"zai/glm-5-turbo"}`)
	if got := extractModelFromDefaults(raw); got != "zai/glm-5-turbo" {
		t.Errorf("string form: got %q, want %q", got, "zai/glm-5-turbo")
	}

	// Object form.
	raw = json.RawMessage(`{"model":{"primary":"anthropic/claude-3-haiku"}}`)
	if got := extractModelFromDefaults(raw); got != "anthropic/claude-3-haiku" {
		t.Errorf("object form: got %q, want %q", got, "anthropic/claude-3-haiku")
	}

	// Empty.
	if got := extractModelFromDefaults(nil); got != "" {
		t.Errorf("nil: got %q, want empty", got)
	}
}

