package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

type constantTool struct{ output string }

func (t constantTool) Execute(_ context.Context, _ string, _ json.RawMessage) (string, error) {
	return t.output, nil
}

// A warning-level loop must reach the MODEL, not just the operator log: the
// tool still runs at this level, so the result block is the only channel the
// author has before the critical threshold blocks the call outright.
func TestToolLoopWarningIsPrefixedOntoTheResult(t *testing.T) {
	detector := NewToolLoopDetector(DefaultToolLoopConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	tools := constantTool{output: "SAME-OUTPUT"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var last llm.ContentBlock
	for i := 0; i < DefaultToolLoopConfig().WarningThreshold+1; i++ {
		call := llm.ContentBlock{
			Type:  "tool_use",
			ID:    "call_1",
			Name:  "exec",
			Input: llm.FlexibleFromRaw([]byte(`{"command":"echo SAME-OUTPUT"}`)),
		}
		last = executeOneToolTracked(
			context.Background(), call, tools, StreamHooks{}, "", 0, logger, nil, detector,
		).block
	}

	if !strings.Contains(last.Content, "has been polled") {
		t.Fatalf("warning never reached the model:\n%s", last.Content)
	}
	if !strings.Contains(last.Content, "SAME-OUTPUT") {
		t.Fatalf("tool output must survive the prefix:\n%s", last.Content)
	}
}

// The recorded result hash must stay the un-prefixed output: the warning text
// carries a call count, so hashing it would reset the no-progress streak that
// escalates to the critical block.
func TestLoopWarningPrefixDoesNotResetTheNoProgressStreak(t *testing.T) {
	detector := NewToolLoopDetector(DefaultToolLoopConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	tools := constantTool{output: "SAME-OUTPUT"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var last llm.ContentBlock
	for i := 0; i < DefaultToolLoopConfig().CriticalThreshold+1; i++ {
		call := llm.ContentBlock{
			Type:  "tool_use",
			ID:    "call_1",
			Name:  "exec",
			Input: llm.FlexibleFromRaw([]byte(`{"command":"echo SAME-OUTPUT"}`)),
		}
		last = executeOneToolTracked(
			context.Background(), call, tools, StreamHooks{}, "", 0, logger, nil, detector,
		).block
	}

	if !last.IsError {
		t.Fatalf("streak must still escalate to the critical block, got:\n%s", last.Content)
	}
}
