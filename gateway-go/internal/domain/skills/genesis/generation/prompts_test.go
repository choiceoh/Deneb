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
