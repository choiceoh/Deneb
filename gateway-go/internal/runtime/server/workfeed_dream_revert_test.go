package server

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// A dream cycle with a git snapshot carries a 되돌리기 action; running it
// reverts the cycle (page content returns to pre-cycle). Cycles without a
// commit (git off / nothing changed) keep the 확인-only card.
func TestDreamCardRevertActionRevertsCycle(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found")
	}
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := wiki.NewStore(wikiDir, wikiDir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			workFeedStore:   workfeed.NewStore(filepath.Join(dir, "feed.jsonl")),
			nativeSyncStore: nativesync.NewStore(filepath.Join(dir, "sync.jsonl")),
			wikiStore:       ws,
		},
	}

	// No-commit cycle: 확인-only, RefID empty.
	s.postDreamWorkfeedCard(&autonomous.DreamReport{WikiPagesCreated: 1})
	items, _, err := s.workFeedStore.List(10, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one card, got %d (err=%v)", len(items), err)
	}
	if len(items[0].Actions) != 1 || items[0].RefID != "" {
		t.Fatalf("commitless card = %+v", items[0])
	}
	// Settle the 확인-only card so the later unsettled-list is just the revert card.
	if _, err := s.nativeWorkFeedStore().RunAction(items[0].ID, "dream:ack", ""); err != nil {
		t.Fatal(err)
	}

	// A real wiki change + snapshot + card with the hash.
	page := wiki.NewPage("되돌림대상", "업무", nil)
	if page == nil {
		t.Fatal("NewPage returned nil")
	}
	if err := ws.WritePage("업무/되돌림대상.md", page); err != nil {
		t.Fatal(err)
	}
	hash := ws.SnapshotGit(t.Context(), "dream cycle test")
	if hash == "" {
		t.Skip("snapshot unavailable in this sandbox")
	}
	s.postDreamWorkfeedCard(&autonomous.DreamReport{WikiPagesCreated: 1, GitCommit: hash, WikiChangeSummary: "스냅숏 " + hash})
	items, _, err = s.workFeedStore.List(10, false)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one unsettled card, got %d (err=%v)", len(items), err)
	}
	card := items[0]
	if len(card.Actions) != 2 || card.Actions[1].ID != dreamRevertActionPrefix+hash {
		t.Fatalf("card actions = %+v", card.Actions)
	}

	// Running the revert action settles the card and restores the tree (the
	// page the cycle created disappears).
	nf := s.nativeWorkFeedStore()
	result, err := nf.RunAction(card.ID, card.Actions[1].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Item.Status != workfeed.StatusAcked {
		t.Fatalf("revert must settle the card, status=%s", result.Item.Status)
	}
	if p, _ := ws.ReadPage("업무/되돌림대상.md"); p != nil {
		t.Fatal("cycle-created page must be gone after revert")
	}
}
