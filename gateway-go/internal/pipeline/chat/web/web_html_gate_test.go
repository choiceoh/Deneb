package web

import (
	"strings"
	"testing"
)

func TestShouldCallLocalAI(t *testing.T) {
	tiny := "short"
	if shouldCallLocalAI(tiny, 5000) {
		t.Fatal("tiny markdown must skip LocalAI")
	}

	// Mid-size clean article: enough md, high retention → htmlmd only.
	mid := strings.Repeat("paragraph ", 400) // ~4000 chars
	if shouldCallLocalAI(mid, 5000) {
		t.Fatal("mid-size high-retention page must skip LocalAI")
	}

	// Large noisy page: ≥10k md and <20% retention → LocalAI.
	large := strings.Repeat("x", localAIMinMDChars)
	if !shouldCallLocalAI(large, localAIMinMDChars*10) {
		t.Fatal("large low-retention page should call LocalAI")
	}

	// Large but clean retention → skip.
	if shouldCallLocalAI(large, localAIMinMDChars+100) {
		t.Fatal("large high-retention page must skip LocalAI")
	}
}
