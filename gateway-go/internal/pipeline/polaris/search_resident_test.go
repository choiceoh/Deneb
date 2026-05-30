package polaris

import "testing"

func TestSearchResidentSessions(t *testing.T) {
	s := testStore(t)

	// Two distinct conversations, both resident after AppendMessage.
	if err := s.AppendMessage("current", textMsg("user", "today let us discuss the quarterly budget", 1000)); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage("older", textMsg("user", "earlier we agreed the budget deadline is friday", 500)); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage("older", textMsg("assistant", "noted, friday it is", 600)); err != nil {
		t.Fatal(err)
	}

	// Searching from "current" must surface the "older" session's budget message
	// and must NOT echo the current session's own messages.
	hits, err := s.SearchResidentSessions("current", "budget deadline", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one cross-session hit")
	}
	for _, h := range hits {
		if h.SessionKey == "current" {
			t.Errorf("cross-session search must exclude the current session, got hit in %q", h.SessionKey)
		}
	}
	if hits[0].SessionKey != "older" {
		t.Errorf("top hit session = %q, want \"older\"", hits[0].SessionKey)
	}
}

func TestSearchResidentSessions_EmptyAndGuards(t *testing.T) {
	s := testStore(t)
	s.AppendMessage("a", textMsg("user", "hello world", 1000))

	if hits, _ := s.SearchResidentSessions("a", "", 5); hits != nil {
		t.Error("empty query should return nil")
	}
	if hits, _ := s.SearchResidentSessions("a", "hello", 0); hits != nil {
		t.Error("zero maxResults should return nil")
	}
	// Only session "a" is resident and it is excluded → no hits.
	if hits, _ := s.SearchResidentSessions("a", "hello", 5); len(hits) != 0 {
		t.Errorf("excluding the only resident session should yield no hits, got %d", len(hits))
	}
}
