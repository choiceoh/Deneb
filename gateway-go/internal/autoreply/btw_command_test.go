package autoreply

import (
	"testing"
)

func TestIsBtwRequestText(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"", false},
		{"hello", false},
		{"/btw", true},
		{"/btw what is Go?", true},
		{"/BTW question", true},
		{"/btw:question", true},
		{"/btwx", false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := IsBtwRequestText(tt.text, "", nil); got != tt.want {
				t.Errorf("IsBtwRequestText(%q) = %v", tt.text, got)
			}
		})
	}
}

func TestExtractBtwQuestion(t *testing.T) {
	// Not a btw command.
	_, ok := ExtractBtwQuestion("hello", "", nil)
	if ok {
		t.Error("non-btw should not match")
	}

	// Bare /btw.
	q, ok := ExtractBtwQuestion("/btw", "", nil)
	if !ok || q != "" {
		t.Errorf("bare /btw: q=%q ok=%v", q, ok)
	}

	// /btw with question.
	q, ok = ExtractBtwQuestion("/btw what is Go?", "", nil)
	if !ok || q != "what is Go?" {
		t.Errorf("btw question: q=%q", q)
	}
}
