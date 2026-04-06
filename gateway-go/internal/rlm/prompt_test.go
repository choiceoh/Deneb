package rlm

import (
	"strings"
	"testing"
)

func TestDataAccessPrinciples_Korean(t *testing.T) {
	text := DataAccessPrinciples()
	if !strings.Contains(text, "데이터 접근 원칙") {
		t.Error("expected Korean heading in DataAccessPrinciples")
	}
	if !strings.Contains(text, "projects_list") {
		t.Error("expected projects_list tool reference")
	}
}

func TestSubLLMPrinciples_Korean(t *testing.T) {
	text := SubLLMPrinciples()
	if !strings.Contains(text, "서브 LLM 활용") {
		t.Error("expected Korean heading in SubLLMPrinciples")
	}
	if !strings.Contains(text, "llm_spawn_batch") {
		t.Error("expected llm_spawn_batch tool reference")
	}
}

func TestSubAgentSystemPrompt_Compact(t *testing.T) {
	text := SubAgentSystemPrompt()
	if !strings.Contains(text, "한국어로 답변") {
		t.Error("expected Korean instruction in SubAgentSystemPrompt")
	}
	// Sub-agent prompt should be compact (under 500 chars).
	if len(text) > 500 {
		t.Errorf("SubAgentSystemPrompt too long: %d chars (want <500)", len(text))
	}
}
