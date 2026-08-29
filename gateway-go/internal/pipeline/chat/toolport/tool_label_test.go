package toolport

import (
	"strings"
	"testing"
)

func TestChatToolLabelNamesCuratedTools(t *testing.T) {
	for tool, want := range map[string]string{
		"gmail":      "메일 확인",
		"exec":       "명령 실행",
		"phone_read": "휴대폰 확인",
		"wiki":       "기억 검색",
	} {
		if got := ChatToolLabel(tool); got != want {
			t.Errorf("ChatToolLabel(%q) = %q, want %q", tool, got, want)
		}
	}
}

// An unknown tool must return empty so the client falls back to rendering the
// identifier. A guessed label would look curated while being nothing of the
// sort, and would hide that the tool still needs an entry.
func TestChatToolLabelLeavesUnknownToolsUnnamed(t *testing.T) {
	for _, tool := range []string{"", "brand_new_tool", "GMAIL"} {
		if got := ChatToolLabel(tool); got != "" {
			t.Errorf("ChatToolLabel(%q) = %q, want empty", tool, got)
		}
	}
}

// The noun form is the contract: surfaces add their own tense (running "…중",
// finished "…완료", trail bare). A label shipped already conjugated would force
// every client to strip it back off.
func TestChatToolLabelsAreNounsNotProgressive(t *testing.T) {
	for tool, label := range chatToolLabels {
		if strings.HasSuffix(label, " 중") {
			t.Errorf("%q label %q is progressive; ship the noun form", tool, label)
		}
		if label == "" || strings.TrimSpace(label) != label {
			t.Errorf("%q label %q must be non-empty and free of edge whitespace", tool, label)
		}
	}
}
