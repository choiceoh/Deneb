package genesis

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Confirmed exploit seeds (RSI P1.5 ④, verifier fuzzing 2606.01066): each of
// these previously made the deterministic gate structurally wrong — an
// unfixable assertion pinned OriginalScore below 100 and the min-delta rule
// then rejected EVERY future candidate of that skill (the wedge), or handed
// out free passes. Checked in as regressions so the gate can't re-grow them.
func TestScoreSkillValidationCasesIgnoresEmptyAssertionsAndClearsWedge(t *testing.T) {
	body := "# 제목\n\n본문 내용"
	mk := func(req, forb, head []string) []SkillValidationCaseRecord {
		return []SkillValidationCaseRecord{{SkillName: "sk", RequiredSubstrings: req, ForbiddenSubstrings: forb, RequiredHeadings: head}}
	}
	cases := []struct {
		name string
		tcs  []SkillValidationCaseRecord
	}{
		{"empty forbidden wedge", mk(nil, []string{""}, nil)},
		{"whitespace forbidden wedge", mk(nil, []string{"   \t "}, nil)},
		{"empty required free pass", mk([]string{""}, nil, nil)},
		{"empty heading wedge", mk(nil, nil, []string{"  "})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := scoreSkillValidationCases(body, tc.tcs)
			if s.Total != 0 {
				t.Fatalf("non-discriminative assertion counted toward Total: %+v", s)
			}
			if s.Skipped == 0 {
				t.Fatalf("isolation not reported: %+v", s)
			}
		})
	}

	// The wedge scenario end-to-end: with one unfixable assertion plus one
	// legitimate improvement, the candidate must now be able to clear the
	// min-delta gate (before the fix, OriginalScore was pinned <100 by the
	// empty forbidden and every candidate was rejected forever).
	tcs := []SkillValidationCaseRecord{{
		SkillName:           "sk",
		RequiredSubstrings:  []string{"회상 단계"},
		ForbiddenSubstrings: []string{""},
	}}
	orig := scoreSkillValidationCases("본문", tcs)
	cand := scoreSkillValidationCases("본문 + 회상 단계", tcs)
	if orig.percent() != 0 || cand.percent() != 100 {
		t.Fatalf("wedge not cleared: orig=%v cand=%v", orig.percent(), cand.percent())
	}
}

// Metamorphic/structural invariants the scorer must hold for ANY input —
// the optimization pressure of the K-candidate selector will find whatever
// these would miss, so they are fuzzed, not just spot-checked.
func checkScoreInvariants(t *testing.T, body string, tcs []SkillValidationCaseRecord) {
	t.Helper()
	s := scoreSkillValidationCases(body, tcs)
	if s.Passed < 0 || s.Total < 0 || s.Passed > s.Total {
		t.Fatalf("invariant Passed<=Total violated: %+v", s)
	}
	if p := s.percent(); p < 0 || p > 100 {
		t.Fatalf("Percent out of range: %v", p)
	}
	if again := scoreSkillValidationCases(body, tcs); again.Passed != s.Passed || again.Total != s.Total {
		t.Fatalf("scorer nondeterministic")
	}
	// Adding a non-discriminative assertion must not move Total/Passed.
	padded := make([]SkillValidationCaseRecord, len(tcs))
	copy(padded, tcs)
	padded = append(padded, SkillValidationCaseRecord{SkillName: "sk", ForbiddenSubstrings: []string{" "}, RequiredSubstrings: []string{""}})
	sp := scoreSkillValidationCases(body, padded)
	if sp.Passed != s.Passed || sp.Total != s.Total {
		t.Fatalf("empty-assertion padding moved the score: %+v vs %+v", sp, s)
	}
}

func FuzzScoreSkillValidationCases(f *testing.F) {
	f.Add("# 제목\n본문", "회상", "eval(", "제목")
	f.Add("", "", "", "")
	f.Add("본문\x00깨진", "  ", "\t", " # ")
	f.Add(strings.Repeat("가", 500), "가가", "나", "가")
	f.Fuzz(func(t *testing.T, body, req, forb, head string) {
		if !utf8.ValidString(body) || !utf8.ValidString(req) || !utf8.ValidString(forb) || !utf8.ValidString(head) {
			t.Skip()
		}
		tcs := []SkillValidationCaseRecord{{
			SkillName:           "sk",
			RequiredSubstrings:  []string{req},
			ForbiddenSubstrings: []string{forb},
			RequiredHeadings:    []string{head},
		}}
		checkScoreInvariants(t, body, tcs)
	})
}

func FuzzNormalizedValidationText(f *testing.F) {
	f.Add("정상 텍스트")
	f.Add("")
	f.Add("  공백\t탭\n개행  ")
	f.Add("\x00\xff비정상")
	f.Fuzz(func(t *testing.T, s string) {
		out := normalizedValidationText(s) // must never panic
		if strings.TrimSpace(out) != out {
			t.Fatalf("normalized text not trimmed: %q", out)
		}
	})
}

func FuzzCrossSkillRegression(f *testing.F) {
	f.Add("이웃 본문", "금지어")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, body, forb string) {
		if !utf8.ValidString(body) || !utf8.ValidString(forb) {
			t.Skip()
		}
		r := crossSkillRegression("n", body, []SkillValidationCaseRecord{{SkillName: "sk", ForbiddenSubstrings: []string{forb}}})
		if r.Passed > r.Total {
			t.Fatalf("cross-regression invariant: %+v", r)
		}
		if r.Failed != (r.Total > 0 && r.Passed < r.Total) {
			t.Fatalf("Failed flag inconsistent: %+v", r)
		}
	})
}
