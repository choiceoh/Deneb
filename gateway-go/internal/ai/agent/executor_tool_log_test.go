package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestExecuteOneToolTracked_ToolResultErrorLogsCompleteAtInfo(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	exec := toolExecFunc(func(context.Context, string, json.RawMessage) (string, error) {
		return "", errors.New("validation failed")
	})
	tc := llm.ContentBlock{
		Type:  "tool_use",
		ID:    "toolu_1",
		Name:  "read",
		Input: llm.FlexibleFromRaw([]byte(`{}`)),
	}

	block := executeOneToolTracked(context.Background(), tc, exec, StreamHooks{}, "", 0, logger, nil, nil).block

	if !block.IsError {
		t.Fatalf("tool result IsError = false, want true: %+v", block)
	}
	got := logs.String()
	if !strings.Contains(got, `level=INFO msg="tool complete"`) {
		t.Fatalf("tool complete log should be info, got:\n%s", got)
	}
	if strings.Contains(got, `level=WARN msg="tool complete"`) {
		t.Fatalf("tool complete log should not be warn, got:\n%s", got)
	}
	if !strings.Contains(got, `isError=true`) || !strings.Contains(got, `errorHead=`) {
		t.Fatalf("tool complete log lost error fields, got:\n%s", got)
	}
}

func TestExecuteOneToolTracked_ToolPanicStillLogsError(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	exec := toolExecFunc(func(context.Context, string, json.RawMessage) (string, error) {
		panic("boom")
	})
	tc := llm.ContentBlock{
		Type:  "tool_use",
		ID:    "toolu_1",
		Name:  "read",
		Input: llm.FlexibleFromRaw([]byte(`{}`)),
	}

	block := executeOneToolTracked(context.Background(), tc, exec, StreamHooks{}, "", 0, logger, nil, nil).block

	if !block.IsError {
		t.Fatalf("tool result IsError = false, want true: %+v", block)
	}
	got := logs.String()
	if !strings.Contains(got, `level=ERROR msg="tool executor panic"`) {
		t.Fatalf("panic log should remain error, got:\n%s", got)
	}
	if strings.Contains(got, `level=WARN msg="tool complete"`) {
		t.Fatalf("tool complete log should not be warn, got:\n%s", got)
	}
}
