package genesis

import (
	"strings"
	"testing"
)

// TestSelfCorrectionReviewResolvesAUniqueIDPrefix pins the 2026-08-29 fix.
// Candidate ids are sc-<epochMillis>-<hash8> and callers lose the hash:
// production answered `self-correction candidate not found: sc-1787744942869`
// twice (08-26, 08-29) for ids whose full form was in the ledger and unique on
// that prefix. The review died on an exact-match miss, and a self-correction
// that never lands is how this funnel loses work without saying so.
func TestSelfCorrectionReviewResolvesAUniqueIDPrefix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr := newTestTracker(t)

	rec, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk",
		Title: "prefix candidate", Source: "chat",
	})
	if err != nil {
		t.Fatal(err)
	}
	millis, _, ok := strings.Cut(strings.TrimPrefix(rec.ID, "sc-"), "-")
	if !ok {
		t.Fatalf("id %q is not sc-<millis>-<hash>; the prefix contract changed", rec.ID)
	}
	prefix := "sc-" + millis

	if _, err := tr.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: prefix, Status: SelfCorrectionStatusAccepted, Reason: "review verdict",
	}); err != nil {
		t.Fatalf("a unique prefix must resolve: %v", err)
	}

	current, found, err := tr.mergedSelfCorrectionCandidateLocked(rec.ID)
	if err != nil || !found {
		t.Fatalf("candidate lookup: found=%v err=%v", found, err)
	}
	if current.Status != SelfCorrectionStatusAccepted {
		t.Fatalf("status = %q, want the review to have landed on the full record", current.Status)
	}
}

// TestSelfCorrectionAmbiguousPrefixIsRefused: resolving what is unambiguous is
// the point; guessing between two candidates is not. The error has to name them
// so the caller can pick.
func TestSelfCorrectionAmbiguousPrefixIsRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr := newTestTracker(t)

	var ids []string
	for _, title := range []string{"first", "second"} {
		rec, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
			Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk",
			Title: title, Source: "chat",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rec.ID)
	}
	// "sc-" alone is a prefix of every candidate.
	_, _, err := tr.mergedSelfCorrectionCandidateLocked("sc-")
	if err == nil {
		t.Fatal("an ambiguous prefix must not resolve")
	}
	for _, id := range ids {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("ambiguity error should name %s: %v", id, err)
		}
	}
}

// TestSelfCorrectionUnknownIDNamesTheFormat: a genuinely unknown id still hard
// errors, but says what an id looks like — a truncated id was the recorded way
// of getting here, so the message has to be actionable.
func TestSelfCorrectionUnknownIDNamesTheFormat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr := newTestTracker(t)

	_, err := tr.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: "sc-does-not-exist", Status: SelfCorrectionStatusAccepted, Reason: "review verdict",
	})
	if err == nil {
		t.Fatal("an unknown id must still fail")
	}
	if !strings.Contains(err.Error(), "sc-<millis>-<hash>") {
		t.Fatalf("the not-found error should name the id format: %v", err)
	}
}

// TestSelfCorrectionEmptyIDDoesNotMatchEverything: the empty string is a prefix
// of every id, so the prefix path must reject it rather than fold the ledger.
func TestSelfCorrectionEmptyIDDoesNotMatchEverything(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr := newTestTracker(t)

	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk",
		Title: "only candidate", Source: "chat",
	}); err != nil {
		t.Fatal(err)
	}
	_, found, err := tr.mergedSelfCorrectionCandidateLocked("")
	if err != nil {
		t.Fatalf("an empty id should be a clean miss, got: %v", err)
	}
	if found {
		t.Fatal("an empty id must not resolve to the sole candidate")
	}
}
