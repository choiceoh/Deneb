package mailwork

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "work.json"))
}

func remember(t *testing.T, s *Store, id string, age time.Duration) {
	t.Helper()
	if _, err := s.RememberMessage(MessageInput{ID: id, Subject: id}); err != nil {
		t.Fatalf("remember %s: %v", id, err)
	}
	// RememberMessage stamps "now"; rewind so age tests are deterministic.
	if _, err := s.update(id, func(ms MessageState) MessageState {
		ms.CreatedAtMs = time.Now().Add(-age).UnixMilli()
		return ms
	}); err != nil {
		t.Fatalf("age %s: %v", id, err)
	}
}

func ids(states []MessageState) []string {
	out := make([]string, 0, len(states))
	for _, ms := range states {
		out = append(out, ms.ID)
	}
	return out
}

func TestPendingAnalysisSelectsUnanalyzedAndFailed(t *testing.T) {
	s := newTestStore(t)
	remember(t, s, "never", 72*time.Hour)
	remember(t, s, "failed", 48*time.Hour)
	remember(t, s, "done", 96*time.Hour)
	remember(t, s, "review", 96*time.Hour)

	if _, err := s.MarkAnalysisFailed(MessageInput{ID: "failed"}, noBodyError{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkAnalysisDone(AnalysisInput{MessageInput: MessageInput{ID: "done"}}); err != nil {
		t.Fatal(err)
	}
	// review is the terminal park for "cannot be backfilled" — it must NOT come
	// back, or an unfetchable message occupies the bounded batch forever.
	if _, err := s.MarkAnalysisReview(MessageInput{ID: "review"}, "본문 없음"); err != nil {
		t.Fatal(err)
	}

	got := ids(s.PendingAnalysis(10, time.Hour))
	want := map[string]bool{"never": true, "failed": true}
	if len(got) != 2 {
		t.Fatalf("PendingAnalysis = %v, want exactly %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("PendingAnalysis returned %q, want only %v", id, want)
		}
	}
}

// A message that just arrived may simply be in flight; the backfill must not
// race the live poller for it.
func TestPendingAnalysisRespectsMinAge(t *testing.T) {
	s := newTestStore(t)
	remember(t, s, "fresh", 10*time.Minute)
	remember(t, s, "old", 72*time.Hour)

	got := ids(s.PendingAnalysis(10, 12*time.Hour))
	if len(got) != 1 || got[0] != "old" {
		t.Fatalf("PendingAnalysis = %v, want [old]", got)
	}
}

func TestPendingAnalysisOldestFirstAndBounded(t *testing.T) {
	s := newTestStore(t)
	remember(t, s, "newest", 24*time.Hour)
	remember(t, s, "middle", 48*time.Hour)
	remember(t, s, "oldest", 96*time.Hour)

	got := ids(s.PendingAnalysis(2, time.Hour))
	if len(got) != 2 || got[0] != "oldest" || got[1] != "middle" {
		t.Fatalf("PendingAnalysis = %v, want [oldest middle]", got)
	}
	if n := len(s.PendingAnalysis(0, time.Hour)); n != 0 {
		t.Fatalf("limit 0 returned %d", n)
	}
}

// A message that always fails must not keep its place at the head of the queue,
// or the bounded batch is spent on it forever and everything behind it starves.
func TestPendingAnalysisFailureGoesToTheBack(t *testing.T) {
	s := newTestStore(t)
	remember(t, s, "broken", 96*time.Hour) // oldest by registration
	remember(t, s, "waiting", 24*time.Hour)

	if got := ids(s.PendingAnalysis(1, time.Hour)); len(got) != 1 || got[0] != "broken" {
		t.Fatalf("first pick = %v, want [broken]", got)
	}
	if _, err := s.MarkAnalysisFailed(MessageInput{ID: "broken"}, noBodyError{}); err != nil {
		t.Fatal(err)
	}
	if got := ids(s.PendingAnalysis(1, time.Hour)); len(got) != 1 || got[0] != "waiting" {
		t.Fatalf("after failure the queue picked %v, want [waiting]", got)
	}
}

type noBodyError struct{}

func (noBodyError) Error() string { return "본문 없음" }
