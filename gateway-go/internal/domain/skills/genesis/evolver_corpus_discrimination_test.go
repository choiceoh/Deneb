package genesis

import (
	"encoding/json"
	"strings"
	"testing"
)

func discriminationFixture(t *testing.T) *Evolver {
	t.Helper()
	tracker := newTestTracker(t)
	return &Evolver{tracker: tracker}
}

func longBody(seed string) string {
	return seed + strings.Repeat(" 절차 본문 상세 내용", 40)
}

// The measured leak: a byte-identical candidate re-ran the full gate pipeline
// three times. The refusal must fire on the repeat and name the prior reason.
func TestRepeatRefusalBlocksByteIdenticalCandidate(t *testing.T) {
	e := discriminationFixture(t)
	body := longBody("유튜브 URL은 watch 도구가 단일 진입점.")
	e.recordRejectedSkillEdit("yt", body, "held-out selection rejected: candidate did not improve validation score enough (83.3 vs original 83.3): x", "self-test", HarnessEditAudit{})

	reason, repeat := e.refuseRepeatedRejectedCandidate("yt", body)
	if !repeat {
		t.Fatal("byte-identical repeat was not refused")
	}
	if !strings.Contains(reason, "repeat refusal") || !strings.Contains(reason, "83.3") {
		t.Errorf("refusal reason must carry the prior verdict: %q", reason)
	}
}

// Anything that is not a true repeat must pass: a different body, a short stub,
// an infrastructure row (outage evidence, not a quality verdict), and a
// different skill's buffer.
func TestRepeatRefusalFailsOpen(t *testing.T) {
	e := discriminationFixture(t)
	body := longBody("원본 후보.")
	e.recordRejectedSkillEdit("sk", body, "judge rejected", "self-test", HarnessEditAudit{})

	if _, repeat := e.refuseRepeatedRejectedCandidate("sk", longBody("다른 방향의 후보.")); repeat {
		t.Error("a different candidate was refused")
	}
	if _, repeat := e.refuseRepeatedRejectedCandidate("other", body); repeat {
		t.Error("another skill's buffer refused this candidate")
	}
	if _, repeat := e.refuseRepeatedRejectedCandidate("sk", "짧은 스텁"); repeat {
		t.Error("a stub body was refused")
	}

	infra := discriminationFixture(t)
	if err := infra.tracker.RecordRejectedSkillEdit(RejectedSkillEditRecord{
		SkillName: "sk", Reason: "judge error: transport closed", CandidateBody: body,
	}); err != nil {
		t.Fatalf("record infra row: %v", err)
	}
	if _, repeat := infra.refuseRepeatedRejectedCandidate("sk", body); repeat {
		t.Error("an infrastructure row must never refuse — it is not a quality verdict")
	}
}

// Three ties inside the window must mint exactly one corpus-mining draft; a
// non-tie rejection must not count toward it.
func TestHeldOutTieCorpusDraftMintsOnThreshold(t *testing.T) {
	e := discriminationFixture(t)
	tie := "held-out selection rejected: candidate did not improve validation score enough (83.3 vs original 83.3): x"
	worse := "held-out selection rejected: candidate did not improve validation score enough (70.0 vs original 83.3): x"

	// Two distinct tie rows + one worse row: below threshold, no draft.
	e.recordRejectedSkillEdit("yt", longBody("a"), tie+" v1", "self-test", HarnessEditAudit{})
	e.recordRejectedSkillEdit("yt", longBody("b"), worse, "self-test", HarnessEditAudit{})
	drafts, err := e.tracker.RecentSelfCorrectionCandidates("yt", "", 20)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, d := range drafts {
		if strings.HasPrefix(d.Source, heldOutTieDraftSource) {
			t.Fatalf("draft minted below threshold: %+v", d)
		}
	}

	e.recordRejectedSkillEdit("yt", longBody("c"), tie+" v2", "self-test", HarnessEditAudit{})
	e.recordRejectedSkillEdit("yt", longBody("d"), tie+" v3", "self-test", HarnessEditAudit{})
	drafts, err = e.tracker.RecentSelfCorrectionCandidates("yt", "", 20)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	minted := 0
	for _, d := range drafts {
		if strings.HasPrefix(d.Source, heldOutTieDraftSource) {
			minted++
			// Operator-facing text is Korean (the card is a Korean approval
			// surface); the demand it must carry is unchanged.
			if !strings.Contains(d.Candidate, "실제 실패") && !strings.Contains(d.ProposedChange, "실제 실패") {
				t.Errorf("draft must demand real-failure case mining: %+v", d)
			}
			if !strings.Contains(d.ProposedChange, "완화하거나 동점 후보를 수용하면 안 된다") {
				t.Errorf("draft must forbid margin relaxation: %q", d.ProposedChange)
			}
		}
	}
	if minted != 1 {
		t.Fatalf("want exactly one tie-corpus draft, got %d", minted)
	}
}

// Guard against payload drift: the JSON shape of the minted draft is consumed
// by the sweep; scope must be validation, skill must be set.
func TestHeldOutTieDraftShape(t *testing.T) {
	e := discriminationFixture(t)
	tie := "held-out selection rejected: candidate did not improve validation score enough (50.0 vs original 50.0): y"
	for i, suffix := range []string{" a", " b", " c"} {
		e.recordRejectedSkillEdit("sk2", longBody(string(rune('a'+i))), tie+suffix, "preflight", HarnessEditAudit{})
	}
	drafts, err := e.tracker.RecentSelfCorrectionCandidates("sk2", "", 20)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, d := range drafts {
		if strings.HasPrefix(d.Source, heldOutTieDraftSource) {
			if d.Scope != "test" || d.SkillName != "sk2" {
				raw, _ := json.Marshal(d)
				t.Fatalf("draft shape drifted: %s", raw)
			}
			return
		}
	}
	t.Fatal("no tie-corpus draft found")
}
