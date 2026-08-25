package server

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	skillcore "github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle/core"
)

// Feed-card adoption closes the P2 human loop: 채택 promotes the .proposed
// into the live artifact, 기각 discards it, and either decision lands in the
// meta-experience ledger the next weekly cycle reads.
func TestHandleMetaProposalActionUpdatesArtifactOnAdoptOrReject(t *testing.T) {
	newServer := func(t *testing.T) (*Server, string) {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DENEB_STATE_DIR", "") // exercise HOME-based resolution (see TestMain)
		metaDir := filepath.Join(t.TempDir(), "meta")
		tracker, err := genesis.NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		return &Server{
			logger: slog.Default(),
			GenesisSubsystem: &GenesisSubsystem{
				genesisMeta:    skillcore.NewMetaArtifacts(metaDir, slog.Default()),
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
		// The operator adoption must carry a health snapshot so the meta
		// rollback watch covers it too (#3459).
		if ledger[0].AdoptionHealth == nil {
			t.Fatal("operator adoption missing AdoptionHealth snapshot — revert watch cannot cover it")
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

func TestLowConfidenceEvolveCardPreservesFirstOperatorVerdict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tracker, err := genesis.NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	margin := 1.5
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			workFeedStore: workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl")),
		},
		GenesisSubsystem: &GenesisSubsystem{
			genesisTracker: tracker,
			genesisEvolver: &genesis.Evolver{},
		},
	}
	result := genesis.EvolveResult{
		SkillName: "email-analysis", Evolved: true, NewVersion: "1.0.1",
		JudgeVersion: "judge-v1", JudgeMargin: &margin, NeedsOperatorVerdict: true,
	}
	s.postLowConfidenceEvolveCard(result)
	items, _, err := s.workFeedStore.List(5, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("feed items = %+v err=%v", items, err)
	}
	item := items[0]
	if item.Source != evolveVerdictSource || item.Metadata["decisionId"] != "email-analysis@1.0.1" || len(item.Actions) != 2 {
		t.Fatalf("low-confidence card = %+v", item)
	}
	if err := s.handleEvolveVerdictAction(item, evolveVerdictConfirm); err != nil {
		t.Fatal(err)
	}
	labels := tracker.RecentOperatorJudgeVerdicts(time.Hour, 5)
	if len(labels) != 1 || labels[0].Verdict != genesis.OperatorJudgeVerdictConfirm || labels[0].JudgeMargin != margin {
		t.Fatalf("operator labels = %+v", labels)
	}
	// A later tap on the opposite chip is a no-op: the first decision wins.
	if err := s.handleEvolveVerdictAction(item, evolveVerdictRollback); err != nil {
		t.Fatal(err)
	}
	labels = tracker.RecentOperatorJudgeVerdicts(time.Hour, 5)
	if len(labels) != 1 || labels[0].Verdict != genesis.OperatorJudgeVerdictConfirm {
		t.Fatalf("settled verdict changed: %+v", labels)
	}
}

func TestNativeWorkFeedVerdictFailureKeepsCardRetryable(t *testing.T) {
	store := workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	if _, err := store.Append(workfeed.Item{
		ID: "verdict", Source: evolveVerdictSource,
		Actions: []workfeed.Action{{ID: evolveVerdictConfirm, Kind: workfeed.ActionAck}},
	}); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("verdict ledger unavailable")
	feed := &nativeWorkFeedStore{
		store:           store,
		onEvolveVerdict: func(workfeed.Item, string) error { return wantErr },
	}
	if _, err := feed.RunAction("verdict", evolveVerdictConfirm, "ignored"); !errors.Is(err, wantErr) {
		t.Fatalf("RunAction error = %v, want %v", err, wantErr)
	}
	items, total, err := store.List(10, false)
	if err != nil || total != 1 || len(items) != 1 || items[0].Status == workfeed.StatusAcked {
		t.Fatalf("failed verdict consumed card: items=%+v total=%d err=%v", items, total, err)
	}
}

func TestNativeWorkFeedRelockFailureKeepsCardRetryable(t *testing.T) {
	store := workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	actionID := ladderActionRelockPrefix + "source:runtime-error"
	if _, err := store.Append(workfeed.Item{
		ID: "graduation", Source: ladderReadySource,
		Actions: []workfeed.Action{{ID: actionID, Kind: workfeed.ActionAck}},
	}); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("graduation ledger unavailable")
	feed := &nativeWorkFeedStore{
		store:          store,
		onLadderAction: func(workfeed.Item, string) error { return wantErr },
	}
	if _, err := feed.RunAction("graduation", actionID, "ignored"); !errors.Is(err, wantErr) {
		t.Fatalf("RunAction error = %v, want %v", err, wantErr)
	}
	items, total, err := store.List(10, false)
	if err != nil || total != 1 || len(items) != 1 || items[0].Status == workfeed.StatusAcked {
		t.Fatalf("failed relock consumed card: items=%+v total=%d err=%v", items, total, err)
	}
}
