package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// advisingExecutor is a ToolExecutor that also carries error-time correction
// memory (the capability ToolRegistry implements in production).
type advisingExecutor struct {
	err     error
	out     string
	advice  string
	gotTool string
	gotErr  string
}

func (a *advisingExecutor) Execute(context.Context, string, json.RawMessage) (string, error) {
	return a.out, a.err
}

func (a *advisingExecutor) ToolErrorAdvice(toolName, errText string) string {
	a.gotTool, a.gotErr = toolName, errText
	return a.advice
}

func adviceTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func adviceToolCall() llm.ContentBlock {
	return llm.ContentBlock{Type: "tool_use", ID: "toolu_1", Name: "gmail", Input: llm.FlexibleFromRaw([]byte(`{}`))}
}

// A failed call carries the learned correction back to the model, and the
// advisor sees the real tool name and error text it must match on.
func TestExecuteOneToolTracked_FailedCallAppendsLearnedCorrection(t *testing.T) {
	exec := &advisingExecutor{err: errors.New("label not found"), advice: "이전에 scope 필드를 고쳐 성공했다"}

	block := executeOneToolTracked(context.Background(), adviceToolCall(), exec, StreamHooks{}, "", 0, adviceTestLogger(), nil, nil).block

	if !block.IsError {
		t.Fatalf("IsError = false, want true: %+v", block)
	}
	if !strings.Contains(block.Content, "Error: label not found") {
		t.Errorf("error text lost: %q", block.Content)
	}
	if !strings.Contains(block.Content, exec.advice) {
		t.Errorf("advice missing from %q", block.Content)
	}
	if exec.gotTool != "gmail" || exec.gotErr != "label not found" {
		t.Errorf("advisor saw (%q, %q), want (gmail, label not found)", exec.gotTool, exec.gotErr)
	}
}

// Nothing is appended when there is no learned correction, and a successful
// call never consults the advisor at all — the hint is an error-only cost.
func TestExecuteOneToolTracked_AdviceOnlyOnFailureWithEvidence(t *testing.T) {
	silent := &advisingExecutor{err: errors.New("label not found")}
	block := executeOneToolTracked(context.Background(), adviceToolCall(), silent, StreamHooks{}, "", 0, adviceTestLogger(), nil, nil).block
	if block.Content != "Error: label not found" {
		t.Errorf("empty advice changed content: %q", block.Content)
	}

	ok := &advisingExecutor{out: "sent", advice: "이전에 scope 필드를 고쳐 성공했다"}
	block = executeOneToolTracked(context.Background(), adviceToolCall(), ok, StreamHooks{}, "", 0, adviceTestLogger(), nil, nil).block
	if block.IsError {
		t.Fatalf("successful call marked as error: %+v", block)
	}
	if strings.Contains(block.Content, ok.advice) {
		t.Errorf("advice leaked into a successful result: %q", block.Content)
	}
	if ok.gotTool != "" {
		t.Errorf("advisor consulted on success (tool %q)", ok.gotTool)
	}
}

// A plain ToolExecutor without the optional capability must still work.
func TestExecuteOneToolTracked_PlainExecutorNeedsNoAdvisor(t *testing.T) {
	exec := toolExecFunc(func(context.Context, string, json.RawMessage) (string, error) {
		return "", errors.New("boom")
	})
	block := executeOneToolTracked(context.Background(), adviceToolCall(), exec, StreamHooks{}, "", 0, adviceTestLogger(), nil, nil).block
	if block.Content != "Error: boom" {
		t.Errorf("content = %q, want plain error", block.Content)
	}
}
