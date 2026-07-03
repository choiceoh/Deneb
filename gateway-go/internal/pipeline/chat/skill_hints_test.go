package chat

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func hintSkills() []skills.PromptSkill {
	return []skills.PromptSkill{
		{
			Name:        "contract-review",
			Description: "계약서·약정서·발주서 등 문서를 고정 조항 체크리스트로 검토해 우리에게 불리한 위험 조항을 빠짐없이 드러낸다 — 조항별 존재/부재를 강제한다. Use when: 계약 검토.",
			Triggers:    []string{"계약서", "독소조항", "공급계약"},
		},
		{
			Name:        "meeting-minutes",
			Description: "회의·통화·논의 녹음의 전사본을 회의록으로 정리하고 업무 관점에서 분석한다. Use when: 회의 녹음.",
			Triggers:    []string{"회의록", "녹취"},
		},
		{
			Name:        "fact-check",
			Description: "응답에 담을 사실 주장을 도구로 독립 재확인한다. Use when: 팩트체크.",
			Triggers:    []string{"팩트체크", "확실해?"},
		},
		// No triggers — must never hint regardless of message content.
		{Name: "github", Description: "GitHub operations via gh CLI."},
	}
}

// TestBuildSkillHints_MatchAndFormat: a trigger hit produces the hint block
// with the skill name, first-clause summary, and an explicit read call.
func TestBuildSkillHints_MatchAndFormat(t *testing.T) {
	out := buildSkillHints(RunParams{SessionKey: "client:main", Message: "이 계약서 검토해줘"}, hintSkills())
	if out == "" {
		t.Fatal("expected a hint for 계약서")
	}
	if !strings.Contains(out, "contract-review") {
		t.Errorf("hint missing skill name:\n%s", out)
	}
	if !strings.Contains(out, `skills(action="read", name="contract-review")`) {
		t.Errorf("hint missing explicit read call:\n%s", out)
	}
	// First clause only — the " — " tail of the description must be cut.
	if strings.Contains(out, "존재/부재를 강제") || strings.Contains(out, "Use when") {
		t.Errorf("hint should carry only the first clause of the description:\n%s", out)
	}
	// Unrelated skills must not ride along.
	if strings.Contains(out, "meeting-minutes") || strings.Contains(out, "github") {
		t.Errorf("unrelated skills hinted:\n%s", out)
	}
}

// TestBuildSkillHints_CapAndOrder: more matches than the cap keeps the
// longest-trigger (most specific) ones.
func TestBuildSkillHints_CapAndOrder(t *testing.T) {
	msg := "회의록이랑 계약서, 그리고 팩트체크까지 — 독소조항 있는지 확실해?"
	out := buildSkillHints(RunParams{SessionKey: "client:main", Message: msg}, hintSkills())
	if out == "" {
		t.Fatal("expected hints")
	}
	if n := strings.Count(out, "\n- "); n > maxSkillHints {
		t.Errorf("hints = %d, cap is %d:\n%s", n, maxSkillHints, out)
	}
	// contract-review matched "독소조항" (4 runes) — must survive the cap.
	if !strings.Contains(out, "contract-review") {
		t.Errorf("longest-trigger skill dropped by cap:\n%s", out)
	}
}

// TestBuildSkillHints_Gates: system sessions (skill-review forks would
// self-trigger on SKILL.md bodies), ephemeral/recall-suppressed runs, empty
// messages, and triggerless catalogs all yield no hint.
func TestBuildSkillHints_Gates(t *testing.T) {
	cases := []struct {
		name   string
		params RunParams
	}{
		{"system session", RunParams{SessionKey: "system:skill-review:client:main", Message: "계약서 검토"}},
		{"ephemeral", RunParams{SessionKey: "client:main", Message: "계약서 검토", EphemeralUser: true}},
		{"skip recall", RunParams{SessionKey: "client:main", Message: "계약서 검토", SkipRecall: true}},
		{"empty message", RunParams{SessionKey: "client:main", Message: "  "}},
		{"no match", RunParams{SessionKey: "client:main", Message: "오늘 날씨 어때"}},
	}
	for _, tc := range cases {
		if out := buildSkillHints(tc.params, hintSkills()); out != "" {
			t.Errorf("%s: expected no hint, got:\n%s", tc.name, out)
		}
	}
	if out := buildSkillHints(RunParams{SessionKey: "client:main", Message: "계약서"}, nil); out != "" {
		t.Errorf("nil catalog: expected no hint, got:\n%s", out)
	}
}

// TestSkillHintSummary: cut at the EARLIEST separator, cap at 90 runes.
func TestSkillHintSummary(t *testing.T) {
	if got := skillHintSummary("짧은 설명 — 부연. Use when: x."); got != "짧은 설명" {
		t.Errorf("earliest-separator cut failed: %q", got)
	}
	long := strings.Repeat("가", 120)
	if got := skillHintSummary(long); len([]rune(got)) != 91 { // 90 + ellipsis
		t.Errorf("cap failed: %d runes", len([]rune(got)))
	}
}

// TestBuildTailAdditions_SkillHintPosition: the hint rides after reference
// material and before the directives, in both the recall and notebook branches
// (a procedure pointer is orthogonal to reference material, so unlike
// recall/feed it is NOT suppressed by notebook grounding).
func TestBuildTailAdditions_SkillHintPosition(t *testing.T) {
	adds := buildTailAdditions(RunParams{AutoDeliveredOutput: true}, "recall", "", "힌트")
	if len(adds) != 3 || adds[0] != "recall" || adds[1] != "힌트" {
		t.Fatalf("recall branch adds = %#v", adds)
	}
	adds = buildTailAdditions(RunParams{}, "", "노트북", "힌트")
	if len(adds) != 2 || adds[0] != "노트북" || adds[1] != "힌트" {
		t.Fatalf("notebook branch adds = %#v", adds)
	}
}

// hintFakeRecorder captures RecordSkillUse calls.
type hintFakeRecorder struct{ calls int }

func (f *hintFakeRecorder) RecordSkillUse(sessionKey, skillName string, success bool, errMsg string) {
	f.calls++
}

// TestRecordTurnSkillUsage_SkipsReviewForks: skill-review fork sessions read
// skills to judge them — that must not be recorded as usage (it inflated
// consult counts and wrote spurious failure rows in production).
func TestRecordTurnSkillUsage_SkipsReviewForks(t *testing.T) {
	rec := &hintFakeRecorder{}
	log := NewSkillConsultLog()
	log.Add("topsolar-db")
	recordTurnSkillUsage(rec, log, nil, "system:skill-review:cron:email:123")
	if rec.calls != 0 {
		t.Fatalf("review-fork consult recorded as usage (%d calls)", rec.calls)
	}

	// A real session still records.
	log2 := NewSkillConsultLog()
	log2.Add("topsolar-db")
	recordTurnSkillUsage(rec, log2, []agent.ToolActivity{}, "client:main")
	if rec.calls != 1 {
		t.Fatalf("real-session consult not recorded (%d calls)", rec.calls)
	}
}
