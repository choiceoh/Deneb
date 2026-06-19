package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// TestRecordValidationCaseFromSuccessfulUse verifies the success mirror of the
// failed-use capture: a successful skill run's tool-call trace becomes a held-out
// replay case whose ExpectedToolCalls are the proven-good calls. This is the
// corpus the behavioral evolve gate (SkillValidationEngine.EvaluateBehavior)
// consumes — without it the gate stays inert.
func TestRecordValidationCaseFromSuccessfulUse(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	const sessionKey = "client:main:topsolar"
	store := skillLifecycleTranscriptStore{byKey: map[string][]toolctx.ChatMessage{
		sessionKey: {
			{Role: "user", Content: json.RawMessage(`"탑솔라 프로젝트 현황 알려줘"`)},
			{Role: "assistant", Content: json.RawMessage(`[
				{"type":"tool_use","id":"tu_1","name":"exec","input":{"cmd":"python3 topsolar.py dashboard"}}
			]`)},
			{Role: "user", Content: json.RawMessage(`[
				{"type":"tool_result","tool_use_id":"tu_1","content":"{\"status\":\"ok\"}","is_error":false}
			]`)},
			{Role: "assistant", Content: json.RawMessage(`"현황을 정리했습니다."`)},
		},
	}}
	adapter := &chatUsageRecorderAdapter{
		inner:       tracker,
		transcripts: store,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	adapter.recordValidationCaseFromSuccessfulUse(sessionKey, "topsolar-db")

	cases, err := tracker.RecentSkillValidationCases("topsolar-db", 10)
	if err != nil {
		t.Fatalf("RecentSkillValidationCases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected 1 captured case, got %d: %+v", len(cases), cases)
	}
	c := cases[0]
	if c.Source != "auto-successful-skill-use" {
		t.Fatalf("expected auto-successful-skill-use source, got %q", c.Source)
	}
	if c.Replay.Input != "탑솔라 프로젝트 현황 알려줘" {
		t.Fatalf("expected the user task as replay input, got %q", c.Replay.Input)
	}
	if len(c.Replay.ExpectedToolCalls) != 1 || c.Replay.ExpectedToolCalls[0].Name != "exec" {
		t.Fatalf("expected one exec tool call captured, got %+v", c.Replay.ExpectedToolCalls)
	}
	if len(c.Replay.ExpectedToolCalls[0].InputIncludes) == 0 {
		t.Fatalf("expected captured call to carry input fragments, got %+v", c.Replay.ExpectedToolCalls[0])
	}
	// The proven-good call must be EXPECTED, never forbidden — that is exactly
	// what the behavioral gate protects against a regressing rewrite.
	if len(c.Replay.ForbiddenToolCalls) != 0 {
		t.Fatalf("a successful run must not record forbidden calls, got %+v", c.Replay.ForbiddenToolCalls)
	}
}

// TestRecordValidationCaseFromSuccessfulUse_NoToolCallsSkipped verifies a
// successful run that made no tool calls is not recorded: there is nothing for
// the behavioral gate to protect (and the weak-automatic guard would reject it).
func TestRecordValidationCaseFromSuccessfulUse_NoToolCallsSkipped(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	const sessionKey = "client:main:chat"
	store := skillLifecycleTranscriptStore{byKey: map[string][]toolctx.ChatMessage{
		sessionKey: {
			{Role: "user", Content: json.RawMessage(`"그냥 인사"`)},
			{Role: "assistant", Content: json.RawMessage(`"안녕하세요."`)},
		},
	}}
	adapter := &chatUsageRecorderAdapter{
		inner:       tracker,
		transcripts: store,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	adapter.recordValidationCaseFromSuccessfulUse(sessionKey, "chitchat")

	cases, err := tracker.RecentSkillValidationCases("chitchat", 10)
	if err != nil {
		t.Fatalf("RecentSkillValidationCases: %v", err)
	}
	if len(cases) != 0 {
		t.Fatalf("expected no case for a tool-less successful run, got %+v", cases)
	}
}
