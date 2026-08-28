package textsearch

import (
	"strings"
	"testing"
)

// Each rule in extractSnippet exists because of a measured reader-stage
// failure; these pin the behaviors so a refactor cannot quietly shed one.

func TestSnippetPrefersIdfWeightOverTokenCount(t *testing.T) {
	// Head holds TWO ubiquitous tokens; tail holds the ONE informative token.
	doc := "many people ask many different things in many different ways here. " +
		strings.Repeat("filler sentence goes on. ", 20) +
		"I saw three doctors last month: a GP, an ENT, and a dermatologist."
	idf := map[string]float64{"many": 0.1, "different": 0.1, "doctors": 3.0}
	got := extractSnippet([]string{doc}, []string{"many", "different", "doctors"}, 40, idf)
	if !strings.Contains(got, "doctors") {
		t.Fatalf("idf weighting must pull the window to the rare token: %q", got)
	}
}

func TestSnippetSnapsToSentenceBoundary(t *testing.T) {
	doc := strings.Repeat("padding words fill space here. ", 8) +
		"The visit total was three doctors in all. " +
		strings.Repeat("more trailing padding sentences. ", 8)
	got := extractSnippet([]string{doc}, []string{"doctors"}, 40, nil)
	if !strings.Contains(got, "The visit total was three doctors in all.") {
		t.Fatalf("window should carry the full sentence: %q", got)
	}
}

func TestSnippetJoinsDisjointClusters(t *testing.T) {
	doc := "The dermatologist visit went well and was quick. " +
		strings.Repeat("unrelated chatter continues at length here. ", 30) +
		"Separately the cardiologist appointment is booked."
	idf := map[string]float64{"dermatologist": 2.5, "cardiologist": 2.5}
	got := extractSnippet([]string{doc}, []string{"dermatologist", "cardiologist"}, 40, idf)
	if !strings.Contains(got, "dermatologist") || !strings.Contains(got, "cardiologist") {
		t.Fatalf("disjoint clusters should both appear: %q", got)
	}
	if !strings.Contains(got, " … ") {
		t.Fatalf("two fragments should be joined with an ellipsis: %q", got)
	}
}
