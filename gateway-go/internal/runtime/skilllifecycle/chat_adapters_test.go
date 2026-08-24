package skilllifecycle

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

type usageRecorderTranscriptStore struct {
	msgs  []toolport.ChatMessage
	byKey map[string][]toolport.ChatMessage
}

func (s usageRecorderTranscriptStore) Append(string, toolport.ChatMessage) error { return nil }
func (s usageRecorderTranscriptStore) Load(key string, _ int) ([]toolport.ChatMessage, int, error) {
	msgs := s.msgs
	if s.byKey != nil {
		msgs = s.byKey[key]
	}
	return append([]toolport.ChatMessage(nil), msgs...), len(msgs), nil
}
func (s usageRecorderTranscriptStore) Delete(string) error         { return nil }
func (s usageRecorderTranscriptStore) ListKeys() ([]string, error) { return nil, nil }
func (s usageRecorderTranscriptStore) Search(string, int) ([]toolport.SearchResult, error) {
	return nil, nil
}
func (s usageRecorderTranscriptStore) CloneRecent(string, string, int) error { return nil }

func newUsageRecorderTracker(t *testing.T) *genesis.Tracker {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	tracker, err := genesis.NewTracker(slog.Default())
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	return tracker
}

func TestChatUsageRecorderAutoValidationCaseFromFailedUse(t *testing.T) {
	tracker := newUsageRecorderTracker(t)
	store := usageRecorderTranscriptStore{msgs: []toolport.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"srv1에서 실제 deneb-gateway 상태를 확인하고 개선"`)},
		{Role: "assistant", Content: json.RawMessage(`[
			{"type":"tool_use","id":"tu_1","name":"exec","input":{"cmd":"ssh srv1 systemctl --user status deneb-gateway.service","workdir":"/srv/deneb"}}
		]`)},
		{Role: "user", Content: json.RawMessage(`[
			{"type":"tool_result","tool_use_id":"tu_1","content":"Active: failed","is_error":true}
		]`)},
	}}
	rec := NewChatUsageRecorder(tracker, store, slog.Default(), true, nil, "")

	rec.RecordSkillUse("client:main:srv1", "srv1-ops", false, "turn failed: tool exec errored", "m1")

	var cases []genesis.SkillValidationCaseRecord
	var err error
	for i := 0; i < 20; i++ {
		cases, err = tracker.RecentSkillValidationCases("srv1-ops", 5)
		if err != nil {
			t.Fatalf("RecentSkillValidationCases: %v", err)
		}
		if len(cases) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(cases) != 1 {
		t.Fatalf("expected one auto validation case, got %+v", cases)
	}
	rec.RecordSkillUse("client:main:srv1", "srv1-ops", false, "turn failed: tool exec errored", "m1")
	time.Sleep(50 * time.Millisecond)
	tc := cases[0]
	if tc.Source != "auto-failed-skill-use" ||
		tc.ID != "session-client:main:srv1" ||
		!strings.Contains(tc.Description, "turn failed: tool exec errored") ||
		tc.Replay.Input != "srv1에서 실제 deneb-gateway 상태를 확인하고 개선" ||
		len(tc.Replay.ExpectedToolCalls) != 0 ||
		len(tc.Replay.RequiredTools) != 0 ||
		len(tc.Replay.ForbiddenToolCalls) != 1 ||
		tc.Replay.ForbiddenToolCalls[0].Name != "exec" ||
		len(tc.Replay.ForbiddenToolCalls[0].InputIncludes) != 2 ||
		tc.Replay.ForbiddenToolCalls[0].InputIncludes[0] != "ssh srv1" ||
		tc.Replay.ForbiddenToolCalls[0].InputIncludes[1] != "systemctl --user status" {
		t.Fatalf("unexpected auto validation case: %+v", tc)
	}
	summary, err := tracker.ValidationCaseSummary("srv1-ops")
	if err != nil {
		t.Fatalf("ValidationCaseSummary: %v", err)
	}
	if summary.RawRecords != 1 || summary.AutomaticRecords != 1 || summary.UniqueAutomaticRecords != 1 {
		t.Fatalf("expected auto failed-use case in automatic summary, got %+v", summary)
	}
}

func TestChatUsageRecorderAutoValidationCaseRefreshesRicherFailedUseTrace(t *testing.T) {
	tracker := newUsageRecorderTracker(t)
	sessionKey := "client:main:srv1"
	store := usageRecorderTranscriptStore{byKey: map[string][]toolport.ChatMessage{
		sessionKey: {
			{Role: "user", Content: json.RawMessage(`"srv1에서 deneb-gateway 복구"`)},
			{Role: "assistant", Content: json.RawMessage(`[
				{"type":"tool_use","id":"tu_1","name":"exec","input":{"cmd":"systemctl --user restart deneb-gateway.service"}}
			]`)},
			{Role: "user", Content: json.RawMessage(`[
				{"type":"tool_result","tool_use_id":"tu_1","content":"Failed to connect to bus","is_error":true}
			]`)},
		},
	}}
	rec := &chatUsageRecorderAdapter{inner: tracker, transcripts: store, logger: slog.Default()}

	rec.recordValidationCaseFromFailedUse(sessionKey, "srv1-ops", "turn failed: local restart errored")
	store.byKey[sessionKey] = []toolport.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"srv1에서 deneb-gateway 복구"`)},
		{Role: "assistant", Content: json.RawMessage(`[
			{"type":"tool_use","id":"tu_1","name":"exec","input":{"cmd":"systemctl --user restart deneb-gateway.service"}},
			{"type":"tool_use","id":"tu_2","name":"exec","input":{"cmd":"ssh srv1 systemctl --user status deneb-gateway.service"}}
		]`)},
		{Role: "user", Content: json.RawMessage(`[
			{"type":"tool_result","tool_use_id":"tu_1","content":"Failed to connect to bus","is_error":true},
			{"type":"tool_result","tool_use_id":"tu_2","content":"Active: active (running)","is_error":false}
		]`)},
	}
	rec.recordValidationCaseFromFailedUse(sessionKey, "srv1-ops", "turn failed: local restart errored")

	summary, err := tracker.ValidationCaseSummary("srv1-ops")
	if err != nil {
		t.Fatalf("ValidationCaseSummary: %v", err)
	}
	if summary.RawRecords != 2 || summary.UniqueRecords != 1 || summary.DuplicateRecords != 1 {
		t.Fatalf("expected richer same-session case to append and dedupe, got %+v", summary)
	}
	cases, err := tracker.RecentSkillValidationCases("srv1-ops", 5)
	if err != nil {
		t.Fatalf("RecentSkillValidationCases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected one effective case, got %+v", cases)
	}
	replay := cases[0].Replay
	if len(replay.ExpectedToolCalls) != 1 ||
		replay.ExpectedToolCalls[0].InputIncludes[0] != "ssh srv1" ||
		replay.ExpectedToolCalls[0].InputIncludes[1] != "systemctl --user status" ||
		len(replay.ForbiddenToolCalls) != 1 ||
		replay.ForbiddenToolCalls[0].InputIncludes[0] != "systemctl --user restart" ||
		len(replay.RequiredTools) != 1 ||
		replay.RequiredTools[0] != "exec" {
		t.Fatalf("expected richer replay to preserve recovery and forbidden calls, got %+v", replay)
	}
}

// The attribution written by the chat pipeline must survive the whole seam —
// adapter → tracker → on-disk record — or the layer split downstream reads
// every real record as unattributable.
func TestChatUsageRecorderPersistsAttribution(t *testing.T) {
	tracker := newUsageRecorderTracker(t)
	rec := NewChatUsageRecorder(tracker, usageRecorderTranscriptStore{}, slog.Default(), false, nil, "")

	attributed, ok := rec.(chatport.SkillUsageAttributionRecorder)
	if !ok {
		t.Fatal("chat usage recorder must implement the attribution capability")
	}
	attributed.RecordSkillUseAttributed("client:main", "kb", false, "run failed: tool web errored", "m1",
		chatport.SkillUseAttribution{
			Delivery:  chatport.SkillDeliveryAutoLoad,
			Exercised: chatport.SkillExercisedNo,
		})

	summary, err := tracker.UsageQualitySummary("kb")
	if err != nil {
		t.Fatalf("UsageQualitySummary: %v", err)
	}
	if summary.FailureLayers.UnexercisedFailures != 1 {
		t.Fatalf("attribution lost across the seam: %+v", summary.FailureLayers)
	}
	if summary.FailureLayers.AutoLoadFailures != 1 {
		t.Errorf("delivery lost across the seam: %+v", summary.FailureLayers)
	}

	// The plain path must still work for callers that have no attribution.
	rec.RecordSkillUse("client:main", "kb", false, "run failed: no result", "m1")
	summary, err = tracker.UsageQualitySummary("kb")
	if err != nil {
		t.Fatalf("UsageQualitySummary: %v", err)
	}
	if summary.FailureLayers.UnattributableFailures != 1 {
		t.Errorf("unattributed record misfiled: %+v", summary.FailureLayers)
	}
}
