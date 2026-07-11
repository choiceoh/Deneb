package textutil

import "testing"

func TestLimitedTermsDeduplicatesAndCaps(t *testing.T) {
	terms := NewLimitedTerms(2, 20)
	for _, value := range []string{" Alice ", "alice", "Bob"} {
		if !terms.Add(value) {
			t.Fatalf("Add(%q) unexpectedly hit cap", value)
		}
	}
	if terms.Add("Carol") {
		t.Fatal("third unique term should exceed count cap")
	}
	if got := terms.String(); got != "Alice, Bob" {
		t.Fatalf("String() = %q", got)
	}
}
