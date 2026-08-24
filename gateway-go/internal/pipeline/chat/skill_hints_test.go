package chat

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func hintSkills() []skills.PromptSkill {
	return []skills.PromptSkill{
		{
			Name:          "contract-review",
			Description:   "계약서·약정서·발주서 등 문서를 고정 조항 체크리스트로 검토해 우리에게 불리한 위험 조항을 빠짐없이 드러낸다 — 조항별 존재/부재를 강제한다. Use when: 계약 검토.",
			Triggers:      []string{"계약서", "독소조항", "공급계약"},
			RequiresTools: []string{"graphify"},
			Body:          "# Contract Review\n\n## Completion\n- 위험 조항\n- 권고",
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

// TestBuildSkillHints_MatchAndFormat: a trigger hit injects the bounded body
// directly, without relying on a model-initiated read hop.
func TestBuildSkillHints_MatchAndFormat(t *testing.T) {
	out, names, loaded := buildSkillHints(RunParams{SessionKey: "client:main", Message: "이 계약서 검토해줘"}, "", hintSkills())
	if out == "" {
		t.Fatal("expected a hint for 계약서")
	}
	if len(names) != 1 || names[0] != "contract-review" {
		t.Errorf("hinted names = %v, want [contract-review]", names)
	}
	if !strings.Contains(out, "contract-review") {
		t.Errorf("hint missing skill name:\n%s", out)
	}
	if len(loaded) != 1 || loaded[0] != "contract-review" {
		t.Errorf("auto-loaded names = %v, want [contract-review]", loaded)
	}
	if !strings.Contains(out, "## Completion") || strings.Contains(out, `skills(action="read"`) {
		t.Errorf("hint should carry the body without a read hop:\n%s", out)
	}
	if strings.Contains(out, "fetch_tools") || strings.Contains(out, "Required tools:") {
		t.Errorf("auto-loaded capability must not ask the model for a fetch hop:\n%s", out)
	}
	// Unrelated skills must not ride along.
	if strings.Contains(out, "meeting-minutes") || strings.Contains(out, "github") {
		t.Errorf("unrelated skills hinted:\n%s", out)
	}
}

// TestBuildSkillHintsTruncatesAtCap: more matches than the cap keeps the
// longest-trigger (most specific) ones.
func TestBuildSkillHintsTruncatesAtCap(t *testing.T) {
	msg := "회의록이랑 계약서, 그리고 팩트체크까지 — 독소조항 있는지 확실해?"
	out, names, _ := buildSkillHints(RunParams{SessionKey: "client:main", Message: msg}, "", hintSkills())
	if out == "" || len(names) == 0 || len(names) > maxSkillHints {
		t.Fatalf("expected capped hints, names=%v", names)
	}
	// contract-review matched "독소조항" (4 runes) — must survive the cap.
	if !strings.Contains(out, "contract-review") {
		t.Errorf("longest-trigger skill dropped by cap:\n%s", out)
	}
}

// TestBuildSkillHintsFallsBackForUnavailableBody: oversized bodies retain the
// explicit read path; instructions are never silently truncated.
func TestBuildSkillHintsFallsBackForUnavailableBody(t *testing.T) {
	resolved := []skills.PromptSkill{{
		Name:        "large-skill",
		Description: "large procedure — details",
		Triggers:    []string{"큰절차"},
		Body:        strings.Repeat("x", maxAutoLoadedSkillBodyBytes+1),
	}}
	out, names, loaded := buildSkillHints(RunParams{SessionKey: "client:main", Message: "큰절차 실행"}, "", resolved)
	if len(names) != 1 || len(loaded) != 0 {
		t.Fatalf("names=%v loaded=%v, want one match and no auto-load", names, loaded)
	}
	if !strings.Contains(out, `skills(action="read", name="large-skill")`) {
		t.Errorf("oversized skill missing read fallback:\n%s", out)
	}
}

func TestBuildSkillHintsCapsAggregateBodyBytes(t *testing.T) {
	resolved := []skills.PromptSkill{
		{Name: "alpha", Description: "alpha", Triggers: []string{"알파절차"}, Body: strings.Repeat("a", 11_000)},
		{Name: "beta", Description: "beta", Triggers: []string{"베타절차"}, Body: strings.Repeat("b", 11_000)},
	}
	out, names, loaded := buildSkillHints(RunParams{SessionKey: "client:main", Message: "알파절차 베타절차"}, "", resolved)
	if len(names) != 2 || len(loaded) != 1 || loaded[0] != "alpha" {
		t.Fatalf("names=%v loaded=%v, want two matches and one deterministic load", names, loaded)
	}
	if !strings.Contains(out, `skills(action="read", name="beta")`) {
		t.Errorf("aggregate overflow missing read fallback")
	}
}

// TestBuildSkillHintsReturnsEmptyWhenGated: system sessions (skill-review forks would
// self-trigger on SKILL.md bodies), ephemeral/recall-suppressed runs, empty
// messages, triggerless catalogs, and skills-tool-less presets all yield no
// hint.
func TestBuildSkillHintsReturnsEmptyWhenGated(t *testing.T) {
	cases := []struct {
		name   string
		params RunParams
		preset string
	}{
		{"system session", RunParams{SessionKey: "system:skill-review:client:main", Message: "계약서 검토"}, ""},
		{"ephemeral", RunParams{SessionKey: "client:main", Message: "계약서 검토", EphemeralUser: true}, ""},
		{"skip recall", RunParams{SessionKey: "client:main", Message: "계약서 검토", SkipRecall: true}, ""},
		{"empty message", RunParams{SessionKey: "client:main", Message: "  "}, ""},
		{"no match", RunParams{SessionKey: "client:main", Message: "오늘 날씨 어때"}, ""},
		// btw:* side questions run with the "conversation" preset, whose
		// allow-list has no skills tool — the hinted call would be rejected.
		{"conversation preset (btw)", RunParams{SessionKey: "btw:abc123", Message: "계약서 검토"}, "conversation"},
	}
	for _, tc := range cases {
		if out, names, loaded := buildSkillHints(tc.params, tc.preset, hintSkills()); out != "" || len(names) != 0 || len(loaded) != 0 {
			t.Errorf("%s: expected no hint, got:\n%s", tc.name, out)
		}
	}
	if out, _, _ := buildSkillHints(RunParams{SessionKey: "client:main", Message: "계약서"}, "", nil); out != "" {
		t.Errorf("nil catalog: expected no hint, got:\n%s", out)
	}
	// The self-review preset DOES allow the skills tool — the preset gate must
	// key on the allow-list, not blanket-suppress preset runs.
	if out, _, _ := buildSkillHints(RunParams{SessionKey: "client:main", Message: "계약서 검토"}, "self-review", hintSkills()); out == "" {
		t.Error("self-review preset allows the skills tool; hint should fire")
	}
}

// TestSkillHintSummaryTruncatesAtSeparatorAndCap: cut at the EARLIEST separator, cap at 90 runes.
func TestSkillHintSummaryTruncatesAtSeparatorAndCap(t *testing.T) {
	if got := skillHintSummary("짧은 설명 — 부연. Use when: x."); got != "짧은 설명" {
		t.Errorf("earliest-separator cut failed: %q", got)
	}
	long := strings.Repeat("가", 120)
	if got := skillHintSummary(long); len([]rune(got)) != 91 { // 90 + ellipsis
		t.Errorf("cap failed: %d runes", len([]rune(got)))
	}
}

// TestBuildTailAdditionsPreservesHintPosition: the hint rides after reference
// material and before the directives, in both the recall and notebook branches
// (skill context is orthogonal to reference material, so unlike
// recall/feed it is NOT suppressed by notebook grounding).
func TestBuildTailAdditionsPreservesHintPosition(t *testing.T) {
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

func (f *hintFakeRecorder) RecordSkillUse(sessionKey, skillName string, success bool, errMsg, _ string) {
	f.calls++
}

// TestRecordRunSkillUsageIgnoresReviewForks: skill-review fork sessions read
// skills to judge them — that must not be recorded as usage (it inflated
// consult counts and wrote spurious failure rows in production).
func TestRecordRunSkillUsageIgnoresReviewForks(t *testing.T) {
	rec := &hintFakeRecorder{}
	log := NewSkillConsultLog()
	log.Add("topsolar-db")
	recordRunSkillUsage(rec, log, nil, nil, "system:skill-review:cron:email:123", "m1", nil)
	if rec.calls != 0 {
		t.Fatalf("review-fork consult recorded as usage (%d calls)", rec.calls)
	}

	// A real session still records.
	log2 := NewSkillConsultLog()
	log2.Add("topsolar-db")
	recordRunSkillUsage(rec, log2, &agent.AgentResult{Text: "done"}, nil, "client:main", "m1", nil)
	if rec.calls != 1 {
		t.Fatalf("real-session consult not recorded (%d calls)", rec.calls)
	}
}
