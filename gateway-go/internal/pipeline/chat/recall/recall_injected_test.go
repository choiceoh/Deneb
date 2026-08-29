package recall

import (
	"testing"
	"time"
)

// Renderer↔parser round-trip: armSnapshotCitations must recover from a
// RENDERED snapshot exactly the paths the ledger scores (isLedgerPage's
// rule) — wiki rows and org rows that point at real pages, never session
// rows or org placeholders. Pins the row shape the parser depends on.
func TestArmSnapshotCitationsRoundTrip(t *testing.T) {
	evidence := []recallEvidence{
		{Kind: "wiki", Source: "프로젝트/pl1-hnm-epc-001/대표.md", Note: "위키 근거", Score: 1.2, At: 1},
		{Kind: "org", Source: "인물/임형철.md", Note: "조직 인물", Score: 1.1, At: 1},
		{Kind: "org", Source: "조직도: 미등재멤버", Note: "페이지 없음", Score: 1.0, At: 1},
		{Kind: "session", Source: "cl:x:s3#1/user", Note: "세션 행", Score: 0.9, At: 1},
	}
	block, _ := formatRecallEvidenceAt(evidence, time.Now(), true, true)

	armSnapshotCitations("client:roundtrip", block)
	got := takeInjectedPaths("client:roundtrip")
	want := map[string]bool{"프로젝트/pl1-hnm-epc-001/대표.md": true, "인물/임형철.md": true}
	if len(got) != len(want) {
		t.Fatalf("want exactly the ledger pages, got %v", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("unexpected path %q in %v", p, got)
		}
	}
	if again := takeInjectedPaths("client:roundtrip"); len(again) != 0 {
		t.Fatalf("candidates must stay consume-once, got %v", again)
	}
}
