package genesis

import "testing"

// TestDispatchProcedureRefFoldsFirstSeenAndResetsPerAttempt verifies the
// dispatch-procedure stamp is adopted first-seen within an attempt (so the
// started row's version wins over later phases) and cleared on a new attempt
// (so a retry re-captures the then-current dispatch contract version).
func TestDispatchProcedureRefFoldsFirstSeenAndResetsPerAttempt(t *testing.T) {
	// started, attempt a1 → stamps the ref.
	base := applySelfCorrectionDispatch(SelfCorrectionCandidateRecord{},
		SelfCorrectionCandidateRecord{DispatchPhase: selfCorrectionDispatchStarted, AttemptID: "a1", DispatchProcedureRef: "proc-aaaa1111"})
	if base.DispatchProcedureRef != "proc-aaaa1111" {
		t.Fatalf("started did not stamp the dispatch ref: %q", base.DispatchProcedureRef)
	}

	// A later phase of the SAME attempt carries a different ref → first-seen keeps a1's.
	base = applySelfCorrectionDispatch(base,
		SelfCorrectionCandidateRecord{DispatchPhase: selfCorrectionDispatchPROpened, AttemptID: "a1", DispatchProcedureRef: "proc-bbbb2222"})
	if base.DispatchProcedureRef != "proc-aaaa1111" {
		t.Fatalf("later phase overwrote the started ref: %q", base.DispatchProcedureRef)
	}

	// A NEW attempt's started resets and re-captures the then-current version.
	base = applySelfCorrectionDispatch(base,
		SelfCorrectionCandidateRecord{DispatchPhase: selfCorrectionDispatchStarted, AttemptID: "a2", DispatchProcedureRef: "proc-cccc3333"})
	if base.DispatchProcedureRef != "proc-cccc3333" {
		t.Fatalf("new attempt did not re-capture the dispatch ref: %q", base.DispatchProcedureRef)
	}
}
