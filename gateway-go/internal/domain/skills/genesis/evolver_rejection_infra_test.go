package genesis

import "testing"

func TestIsInfrastructureRejectionMatchesOutagesOnly(t *testing.T) {
	outages := []string{
		"judge error",
		"Judge Error",
		"teacher escalation failed",
	}
	for _, reason := range outages {
		if !isInfrastructureRejection(reason) {
			t.Errorf("%q must classify as an infrastructure failure", reason)
		}
	}
	// Every one of these IS a verdict on the candidate — misclassifying one as
	// an outage would silently drop a real rejection out of the metrics.
	verdicts := []string{
		"held-out selection rejected: candidate did not improve validation score enough",
		"self-harness audit rejected: no failure evidence bundle supports target_signature",
		"flip gate rejected: candidate regressed 1 previously-passing held-out case",
		"Hermes patch-first gate rejected broad rewrite: changed 5 sections, max 3",
		"rejected",
		"",
	}
	for _, reason := range verdicts {
		if isInfrastructureRejection(reason) {
			t.Errorf("%q is a verdict, must NOT classify as an infrastructure failure", reason)
		}
	}
}

// The stored flag is what downstream readers consume, so it must be stamped at
// write time rather than left to each reader to re-parse the prose.
func TestRecordRejectedSkillEditStampsInfrastructureFlag(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordRejectedSkillEdit(RejectedSkillEditRecord{
		SkillName: "contract-review", Reason: "judge error", CandidateBody: "body",
	}); err != nil {
		t.Fatalf("record outage: %v", err)
	}
	if err := tr.RecordRejectedSkillEdit(RejectedSkillEditRecord{
		SkillName: "contract-review", Reason: "held-out selection rejected: no gain", CandidateBody: "body",
	}); err != nil {
		t.Fatalf("record verdict: %v", err)
	}

	got, err := tr.RecentRejectedSkillEdits("contract-review", 10)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	byReason := make(map[string]bool, len(got))
	for _, rec := range got {
		byReason[rec.Reason] = rec.Infrastructure
	}
	if !byReason["judge error"] {
		t.Error("judge error row must carry Infrastructure=true")
	}
	if byReason["held-out selection rejected: no gain"] {
		t.Error("a held-out verdict must carry Infrastructure=false")
	}
}
