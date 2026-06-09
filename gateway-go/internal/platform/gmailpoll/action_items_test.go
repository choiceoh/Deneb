package gmailpoll

import "testing"

func TestSanitizeActionItems_TrimsCapsNormalizes(t *testing.T) {
	in := []ActionItem{
		{Title: "  회신  ", DueHint: " 내일 ", Priority: "HIGH"},
		{Title: "", Priority: "high"},    // empty title → dropped
		{Title: "검토", Priority: "weird"}, // unknown → medium
		{Title: "a", Priority: "low"},
		{Title: "b", Priority: "low"},
		{Title: "c", Priority: "low"},
		{Title: "d", Priority: "low"}, // beyond cap → dropped
	}
	got := sanitizeActionItems(in)
	if len(got) != 5 {
		t.Fatalf("expected cap of 5, got %d: %+v", len(got), got)
	}
	if got[0].Title != "회신" || got[0].DueHint != "내일" {
		t.Errorf("fields not trimmed: %+v", got[0])
	}
	if got[0].Priority != "high" {
		t.Errorf("HIGH should normalize to high, got %q", got[0].Priority)
	}
	if got[1].Title != "검토" || got[1].Priority != "medium" {
		t.Errorf("unknown priority should default to medium: %+v", got[1])
	}
}

func TestNormalizeActionPriority(t *testing.T) {
	cases := map[string]string{
		"high": "high", "HIGH": "high", "urgent": "high", "높음": "high", "긴급": "high",
		"low": "low", "낮음": "low",
		"medium": "medium", "": "medium", "기타": "medium",
	}
	for in, want := range cases {
		if got := normalizeActionPriority(in); got != want {
			t.Errorf("normalizeActionPriority(%q) = %q, want %q", in, got, want)
		}
	}
}
