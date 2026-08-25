package skills

import "testing"

// A short latin trigger must not match inside a longer word: contract-review's
// "mou" fired on "cumulative amount" five times in 30 days of real messages,
// injecting an 8KB skill body into unrelated turns.
func TestMatchSkillTriggers_ASCIITriggerNeedsWordBoundary(t *testing.T) {
	skills := []PromptSkill{{Name: "contract-review", Triggers: []string{"mou", "계약서"}}}

	for _, msg := range []string{
		"케이원일렉트릭과의 cumulative amount 증가 중",
		"서버를 mount 했어",
		"mousepad 주문",
	} {
		if got := MatchSkillTriggers(msg, skills, 2); len(got) != 0 {
			t.Errorf("단어 안쪽 매칭이 발화함: %q → %v", msg, got)
		}
	}
	for _, msg := range []string{
		"MOU 체결 검토해줘",
		"이번 건 mou 초안이야",
		"(mou)",
		"계약서 검토",
	} {
		if got := MatchSkillTriggers(msg, skills, 2); len(got) != 1 {
			t.Errorf("정상 발화가 막힘: %q → %v", msg, got)
		}
	}
}
