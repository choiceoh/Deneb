package server

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// One live decision per proposal file: WriteProposal overwrites <name>.proposed,
// so a second card for the same path must settle the first (adopting the old
// card would adopt the NEW content under the old reason), and an expired
// verdict window settles the card through the task callback.
func TestMetaProposalCardsSettleOnResupersedeAndExpiry(t *testing.T) {
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			workFeedStore: workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl")),
		},
	}
	path := filepath.Join(t.TempDir(), "meta", "evolve-system-prompt.md.proposed")

	s.postMetaProposalCard("evolve-system-prompt.md", "producer", "first reason", path, false)
	s.postMetaProposalCard("evolve-system-prompt.md", "producer", "second reason (new cycle)", path, false)
	items, _, err := s.workFeedStore.List(10, true)
	if err != nil {
		t.Fatal(err)
	}
	var active, acked int
	for _, it := range items {
		if it.Source != metaProposalSource || it.RefID != path {
			continue
		}
		if it.Status == workfeed.StatusAcked {
			acked++
		} else {
			active++
		}
	}
	if active != 1 || acked != 1 {
		t.Fatalf("active=%d acked=%d, want exactly one live decision card", active, acked)
	}

	s.settleMetaProposalCard("evolve-system-prompt.md", path, "verdict expired after 7d")
	if ok, err := s.nativeWorkFeedStore().HasActiveSourceRef(metaProposalSource, path); err != nil || ok {
		t.Fatalf("expired card still active (ok=%v err=%v)", ok, err)
	}
}
