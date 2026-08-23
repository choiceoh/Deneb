package server

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// newSelfCorrectionTestServer builds a Server with a hermetic genesis tracker
// (DENEB_STATE_DIR) and work-feed store wired for the card path.
func newSelfCorrectionTestServer(t *testing.T) (*Server, *genesis.Tracker) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := genesis.NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s := &Server{
		logger: slog.Default(),
		GenesisSubsystem: &GenesisSubsystem{
			genesisTracker: tr,
		},
		MemorySubsystem: &MemorySubsystem{
			workFeedStore:   workfeed.NewStore(filepath.Join(dir, "feed.jsonl")),
			nativeSyncStore: nativesync.NewStore(filepath.Join(dir, "sync.jsonl")),
		},
	}
	return s, tr
}

// Approve settles the card and records the ledger review "accepted"; reject
// records "rejected" with the tray/dialog comment as the review note. A
// failed side effect (missing candidate) keeps the card unsettled.
func TestSelfCorrectionCardApproveRejectRecordsLedgerReview(t *testing.T) {
	s, tr := newSelfCorrectionTestServer(t)

	rec, err := tr.RecordSelfCorrectionCandidate(genesis.SelfCorrectionCandidateRecord{
		Scope: "skill", SkillName: "morning-letter", Title: "요약 문단 중복",
		Candidate: "인사말 뒤 요약 문단이 두 번 생성되는 관측", Risk: "낮음", Source: "feed-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.postSelfCorrectionCard(rec); err != nil {
		t.Fatal(err)
	}
	items, _, err := s.workFeedStore.List(10, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one card, got %d (err=%v)", len(items), err)
	}
	card := items[0]
	if card.Source != selfCorrectionSource || card.RefID != rec.ID || len(card.Actions) != 2 {
		t.Fatalf("card = %+v", card)
	}

	nf := s.nativeWorkFeedStore()
	result, err := nf.RunAction(card.ID, workfeedApproveAction, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Item.Status != workfeed.StatusAcked {
		t.Fatalf("approve must settle the card, status=%s", result.Item.Status)
	}
	accepted, err := tr.RecentSelfCorrectionCandidates("", genesis.SelfCorrectionStatusAccepted, 10)
	if err != nil || len(accepted) != 1 || accepted[0].ID != rec.ID {
		t.Fatalf("expected accepted review in ledger, got %+v (err=%v)", accepted, err)
	}

	// Reject path: the comment rides along as the review note.
	rec2, err := tr.RecordSelfCorrectionCandidate(genesis.SelfCorrectionCandidateRecord{
		Scope: "skill", SkillName: "morning-letter", Title: "두 번째 후보",
		Candidate: "거절 경로 검증용", Source: "feed-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.postSelfCorrectionCard(rec2); err != nil {
		t.Fatal(err)
	}
	items, _, err = s.workFeedStore.List(10, false)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one unsettled card, got %d (err=%v)", len(items), err)
	}
	if _, err := nf.RunAction(items[0].ID, workfeedRejectAction, "의도된 동작입니다"); err != nil {
		t.Fatal(err)
	}
	rejected, err := tr.RecentSelfCorrectionCandidates("", genesis.SelfCorrectionStatusRejected, 10)
	if err != nil || len(rejected) != 1 || rejected[0].ID != rec2.ID {
		t.Fatalf("expected rejected review in ledger, got %+v (err=%v)", rejected, err)
	}
	if rejected[0].ReviewNote != "의도된 동작입니다" {
		t.Fatalf("reject comment must land as review note, got %q", rejected[0].ReviewNote)
	}
	if rejected[0].Reviewer != "operator-feed" {
		t.Fatalf("reviewer must be operator-feed, got %q", rejected[0].Reviewer)
	}
}

// A card whose candidate no longer exists (ledger wiped, stale feed) must
// keep the card unsettled so the operator sees the failure instead of a
// silently lost decision.
func TestSelfCorrectionCardMissingCandidateKeepsCardUnsettled(t *testing.T) {
	s, _ := newSelfCorrectionTestServer(t)

	orphan := genesis.SelfCorrectionCandidateRecord{
		ID: "sc-does-not-exist", Scope: "skill", Title: "고아 카드",
	}
	if err := s.postSelfCorrectionCard(orphan); err != nil {
		t.Fatal(err)
	}
	items, _, err := s.workFeedStore.List(10, false)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one card, got %d (err=%v)", len(items), err)
	}
	nf := s.nativeWorkFeedStore()
	if _, err := nf.RunAction(items[0].ID, workfeedApproveAction, ""); err == nil {
		t.Fatal("approve on a missing candidate must fail")
	}
	after, _, err := s.workFeedStore.List(10, false)
	if err != nil || len(after) != 1 {
		t.Fatalf("failed side effect must keep the card, got %d (err=%v)", len(after), err)
	}
	if strings.TrimSpace(after[0].Status) == workfeed.StatusAcked {
		t.Fatal("card must stay unsettled")
	}
}
