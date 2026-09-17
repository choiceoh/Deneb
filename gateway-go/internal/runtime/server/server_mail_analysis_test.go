package server

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
)

func TestIncompleteMailAgentSynthesis(t *testing.T) {
	if !incompleteMailAgentSynthesis(nil) {
		t.Fatal("nil result must be incomplete")
	}
	timeout := &chat.SyncResult{
		StopReason:      "timeout",
		Text:            "응답 생성이 시간 초과로 중단됐어요.",
		AllText:         "응답 생성이 시간 초과로 중단됐어요.",
		DeliverableText: "응답 생성이 시간 초과로 중단됐어요.",
	}
	if !incompleteMailAgentSynthesis(timeout) {
		t.Fatal("timeout notice must not count as a finished analysis")
	}
	if err := incompleteMailAgentError(timeout); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("incomplete error = %v", err)
	}
	ok := &chat.SyncResult{StopReason: "end_turn", Text: "분석 본문", AllText: "분석 본문", DeliverableText: "분석 본문"}
	if incompleteMailAgentSynthesis(ok) {
		t.Fatal("end_turn with text must be complete")
	}
	empty := &chat.SyncResult{StopReason: "end_turn"}
	if !incompleteMailAgentSynthesis(empty) {
		t.Fatal("empty end_turn must be incomplete")
	}
}
