package memory

import "testing"

// Every per-axis englishAssert pattern is anchored at ^, so "remember that I am
// vegan" — the natural English phrasing — left the assertion unmatchable while
// "remember: I am vegan" bound, purely because the extractor trimmed
// punctuation but not the conjunction.
func TestEnglishRememberBindsWithTheConjunction(t *testing.T) {
	cases := []struct {
		message string
		wantKey string
	}{
		{"remember that I am vegan", "diet.vegan"},
		{"remember that I'm allergic to peanuts", "health.allergy"},
		{"remember that I prefer short answers", "communication.response_length"},
		{"remember that my long term goal is to retire early", "goals.long_term"},
		// The colon form already worked and must keep working.
		{"remember: I am vegan", "diet.vegan"},
		{"please remember that I am vegan", "diet.vegan"},
	}
	for _, tc := range cases {
		ind := InduceFromTurn(tc.message)
		if ind == nil || ind.Route != RouteMemory || ind.Candidate.FactKey != tc.wantKey {
			got := "<nil>"
			if ind != nil {
				got = string(ind.Route) + "/" + ind.Candidate.FactKey
			}
			t.Errorf("InduceFromTurn(%q) = %s; want memory/%s", tc.message, got, tc.wantKey)
		}
	}
}

// A demonstrative "that" must not turn an episodic sentence into a fact.
func TestEnglishRememberConjunctionDoesNotInventFacts(t *testing.T) {
	for _, msg := range []string{
		"remember that meeting was on tuesday",
		"remember that thatcher resigned in 1990",
		"remember that the client asked for a discount",
	} {
		ind := InduceFromTurn(msg)
		if ind != nil && ind.Route == RouteMemory {
			t.Errorf("InduceFromTurn(%q) bound a fact: key=%q", msg, ind.Candidate.FactKey)
		}
	}
}

// The trim is English-only and boundary-safe.
func TestTrimEnglishRememberConjunction(t *testing.T) {
	cases := map[string]string{
		"that I am vegan": "I am vegan",
		"That I am vegan": "I am vegan",
		"I am vegan":      "I am vegan",
		"thatcher policy": "thatcher policy",
		"that":            "that",
		"":                "",
		"나는 비건이야":         "나는 비건이야",
	}
	for in, want := range cases {
		if got := trimEnglishRememberConjunction(in); got != want {
			t.Errorf("trimEnglishRememberConjunction(%q) = %q, want %q", in, got, want)
		}
	}
}

// Korean payload extraction is untouched.
func TestKoreanRememberPayloadUnaffected(t *testing.T) {
	ind := InduceFromTurn("기억해줘: 나는 비건이야")
	if ind == nil || ind.Route != RouteMemory || ind.Candidate.FactKey != "diet.vegan" {
		t.Fatalf("Korean remember regressed: %+v", ind)
	}
}
