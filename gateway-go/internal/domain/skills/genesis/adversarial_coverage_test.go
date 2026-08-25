package genesis

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

const advBody = `# 배포 스킬

## When to Use
프로덕션 배포가 필요할 때.

## Procedure
1. 게이트를 통과시킨다.
2. 핫스왑한다.

## Verification
health를 확인한다.`

// The probe authors a discriminative RequiredHeadings case for a section whose
// removal the existing case set fails to catch, and stays quiet where coverage
// already bites.
func TestProbeStructuralCoverageGaps_CreatesCasesOnlyForUncoveredSections(t *testing.T) {
	t.Run("uncaught section drop is authored as a heading case", func(t *testing.T) {
		// Existing cases protect only 'Procedure' (a substring) — 'Verification'
		// and 'When to Use' section drops go undetected.
		cases := []SkillValidationCaseRecord{
			{SkillName: "sk", ID: "c1", RequiredSubstrings: []string{"핫스왑"}},
		}
		got := probeStructuralCoverageGaps("sk", advBody, cases)
		if len(got) == 0 {
			t.Fatal("no coverage gap authored despite unprotected sections")
		}
		for _, c := range got {
			if c.Source != "adversarial-coverage" || len(c.RequiredHeadings) != 1 {
				t.Fatalf("malformed authored case: %+v", c)
			}
			// Discriminative: passes on the good body, fails when its section is dropped.
			if !scoreSkillValidationCases(advBody, []SkillValidationCaseRecord{c}).casePasses() {
				t.Fatalf("authored case fails on the good body: %+v", c)
			}
		}
		// 'Procedure' is already protected by the 핫스왑 substring drop → not re-authored.
		for _, c := range got {
			if strings.EqualFold(c.RequiredHeadings[0], "Procedure") {
				t.Fatal("re-authored a section the case set already catches")
			}
		}
	})

	t.Run("tight coverage authors nothing", func(t *testing.T) {
		// Every section required as a heading → any drop is caught.
		cases := []SkillValidationCaseRecord{
			{SkillName: "sk", ID: "c1", RequiredHeadings: []string{"When to Use", "Procedure", "Verification"}},
		}
		if got := probeStructuralCoverageGaps("sk", advBody, cases); len(got) != 0 {
			t.Fatalf("authored cases despite tight coverage: %+v", got)
		}
	})

	t.Run("one-section skill is not probed", func(t *testing.T) {
		if got := probeStructuralCoverageGaps("sk", "# Solo\n\n본문만 있다.", nil); len(got) != 0 {
			t.Fatalf("single-section skill probed: %+v", got)
		}
	})

	t.Run("bounded per skill", func(t *testing.T) {
		var b strings.Builder
		b.WriteString("# Many\n")
		for i := 0; i < 10; i++ {
			fmtSection(&b, i)
		}
		got := probeStructuralCoverageGaps("sk", b.String(), nil)
		if len(got) > adversarialCoverageMaxPerSkill {
			t.Fatalf("probe exceeded per-skill bound: %d", len(got))
		}
	})
}

func TestDropSectionClearsOnlyTargetSection(t *testing.T) {
	headings := extractRawHeadings(advBody)
	var verif rawSkillHeading
	for _, h := range headings {
		if h.normalized == "verification" {
			verif = h
		}
	}
	dropped := dropSection(advBody, verif.line)
	if strings.Contains(dropped, "Verification") || strings.Contains(dropped, "health를 확인") {
		t.Fatalf("Verification section not removed:\n%s", dropped)
	}
	if !strings.Contains(dropped, "Procedure") || !strings.Contains(dropped, "핫스왑") {
		t.Fatal("dropSection removed the wrong section")
	}
}

func fmtSection(b *strings.Builder, i int) {
	b.WriteString("\n## Section")
	b.WriteByte(byte('A' + i))
	b.WriteString("\n내용 줄.\n")
}

const advToolBody = `# 배포 스킬

## Procedure
1. wiki_search 로 관련 위키를 찾는다.
2. mail_archive 로 첨부를 확인한다.
3. 결과를 종합한다.`

// The behavioral probe authors a discriminative RequiredTools case for a tool
// whose removal the existing case set fails to catch, and stays quiet where a
// case already protects the tool.
func TestProbeBehavioralCoverageGaps_CreatesCasesForUncaughtToolDrops(t *testing.T) {
	t.Run("uncaught tool drop is authored as a RequiredTools case", func(t *testing.T) {
		// Only 'mail_archive' is protected (via a required substring); dropping
		// 'wiki_search' goes undetected.
		// The seed carries a REAL user task. The authored coverage case borrows
		// it, which is what makes it behaviorally evaluable — the generator no
		// longer synthesizes an input, because a synthesized one ("exercise the
		// skill's use of tool X") is a meta-instruction the executor refuses.
		cases := []SkillValidationCaseRecord{
			{
				SkillName:          "sk",
				ID:                 "c1",
				RequiredSubstrings: []string{"mail_archive"},
				Replay:             SkillReplayCaseRecord{Input: "이번 주 메일 첨부 정리해줘"},
			},
		}
		got := probeBehavioralCoverageGaps("sk", advToolBody, cases, testKnownTools)
		if len(got) == 0 {
			t.Fatal("no behavioral gap authored despite unprotected tool")
		}
		var names []string
		for _, c := range got {
			if c.Source != "adversarial-coverage" || len(c.Replay.RequiredTools) != 1 {
				t.Fatalf("malformed behavioral case: %+v", c)
			}
			names = append(names, c.Replay.RequiredTools[0])
			// Discriminative: passes on the good body, and — because a real task
			// was available to borrow — the executor gate can pick it up.
			if !scoreSkillValidationCases(advToolBody, []SkillValidationCaseRecord{c}).casePasses() {
				t.Fatalf("authored case fails on the good body: %+v", c)
			}
			if !replayBehaviorEvaluable(c.Replay) {
				t.Fatalf("authored behavioral case is not executor-evaluable: %+v", c)
			}
		}
		if !contains(names, "wiki_search") {
			t.Fatalf("expected wiki_search authored, got %v", names)
		}
		if contains(names, "mail_archive") {
			t.Fatal("re-authored a tool the case set already catches")
		}
	})

	t.Run("tool already required is not re-authored", func(t *testing.T) {
		cases := []SkillValidationCaseRecord{
			{SkillName: "sk", ID: "c1", Replay: SkillReplayCaseRecord{RequiredTools: []string{"wiki_search", "mail_archive"}}},
		}
		if got := probeBehavioralCoverageGaps("sk", advToolBody, cases, testKnownTools); len(got) != 0 {
			t.Fatalf("authored despite protected tools: %+v", got)
		}
	})

	t.Run("prose without snake_case tools yields nothing", func(t *testing.T) {
		if got := probeBehavioralCoverageGaps("sk", "# S\n\n본문에 도구 없음.", nil, testKnownTools); len(got) != 0 {
			t.Fatalf("false tool detected: %+v", got)
		}
	})
}

// testKnownTools is the registry stand-in: only these names are real tools.
var testKnownTools = map[string]struct{}{
	"wiki_search":    {},
	"mail_archive":   {},
	"read_spillover": {},
}

// A SKILL body is full of snake_case that is NOT a tool — parameter names,
// config keys, response fields. Authoring "tool coverage" for those probes
// nothing, so only registry names survive extraction.
func TestExtractToolRefsKeepsOnlyRegisteredTools(t *testing.T) {
	got := extractToolRefs("use wiki_search then Mail_Archive with max_results=5, db_path, no_reply; skip plainword and CamelCase", testKnownTools)
	for _, notTool := range []string{"max_results", "db_path", "no_reply"} {
		if contains(got, notTool) {
			t.Fatalf("parameter name %q extracted as a tool: %v", notTool, got)
		}
	}
	if extractToolRefs("use wiki_search", nil) != nil {
		t.Fatal("no registry wired must author no tool cases, not guess")
	}
	if !contains(got, "wiki_search") || !contains(got, "mail_archive") {
		t.Fatalf("tool extraction = %v", got)
	}
	if contains(got, "plainword") || contains(got, "camelcase") {
		t.Fatalf("non-tool token extracted: %v", got)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Gate-exploit trap (2605.20744): the substring-stuffed candidate must be
// REJECTED by the real preflight — a pass is the alarm condition.
func TestProbeGateExploitTrap_RejectedByPreflight(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	e := NewEvolver(nil, skills.NewCatalog(nil), tr, "", nil)
	original := strings.Repeat("# 스킬\n\n## 절차\n실제 내용이 담긴 문단이다. ", 30)
	cases := []SkillValidationCaseRecord{{
		SkillName:          "sk",
		RequiredSubstrings: []string{"필수 문구 하나", "필수 문구 둘"},
	}}
	if err := tr.RecordSkillValidationCase(cases[0]); err != nil {
		t.Fatal(err)
	}
	exploitable, _ := probeGateExploitTrap(e, "sk", original, cases)
	if exploitable {
		t.Fatal("substring-stuffed trap must NOT clear the deterministic preflight")
	}
	// No required substrings anywhere → no trap to build → never exploitable.
	if got, _ := probeGateExploitTrap(e, "sk", original, nil); got {
		t.Fatal("empty case set must yield no trap")
	}
}
