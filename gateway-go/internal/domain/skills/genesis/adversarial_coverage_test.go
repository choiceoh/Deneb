package genesis

import (
	"strings"
	"testing"
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
func TestProbeStructuralCoverageGaps(t *testing.T) {
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

func TestDropSection(t *testing.T) {
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
