package genesis

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func TestJaccardSimilarity(t *testing.T) {
	a := map[string]struct{}{"git": {}, "rebase": {}, "flow": {}}
	if got := jaccardSimilarity(a, a); got != 1.0 {
		t.Fatalf("identical sets: want 1.0, got %v", got)
	}
	b := map[string]struct{}{"deploy": {}, "gateway": {}}
	if got := jaccardSimilarity(a, b); got != 0 {
		t.Fatalf("disjoint sets: want 0, got %v", got)
	}
	if got := jaccardSimilarity(a, nil); got != 0 {
		t.Fatalf("empty set: want 0, got %v", got)
	}
	// 3 shared of 4 union = 0.75
	c := map[string]struct{}{"git": {}, "rebase": {}, "flow": {}, "extra": {}}
	if got := jaccardSimilarity(a, c); got < 0.74 || got > 0.76 {
		t.Fatalf("3/4 overlap: want ~0.75, got %v", got)
	}
}

func TestSkillDedupTokens_DropsShortAndTokenizesKorean(t *testing.T) {
	tokens := skillDedupTokens("git-rebase-flow", "rebase 충돌 해결")
	for _, want := range []string{"git", "rebase", "flow", "충돌", "해결"} {
		if _, ok := tokens[want]; !ok {
			t.Fatalf("expected token %q in %v", want, tokens)
		}
	}
	// Single-rune noise dropped.
	if _, ok := tokens["a"]; ok {
		t.Fatalf("single-rune token should be dropped")
	}
}

func TestSanitizeSkillDescription(t *testing.T) {
	if got := sanitizeSkillDescription("line1\nline2\ttab"); got != "line1 line2 tab" {
		t.Fatalf("newline/tab collapse: got %q", got)
	}
	if got := sanitizeSkillDescription("a\x00b\x07c"); got != "abc" {
		t.Fatalf("control chars removed: got %q", got)
	}
	if got := sanitizeSkillDescription("a    b   c"); got != "a b c" {
		t.Fatalf("whitespace collapse: got %q", got)
	}
	long := strings.Repeat("가", 400)
	got := sanitizeSkillDescription(long)
	if r := []rune(got); len(r) != 301 || r[300] != '…' {
		t.Fatalf("length cap: want 300 runes + ellipsis, got %d runes", len([]rune(got)))
	}
}

func TestIsDuplicateSkill(t *testing.T) {
	cat := skills.NewCatalog(slog.Default())
	cat.Register(skills.SkillEntry{Skill: skills.Skill{
		Name:        "foo-bar-baz",
		Description: "alpha beta gamma delta",
	}})
	svc := &Service{catalog: cat, logger: slog.Default()}

	// Exact name collision is always a duplicate.
	if !svc.isDuplicateSkill("foo-bar-baz", "완전히 다른 설명") {
		t.Fatal("exact name should be a duplicate")
	}
	// Near-identical name+description (7/8 token overlap >= 0.82).
	if !svc.isDuplicateSkill("foo-bar-baz-qux", "alpha beta gamma delta") {
		t.Fatal("near-identical skill should be a duplicate")
	}
	// Unrelated skill is not a duplicate.
	if svc.isDuplicateSkill("zoo-zar-zaz", "omega psi chi") {
		t.Fatal("unrelated skill should not be a duplicate")
	}
	// Nil catalog never dedupes.
	bare := &Service{logger: slog.Default()}
	if bare.isDuplicateSkill("foo-bar-baz", "alpha beta gamma delta") {
		t.Fatal("nil catalog should never dedupe")
	}
}

func TestEnvBool(t *testing.T) {
	if !envBool("DENEB_TEST_MISSING_BOOL", true) {
		t.Fatal("unset should return fallback true")
	}
	for _, v := range []string{"1", "true", "yes", "on", "TRUE", "On"} {
		t.Setenv("DENEB_TEST_BOOL", v)
		if !envBool("DENEB_TEST_BOOL", false) {
			t.Fatalf("%q should be true", v)
		}
	}
	for _, v := range []string{"0", "false", "no", "off"} {
		t.Setenv("DENEB_TEST_BOOL", v)
		if envBool("DENEB_TEST_BOOL", true) {
			t.Fatalf("%q should be false", v)
		}
	}
	t.Setenv("DENEB_TEST_BOOL", "garbage")
	if !envBool("DENEB_TEST_BOOL", true) {
		t.Fatal("garbage should fall back to true")
	}
}
