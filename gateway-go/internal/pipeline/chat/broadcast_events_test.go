package chat

import (
	"encoding/json"
	"sort"
	"testing"
)

// TestBroadcastEventsWireShape locks the wire JSON of every broadcast event. The
// native client parses these payloads by key, so the exact top-level key set must
// match what the prior map[string]any literals produced — including omitempty
// fields that appear on one emitter path but not another (sessions.changed.deltaMs,
// chat.delivery_failed.error, chat.compaction_stuck.budget/inputHash).
func TestBroadcastEventsWireShape(t *testing.T) {
	cases := []struct {
		name     string
		payload  any
		wantKeys []string
	}{
		{
			"sessions.changed minimal (no deltaMs)",
			SessionsChangedEvent{SessionKey: "k", Reason: "message_sent", Status: "running"},
			[]string{"reason", "sessionKey", "status"},
		},
		{
			"sessions.changed merged (deltaMs present)",
			SessionsChangedEvent{SessionKey: "k", Reason: "merged", Status: "running", DeltaMs: 42},
			[]string{"deltaMs", "reason", "sessionKey", "status"},
		},
		{
			"chat.delivery_failed without error",
			ChatDeliveryFailedEvent{Session: "s", Channel: "client", Reason: "parse_directives_nil"},
			[]string{"channel", "reason", "session"},
		},
		{
			"chat.delivery_failed with error",
			ChatDeliveryFailedEvent{Session: "s", Channel: "client", Reason: "reply_func_error", Error: "boom"},
			[]string{"channel", "error", "reason", "session"},
		},
		{
			"chat.empty_response (turns=0 still present)",
			ChatEmptyResponseEvent{Session: "s", Channel: "client", StopReason: "end_turn", Turns: 0},
			[]string{"channel", "session", "stopReason", "turns"},
		},
		{
			"chat.media_delivery_failed",
			ChatMediaDeliveryFailedEvent{Session: "s", Channel: "client", Count: 1, Total: 2, URLs: []string{"u"}},
			[]string{"channel", "count", "session", "total", "urls"},
		},
		{
			"session.tool (isError=false still present)",
			SessionToolEvent{SessionKey: "k", RunID: "r", Tool: "fs", ToolUseID: "t", IsError: false},
			[]string{"isError", "runId", "sessionKey", "tool", "toolUseId"},
		},
		{
			"chat.tool_failed (session AND sessionKey)",
			ChatToolFailedEvent{Session: "s", SessionKey: "s", RunID: "r", Tool: "fs", Reason: "mutation_tool_in_band_failure", Error: "e"},
			[]string{"error", "reason", "runId", "session", "sessionKey", "tool"},
		},
		{
			"chat.compaction_degraded",
			ChatCompactionDegradedEvent{Session: "s", TokensBefore: 10, TokensAfter: 5, Budget: 8},
			[]string{"budget", "session", "tokensAfter", "tokensBefore"},
		},
		{
			"chat.compaction_stuck (budget path, no inputHash)",
			ChatCompactionStuckEvent{Reason: "protected_zone_exceeds_budget", MessageCount: 3, Budget: 8},
			[]string{"budget", "messageCount", "reason"},
		},
		{
			"chat.compaction_stuck (inputHash path, no budget)",
			ChatCompactionStuckEvent{Reason: "idempotent_compaction", MessageCount: 3, InputHash: "abc"},
			[]string{"inputHash", "messageCount", "reason"},
		},
		{
			"chat.context_overflow_unrecoverable",
			ChatContextOverflowEvent{Model: "m", MessageCount: 3, Attempts: 4, Error: "e"},
			[]string{"attempts", "error", "messageCount", "model"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			gotKeys := make([]string, 0, len(got))
			for k := range got {
				gotKeys = append(gotKeys, k)
			}
			sort.Strings(gotKeys)
			if len(gotKeys) != len(tc.wantKeys) {
				t.Fatalf("key count: got %v, want %v", gotKeys, tc.wantKeys)
			}
			for i, k := range tc.wantKeys {
				if gotKeys[i] != k {
					t.Errorf("key set mismatch: got %v, want %v", gotKeys, tc.wantKeys)
					break
				}
			}
		})
	}
}

// TestSessionsChangedDeltaMsValue guards the one non-string field whose value
// (not just presence) the merged path depends on.
func TestSessionsChangedDeltaMsValue(t *testing.T) {
	data, err := json.Marshal(SessionsChangedEvent{SessionKey: "k", Reason: "merged", Status: "running", DeltaMs: 1234})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		DeltaMs int64 `json:"deltaMs"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.DeltaMs != 1234 {
		t.Errorf("deltaMs: got %d, want 1234", got.DeltaMs)
	}
}
