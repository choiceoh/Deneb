package guardrails

import (
	"reflect"
	"strings"
	"testing"
)

func TestAuditEmptyAndPtrMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		target  string
		surface string
		change  string
		risk    string
		empty   bool
	}{
		{
			name:    "zero",
			target:  "",
			surface: "",
			change:  "",
			risk:    "",
			empty:   true,
		},
		{
			name:    "target-only",
			target:  "sig",
			surface: "",
			change:  "",
			risk:    "",
			empty:   false,
		},
		{
			name:    "surface-only",
			target:  "",
			surface: "procedure",
			change:  "",
			risk:    "",
			empty:   false,
		},
		{
			name:    "behavior-only",
			target:  "",
			surface: "",
			change:  "change",
			risk:    "",
			empty:   false,
		},
		{
			name:    "risk-only",
			target:  "",
			surface: "",
			change:  "",
			risk:    "risk",
			empty:   false,
		},
		{
			name:    "all",
			target:  "sig",
			surface: "procedure",
			change:  "change",
			risk:    "risk",
			empty:   false,
		},
		{
			name:    "spaces",
			target:  "   ",
			surface: "\t",
			change:  "\n",
			risk:    "  ",
			empty:   true,
		},
		{
			name:    "trimmed",
			target:  " sig ",
			surface: " procedure ",
			change:  " change ",
			risk:    " risk ",
			empty:   false,
		},
		{
			name:    "unicode",
			target:  "오류",
			surface: "절차",
			change:  "변경",
			risk:    "위험",
			empty:   false,
		},
		{
			name:    "punctuation",
			target:  "terminal=timeout|mechanism=x",
			surface: "pitfalls",
			change:  "retry",
			risk:    "low",
			empty:   false,
		},
		{
			name:    "newline-target",
			target:  "\nfoo\n",
			surface: "",
			change:  "",
			risk:    "",
			empty:   false,
		},
		{
			name:    "tab-surface",
			target:  "",
			surface: "\tprocedure\t",
			change:  "",
			risk:    "",
			empty:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			audit := Audit{TargetSignature: tc.target, EditedSurface: tc.surface, ExpectedBehaviorChange: tc.change, RegressionRisk: tc.risk}
			if got := audit.Empty(); got != tc.empty {
				t.Fatalf("Empty() = %v, want %v", got, tc.empty)
			}
			ptr := audit.Ptr()
			if tc.empty && ptr != nil {
				t.Fatalf("Ptr() = %#v, want nil", ptr)
			}
			if !tc.empty && (ptr == nil || !reflect.DeepEqual(*ptr, audit)) {
				t.Fatalf("Ptr() = %#v, want copy of %#v", ptr, audit)
			}
		})
	}
	if (Audit{PrimaryDimension: "D4-orchestration"}).Empty() {
		t.Fatal("primary harness dimension should make an audit non-empty")
	}
	if (Audit{SecondaryDimensions: []string{"D6-output-processing"}}).Empty() {
		t.Fatal("secondary harness dimensions should make an audit non-empty")
	}
}

func TestNormalizeSignatureMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, input, want string }{
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "spaces",
			input: "   ",
			want:  "",
		},
		{
			name:  "trim",
			input: "  Terminal=Timeout  ",
			want:  "terminal=timeout",
		},
		{
			name:  "lower",
			input: "TERMINAL=TIMEOUT",
			want:  "terminal=timeout",
		},
		{
			name:  "eq-left",
			input: "terminal =timeout",
			want:  "terminal=timeout",
		},
		{
			name:  "eq-right",
			input: "terminal= timeout",
			want:  "terminal=timeout",
		},
		{
			name:  "eq-both",
			input: "terminal = timeout",
			want:  "terminal=timeout",
		},
		{
			name:  "pipe-left",
			input: "a |b",
			want:  "a|b",
		},
		{
			name:  "pipe-right",
			input: "a| b",
			want:  "a|b",
		},
		{
			name:  "pipe-both",
			input: "a | b",
			want:  "a|b",
		},
		{
			name:  "many-spaces",
			input: "A   B   C",
			want:  "a b c",
		},
		{
			name:  "tabs",
			input: "A\tB",
			want:  "a b",
		},
		{
			name:  "newlines",
			input: "A\nB",
			want:  "a b",
		},
		{
			name:  "full",
			input: " Terminal = Timeout | Mechanism = Retry ",
			want:  "terminal=timeout|mechanism=retry",
		},
		{
			name:  "unicode",
			input: " 오류 = 시간초과 | 기제 = 재시도 ",
			want:  "오류=시간초과|기제=재시도",
		},
		{
			name:  "double-pipe",
			input: "a  |  b  |  c",
			want:  "a|b|c",
		},
		{
			name:  "value-spaces",
			input: "cause = missing artifact",
			want:  "cause=missing artifact",
		},
		{
			name:  "already",
			input: "terminal=other|mechanism=x",
			want:  "terminal=other|mechanism=x",
		},
		{
			name:  "mixed-case",
			input: "TeRmInAl = Auth",
			want:  "terminal=auth",
		},
		{
			name:  "trailing",
			input: "a | ",
			want:  "a|",
		},
		{
			name:  "leading",
			input: " | a",
			want:  "|a",
		},
		{
			name:  "equals-only",
			input: " = ",
			want:  "=",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeSignature(tc.input); got != tc.want {
				t.Fatalf("NormalizeSignature(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSignatureMatchesReturnsBidirectionalSubstringContainment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, target, supported string
		want                    bool
	}{
		{
			name:      "both-empty",
			target:    "",
			supported: "",
			want:      false,
		},
		{
			name:      "target-empty",
			target:    "",
			supported: "a",
			want:      false,
		},
		{
			name:      "supported-empty",
			target:    "a",
			supported: "",
			want:      false,
		},
		{
			name:      "equal",
			target:    "a",
			supported: "a",
			want:      true,
		},
		{
			name:      "target-contains",
			target:    "terminal=timeout|mechanism=retry",
			supported: "terminal=timeout",
			want:      true,
		},
		{
			name:      "supported-contains",
			target:    "terminal=timeout",
			supported: "terminal=timeout|mechanism=retry",
			want:      true,
		},
		{
			name:      "disjoint",
			target:    "timeout",
			supported: "auth",
			want:      false,
		},
		{
			name:      "prefix",
			target:    "abc",
			supported: "ab",
			want:      true,
		},
		{
			name:      "suffix",
			target:    "bc",
			supported: "abc",
			want:      true,
		},
		{
			name:      "case-sensitive",
			target:    "ABC",
			supported: "abc",
			want:      false,
		},
		{
			name:      "space-sensitive",
			target:    "a b",
			supported: "ab",
			want:      false,
		},
		{
			name:      "pipe",
			target:    "a|b",
			supported: "a",
			want:      true,
		},
		{
			name:      "unicode",
			target:    "시간초과-재시도",
			supported: "시간초과",
			want:      true,
		},
		{
			name:      "near",
			target:    "terminal=timeout",
			supported: "terminal=timedout",
			want:      false,
		},
		{
			name:      "single",
			target:    "x",
			supported: "x",
			want:      true,
		},
		{
			name:      "zero-char",
			target:    "0",
			supported: "0",
			want:      true,
		},
		{
			name:      "punct",
			target:    "a=b",
			supported: "a=",
			want:      true,
		},
		{
			name:      "reverse-punct",
			target:    "a=",
			supported: "a=b",
			want:      true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := SignatureMatches(tc.target, tc.supported); got != tc.want {
				t.Fatalf("SignatureMatches(%q, %q) = %v, want %v", tc.target, tc.supported, got, tc.want)
			}
		})
	}
}

func TestCanonicalSkillSurfaceNormalizesAliasesToCanonicalNames(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, input, want string }{
		{
			name:  "procedure",
			input: "procedure",
			want:  "procedure",
		},
		{
			name:  "workflow",
			input: "workflow",
			want:  "procedure",
		},
		{
			name:  "workflow-guide",
			input: "workflow-guide",
			want:  "procedure",
		},
		{
			name:  "steps",
			input: "steps",
			want:  "procedure",
		},
		{
			name:  "step by step",
			input: "step by step",
			want:  "procedure",
		},
		{
			name:  "절차",
			input: "절차",
			want:  "procedure",
		},
		{
			name:  "작업 흐름",
			input: "작업 흐름",
			want:  "procedure",
		},
		{
			name:  "pitfalls",
			input: "pitfalls",
			want:  "pitfalls",
		},
		{
			name:  "pitfall",
			input: "pitfall",
			want:  "pitfalls",
		},
		{
			name:  "gotchas",
			input: "gotchas",
			want:  "pitfalls",
		},
		{
			name:  "caution",
			input: "caution",
			want:  "pitfalls",
		},
		{
			name:  "주의 사항",
			input: "주의 사항",
			want:  "pitfalls",
		},
		{
			name:  "위험",
			input: "위험",
			want:  "pitfalls",
		},
		{
			name:  "verification",
			input: "verification",
			want:  "verification",
		},
		{
			name:  "verify",
			input: "verify",
			want:  "verification",
		},
		{
			name:  "verification checklist",
			input: "verification checklist",
			want:  "verification",
		},
		{
			name:  "검증",
			input: "검증",
			want:  "verification",
		},
		{
			name:  "확인 항목",
			input: "확인 항목",
			want:  "verification",
		},
		{
			name:  "when to use",
			input: "when to use",
			want:  "when to use",
		},
		{
			name:  "usage",
			input: "usage",
			want:  "when to use",
		},
		{
			name:  "사용 시점",
			input: "사용 시점",
			want:  "when to use",
		},
		{
			name:  "custom",
			input: "custom",
			want:  "custom",
		},
		{
			name:  "metadata",
			input: "metadata",
			want:  "metadata",
		},
		{
			name:  "body",
			input: "body",
			want:  "body",
		},
		{
			name:  "Procedure",
			input: "Procedure",
			want:  "Procedure",
		},
		{
			name:  "spaced custom",
			input: "spaced custom",
			want:  "spaced custom",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "tooling",
			input: "tooling",
			want:  "tooling",
		},
		{
			name:  "preconditions",
			input: "preconditions",
			want:  "preconditions",
		},
		{
			name:  "workflow pitfalls",
			input: "workflow pitfalls",
			want:  "procedure",
		},
		{
			name:  "verification workflow",
			input: "verification workflow",
			want:  "procedure",
		},
		{
			name:  "usage notes",
			input: "usage notes",
			want:  "when to use",
		},
		{
			name:  "cautions",
			input: "cautions",
			want:  "pitfalls",
		},
		{
			name:  "step verification",
			input: "step verification",
			want:  "procedure",
		},
		{
			name:  "확인 절차",
			input: "확인 절차",
			want:  "procedure",
		},
		{
			name:  "사용 절차",
			input: "사용 절차",
			want:  "procedure",
		},
		{
			name:  "risk",
			input: "risk",
			want:  "risk",
		},
		{
			name:  "examples",
			input: "examples",
			want:  "examples",
		},
		{
			name:  "reference",
			input: "reference",
			want:  "reference",
		},
		{
			name:  "troubleshooting",
			input: "troubleshooting",
			want:  "troubleshooting",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CanonicalSkillSurface(tc.input); got != tc.want {
				t.Fatalf("CanonicalSkillSurface(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizedSkillHeadingsMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, document string
		want           []string
	}{
		{
			name:     "empty",
			document: "",
			want:     []string{},
		},
		{
			name:     "plain",
			document: "body only",
			want:     []string{},
		},
		{
			name:     "h1",
			document: "# Title",
			want:     []string{"title"},
		},
		{
			name:     "h2",
			document: "## Procedure",
			want:     []string{"procedure"},
		},
		{
			name:     "multi",
			document: "# Title\n## Procedure\n### Verify",
			want:     []string{"title", "procedure", "verify"},
		},
		{
			name:     "duplicate-case",
			document: "# Title\n## title\n# TITLE",
			want:     []string{"title"},
		},
		{
			name:     "spaces",
			document: "#   Many   Spaces",
			want:     []string{"many spaces"},
		},
		{
			name:     "empty-heading",
			document: "#\nbody",
			want:     []string{},
		},
		{
			name:     "hashes",
			document: "#### Deep",
			want:     []string{"deep"},
		},
		{
			name:     "indented",
			document: "  ## Procedure  ",
			want:     []string{"procedure"},
		},
		{
			name:     "not-heading",
			document: "text # inline",
			want:     []string{},
		},
		{
			name:     "unicode",
			document: "# 절차\n## 검증",
			want:     []string{"절차", "검증"},
		},
		{
			name:     "duplicates",
			document: "# A\n## B\n### A\n#### b",
			want:     []string{"a", "b"},
		},
		{
			name:     "tabs",
			document: "# A\tB",
			want:     []string{"a b"},
		},
		{
			name:     "frontmatter",
			document: "---\nname: x\n---\n# Title",
			want:     []string{"title"},
		},
		{
			name:     "mixed",
			document: "## When To Use\n## PITFALLS",
			want:     []string{"when to use", "pitfalls"},
		},
		{
			name:     "hash-in-body",
			document: "#Title",
			want:     []string{"title"},
		},
		{
			name:     "blank-between",
			document: "# A\n\n## B",
			want:     []string{"a", "b"},
		},
		{
			name:     "numeric",
			document: "# 123\n## 456",
			want:     []string{"123", "456"},
		},
		{
			name:     "punctuation",
			document: "# A:B\n## C/D",
			want:     []string{"a:b", "c/d"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizedSkillHeadings(tc.document)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("NormalizedSkillHeadings() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func guardrailDocument(change string) (string, string) {
	original := "# Skill\n\n## Procedure\nold procedure\n\n## Pitfalls\nold pitfall\n\n## Verification\nold verification\n\n## When to Use\nold usage\n\n## Examples\nold example\n"
	candidate := original
	replace := func(from, to string) { candidate = strings.Replace(candidate, from, to, 1) }
	switch change {
	case "procedure":
		replace("old procedure", "new procedure")
	case "pitfalls":
		replace("old pitfall", "new pitfall")
	case "verification":
		replace("old verification", "new verification")
	case "when to use":
		replace("old usage", "new usage")
	case "examples":
		replace("old example", "new example")
	case "procedure+pitfalls":
		replace("old procedure", "new procedure")
		replace("old pitfall", "new pitfall")
	case "procedure+verification":
		replace("old procedure", "new procedure")
		replace("old verification", "new verification")
	}
	return original, candidate
}

func TestValidateEditedSurfaceAcceptsEditableSurfacesAndRejectsProtectedOnes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, surface, change string
		wantOK                bool
		reason                string
	}{
		{
			name:    "procedure-change",
			surface: "procedure",
			change:  "procedure",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "workflow-alias",
			surface: "workflow",
			change:  "procedure",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "steps-alias",
			surface: "steps",
			change:  "procedure",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "korean-procedure",
			surface: "절차",
			change:  "procedure",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "pitfalls-change",
			surface: "pitfalls",
			change:  "pitfalls",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "gotcha-alias",
			surface: "gotcha",
			change:  "pitfalls",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "caution-alias",
			surface: "caution",
			change:  "pitfalls",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "verification-change",
			surface: "verification",
			change:  "verification",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "verify-alias",
			surface: "verify",
			change:  "verification",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "when-use",
			surface: "when to use",
			change:  "when to use",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "usage-alias",
			surface: "usage",
			change:  "when to use",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "body-any-change",
			surface: "body",
			change:  "procedure",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "skill-body",
			surface: "skill body",
			change:  "pitfalls",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "skill-md",
			surface: "skill.md",
			change:  "verification",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "metadata-rejected",
			surface: "metadata",
			change:  "procedure",
			wantOK:  false,
			reason:  "not editable",
		},
		{
			name:    "frontmatter-rejected",
			surface: "frontmatter",
			change:  "procedure",
			wantOK:  false,
			reason:  "not editable",
		},
		{
			name:    "support-file-rejected",
			surface: "support-file",
			change:  "procedure",
			wantOK:  false,
			reason:  "not editable",
		},
		{
			name:    "support-space-rejected",
			surface: "support file",
			change:  "procedure",
			wantOK:  false,
			reason:  "not editable",
		},
		{
			name:    "tool-rejected",
			surface: "tool",
			change:  "procedure",
			wantOK:  false,
			reason:  "not editable",
		},
		{
			name:    "tools-rejected",
			surface: "tools",
			change:  "procedure",
			wantOK:  false,
			reason:  "not editable",
		},
		{
			name:    "runtime-rejected",
			surface: "runtime",
			change:  "procedure",
			wantOK:  false,
			reason:  "not editable",
		},
		{
			name:    "orchestration-rejected",
			surface: "orchestration",
			change:  "procedure",
			wantOK:  false,
			reason:  "not editable",
		},
		{
			name:    "mismatch",
			surface: "verification",
			change:  "procedure",
			wantOK:  false,
			reason:  "did not match",
		},
		{
			name:    "empty",
			surface: "",
			change:  "procedure",
			wantOK:  false,
			reason:  "empty",
		},
		{
			name:    "spaces",
			surface: "   ",
			change:  "procedure",
			wantOK:  false,
			reason:  "empty",
		},
		{
			name:    "no-change",
			surface: "procedure",
			change:  "none",
			wantOK:  false,
			reason:  "did not match",
		},
		{
			name:    "body-no-change",
			surface: "body",
			change:  "none",
			wantOK:  false,
			reason:  "did not change",
		},
		{
			name:    "multi-pass",
			surface: "procedure, pitfalls",
			change:  "procedure+pitfalls",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "multi-pipe",
			surface: "procedure|pitfalls",
			change:  "procedure+pitfalls",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "multi-slash",
			surface: "procedure/verification",
			change:  "procedure+verification",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "multi-and",
			surface: "procedure and pitfalls",
			change:  "procedure+pitfalls",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "multi-missing",
			surface: "procedure, verification",
			change:  "procedure",
			wantOK:  false,
			reason:  "did not match",
		},
		{
			name:    "duplicate",
			surface: "procedure, procedure",
			change:  "procedure",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "custom-heading",
			surface: "examples",
			change:  "examples",
			wantOK:  true,
			reason:  "",
		},
		{
			name:    "custom-mismatch",
			surface: "examples",
			change:  "procedure",
			wantOK:  false,
			reason:  "did not match",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			original, candidate := guardrailDocument(tc.change)
			got, reason := ValidateEditedSurface(Audit{EditedSurface: tc.surface}, original, candidate)
			if got != tc.wantOK {
				t.Fatalf("ValidateEditedSurface() = (%v, %q), want ok=%v", got, reason, tc.wantOK)
			}
			if tc.reason != "" && !strings.Contains(reason, tc.reason) {
				t.Fatalf("reason %q does not contain %q", reason, tc.reason)
			}
		})
	}
}

func longSkillDocument(sections int, linesPerSection int) string {
	var b strings.Builder
	b.WriteString("# Stable Skill\n")
	for section := 0; section < sections; section++ {
		b.WriteString("\n## Section ")
		b.WriteString(string(rune('A' + section)))
		b.WriteString("\n")
		for line := 0; line < linesPerSection; line++ {
			b.WriteString("- stable rule ")
			b.WriteString(string(rune('a' + section)))
			b.WriteString(string(rune('0' + line%10)))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func TestValidateTextualEditBudgetEnforcesSizeBoundaryAndHeadingInvariants(t *testing.T) {
	t.Parallel()
	original := longSkillDocument(4, 6)
	tests := []struct {
		name            string
		candidate       func(string) string
		covered, wantOK bool
		reason          string
	}{
		{
			name:      "identical",
			candidate: func(s string) string { return s },
			wantOK:    true,
		},
		{
			name:      "empty",
			candidate: func(string) string { return "   " },
			wantOK:    false,
			reason:    "empty candidate",
		},
		{
			name:      "tiny-short-original",
			candidate: func(string) string { return "# Tiny\nnew" },
			wantOK:    true,
		},
		{
			name:      "remove-required-headings",
			candidate: func(s string) string { return strings.ReplaceAll(s, "## Section B", "plain B") },
			wantOK:    false,
			reason:    "removed required headings",
		},
		{
			name:      "shrink-below-third",
			candidate: func(string) string { return "# Stable Skill\n## Section A\n- one" },
			wantOK:    false,
			reason:    "shrank",
		},
		{
			name:      "uncovered-broad-change",
			candidate: func(s string) string { return strings.ReplaceAll(s, "stable rule", "rewritten rule") },
			wantOK:    false,
			reason:    "changed",
		},
		{
			name:      "frontmatter-preserved",
			candidate: func(s string) string { return s },
			wantOK:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := original
			candidateBase := original
			if tc.name == "tiny-short-original" {
				base = "# Tiny\nold"
				candidateBase = base
			}
			if tc.name == "frontmatter-preserved" {
				base = "---\nname: stable\n---\n" + original
				candidateBase = original
			}
			candidate := tc.candidate(candidateBase)
			ok, reason := ValidateTextualEditBudget(base, candidate, tc.covered)
			if ok != tc.wantOK {
				t.Fatalf("ValidateTextualEditBudget() = (%v, %q), want ok=%v", ok, reason, tc.wantOK)
			}
			if tc.reason != "" && !strings.Contains(reason, tc.reason) {
				t.Fatalf("reason %q does not contain %q", reason, tc.reason)
			}
		})
	}
}

func TestValidateHermesEvolutionGuardrailsRejectsOversizedRetitledAndUncoveredRewrites(t *testing.T) {
	t.Parallel()
	original := longSkillDocument(6, 3)
	tests := []struct {
		name            string
		candidate       string
		covered, wantOK bool
		reason          string
	}{
		{
			name:      "identical",
			candidate: original,
			wantOK:    true,
		},
		{
			name:      "oversized",
			candidate: "# Stable Skill\n" + strings.Repeat("x", 16*1024),
			wantOK:    false,
			reason:    "exceeds",
		},
		{
			name:      "retitled",
			candidate: strings.Replace(original, "# Stable Skill", "# Different Skill", 1),
			wantOK:    false,
			reason:    "title changed",
		},
		{
			name:      "four-sections-uncovered",
			candidate: strings.NewReplacer("stable rule a", "new rule a", "stable rule b", "new rule b", "stable rule c", "new rule c", "stable rule d", "new rule d").Replace(original),
			wantOK:    false,
			reason:    "broad rewrite",
		},
		{
			name:      "four-sections-covered",
			candidate: strings.NewReplacer("stable rule a", "new rule a", "stable rule b", "new rule b", "stable rule c", "new rule c", "stable rule d", "new rule d").Replace(original),
			covered:   true,
			wantOK:    true,
		},
		{
			name:      "six-sections-covered",
			candidate: strings.ReplaceAll(original, "stable rule", "new rule"),
			covered:   true,
			wantOK:    false,
			reason:    "broad rewrite",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, reason := ValidateHermesEvolutionGuardrails(original, tc.candidate, tc.covered)
			if ok != tc.wantOK {
				t.Fatalf("ValidateHermesEvolutionGuardrails() = (%v, %q), want ok=%v", ok, reason, tc.wantOK)
			}
			if tc.reason != "" && !strings.Contains(reason, tc.reason) {
				t.Fatalf("reason %q does not contain %q", reason, tc.reason)
			}
		})
	}
}
