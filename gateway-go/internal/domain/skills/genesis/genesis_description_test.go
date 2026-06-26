package genesis

import (
	"strings"
	"testing"
)

func TestNormalizeSkillDescription(t *testing.T) {
	// Collapse embedded newlines / whitespace runs to single spaces.
	if got := normalizeSkillDescription("Use when X\n  happens\tthen Y"); got != "Use when X happens then Y" {
		t.Errorf("collapse = %q", got)
	}
	// A clean, in-budget description is returned unchanged.
	short := "Use when reviewing a PR diff for bugs"
	if got := normalizeSkillDescription(short); got != short {
		t.Errorf("short mutated = %q", got)
	}
	// Whitespace-only collapses to empty.
	if got := normalizeSkillDescription("   \n  "); got != "" {
		t.Errorf("empty = %q", got)
	}
	// A runaway description clamps to a single bounded line ending in an
	// ellipsis, keeping the leading "Use when" trigger.
	long := "Use when " + strings.Repeat("word ", 50)
	got := normalizeSkillDescription(long)
	if r := []rune(got); len(r) > maxSkillDescriptionRunes+1 {
		t.Errorf("not clamped: %d runes: %q", len(r), got)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("clamp left a newline: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("clamp missing ellipsis: %q", got)
	}
	if !strings.HasPrefix(got, "Use when") {
		t.Errorf("clamp dropped the leading trigger: %q", got)
	}
}

func TestParseGenesisResponse_NormalizesDescription(t *testing.T) {
	resp := `{"skill":{"name":"x","category":"coding","description":"Use when\n   reviewing,\n   then act","body":"## When to Use\nbody\n## Procedure\n1. step"}}`
	skill, err := parseGenesisResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if skill == nil {
		t.Fatal("nil skill")
	}
	if strings.Contains(skill.Description, "\n") {
		t.Errorf("description not flattened: %q", skill.Description)
	}
	if skill.Description != "Use when reviewing, then act" {
		t.Errorf("description = %q", skill.Description)
	}
}
