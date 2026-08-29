package generation

import (
	"strings"
	"testing"
)

func TestGenesisPromptUsesOutcomeDrivenSkillContract(t *testing.T) {
	for _, want := range []string{
		"기대 결과와 대상",
		"결과를 실제로 바꾸는 근거/입력",
		"2~4개의 중요한 경계",
		"2~4개의 관찰 가능한 pass/fail 완료 기준",
		"순서 자체가 계약일 때만 번호 순서를 강제",
		"(requires: tool-name)",
	} {
		if !strings.Contains(genesisSystemPrompt, want) {
			t.Errorf("genesis prompt missing outcome contract %q", want)
		}
	}

	for _, stale := range []string{
		"Procedure\" 섹션: 단계별 절차",
		"번호 단계·도구 호출·명령어가 없음",
	} {
		if strings.Contains(genesisSystemPrompt, stale) {
			t.Errorf("genesis prompt still forces incidental ordering %q", stale)
		}
	}
}

// TestDispatchContractOpensWithTheImpactCheck pins the L4 coding lane's first
// step. The dispatched Codex session has no MCP tools, so the contract must
// name the CLI; and it must tell the agent to DECLARE a missing index rather
// than narrate a blast radius it never checked (the "없는 걸 약속" class).
func TestDispatchContractOpensWithTheImpactCheck(t *testing.T) {
	for _, want := range []string{
		"codegraph impact",
		"codegraph node",
		"codegraph sync .",
		"확인한 척하지 마라",
	} {
		if !strings.Contains(dispatchContractPrompt, want) {
			t.Errorf("dispatch contract missing %q", want)
		}
	}
	// First step means first line: the contract is read top-down by a session
	// that may stop reading once it starts working.
	first := strings.SplitN(dispatchContractPrompt, "\n", 3)[1]
	if !strings.Contains(first, "파급 범위") {
		t.Errorf("impact check is not the contract's first bullet, got %q", first)
	}
}
