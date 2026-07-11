package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

type failingAppendTranscript struct {
	*MemoryTranscriptStore
}

func (f *failingAppendTranscript) Append(string, ChatMessage) error {
	return errors.New("injected append failure")
}

func TestBriefcaseFailsBeforeModelWhenTranscriptPersistenceFails(t *testing.T) {
	cfg := DefaultHandlerConfig()
	cfg.LLMClient = llm.NewClient("http://127.0.0.1:1", "")
	cfg.Transcript = &failingAppendTranscript{MemoryTranscriptStore: NewMemoryTranscriptStore()}
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "briefcase persistence test"
	cfg.BriefcaseMode = true
	handler := NewHandler(session.NewManager(), nil, nil, cfg)
	defer handler.Close()

	_, err := handler.SendSync(context.Background(), "briefcase-persistence", "hello", "test-model", &SyncOptions{ToolPreset: "briefcase"})
	if err == nil || !strings.Contains(err.Error(), "briefcase transcript persistence") {
		t.Fatalf("error = %v, want strict persistence failure", err)
	}
}
