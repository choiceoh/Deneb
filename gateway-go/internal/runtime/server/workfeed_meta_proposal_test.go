package server

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// Feed-card adoption closes the P2 human loop: 채택 promotes the .proposed
// into the live artifact, 기각 discards it, and either decision lands in the
// meta-experience ledger the next weekly cycle reads.
func TestHandleMetaProposalAction(t *testing.T) {
	newServer := func(t *testing.T) (*Server, string) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		metaDir := filepath.Join(t.TempDir(), "meta")
		tracker, err := genesis.NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		return &Server{
			logger: slog.Default(),
			GenesisSubsystem: &GenesisSubsystem{
				genesisMeta:    generation.NewMetaArtifacts(metaDir, slog.Default()),
				genesisTracker: tracker,
			},
		}, metaDir
	}
	proposal := strings.Repeat("proposed prompt revision. ", 20)
	item := func(metaDir string) workfeed.Item {
		return workfeed.Item{Source: "genesis-meta", RefID: filepath.Join(metaDir, "prompt.md.proposed")}
	}

	t.Run("adopt promotes the proposal and records the decision", func(t *testing.T) {
		s, metaDir := newServer(t)
		if _, err := s.genesisMeta.WriteProposal("prompt.md", proposal); err != nil {
			t.Fatal(err)
		}
		s.handleMetaProposalAction(item(metaDir), "meta:adopt")

		if got := s.genesisMeta.Load("prompt.md", "fallback"); got != strings.TrimSpace(proposal) {
			t.Fatalf("live artifact = %q, want adopted proposal", got)
		}
		if _, err := os.Stat(filepath.Join(metaDir, "prompt.md.proposed")); err == nil {
			t.Fatal(".proposed not removed after adoption")
		}
		ledger, err := s.genesisTracker.RecentMetaRevisions(5)
		if err != nil || len(ledger) != 1 || ledger[0].Action != "adopted" || ledger[0].ToVersion == "" {
			t.Fatalf("ledger = %+v, err=%v", ledger, err)
		}
	})

	t.Run("reject discards the proposal and keeps the live artifact", func(t *testing.T) {
		s, metaDir := newServer(t)
		if _, err := s.genesisMeta.WriteProposal("prompt.md", proposal); err != nil {
			t.Fatal(err)
		}
		s.handleMetaProposalAction(item(metaDir), "meta:reject")

		if got := s.genesisMeta.Load("prompt.md", "fallback"); got != "fallback" {
			t.Fatalf("live artifact changed on reject: %q", got)
		}
		if _, err := os.Stat(filepath.Join(metaDir, "prompt.md.proposed")); err == nil {
			t.Fatal(".proposed not removed after rejection")
		}
		ledger, err := s.genesisTracker.RecentMetaRevisions(5)
		if err != nil || len(ledger) != 1 || ledger[0].Action != "rejected" {
			t.Fatalf("ledger = %+v, err=%v", ledger, err)
		}
	})

	t.Run("missing proposal and junk ref are safe no-ops", func(t *testing.T) {
		s, metaDir := newServer(t)
		s.handleMetaProposalAction(item(metaDir), "meta:adopt") // no .proposed on disk
		s.handleMetaProposalAction(workfeed.Item{Source: "genesis-meta", RefID: ""}, "meta:adopt")
		if ledger, _ := s.genesisTracker.RecentMetaRevisions(5); len(ledger) != 0 {
			t.Fatalf("no-op paths wrote ledger entries: %+v", ledger)
		}
	})
}
