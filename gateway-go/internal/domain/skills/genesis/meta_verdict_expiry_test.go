package genesis

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

// A propose-only / low-confidence proposal waits for an operator verdict that,
// in practice, never comes (2026-07-13 → 08-22: zero feed-card verdicts for the
// whole calibration window). The verdict window must close on its own: the
// stale file is discarded, the ledger says "expired" (not adopted, not
// rejected), the card is settled through the callback, and a fresh proposal is
// left alone.
func TestExpireStaleProposalsDiscardsOldPendingAndKeepsFresh(t *testing.T) {
	tr := newTestTracker(t)
	metaDir := filepath.Join(t.TempDir(), "meta")
	meta := generation.NewMetaArtifacts(metaDir, slog.Default())
	var settled []string
	task := &MetaEvolutionTask{
		Meta:    meta,
		Tracker: tr,
		OnProposalExpired: func(artifact, path, reason string) {
			settled = append(settled, artifact+"|"+filepath.Base(path)+"|"+reason)
		},
	}
	now := time.Now()

	// Stale: proposed 9 days ago (ledger-dated), still pending.
	if _, err := meta.WriteProposal(generation.MetaEvolveSystemPrompt, strings.Repeat("stale proposal. ", 20)); err != nil {
		t.Fatal(err)
	}
	if err := tr.LogMetaRevision(MetaRevisionRecord{
		Epoch: metaEpochProducer, Artifact: generation.MetaEvolveSystemPrompt, Proposed: true, ToVersion: "abc",
		Reason:    "[저신뢰: shadow bench margin 67.08→67.08 — 운영자 verdict 대기]",
		CreatedAt: now.Add(-9 * 24 * time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	// Fresh: proposed yesterday.
	if _, err := meta.WriteProposal(generation.MetaSkillJudgeSystemPrompt, strings.Repeat("fresh proposal. ", 20)); err != nil {
		t.Fatal(err)
	}
	if err := tr.LogMetaRevision(MetaRevisionRecord{
		Epoch: metaEpochEvaluator, Artifact: generation.MetaSkillJudgeSystemPrompt, Proposed: true, ToVersion: "def",
		CreatedAt: now.Add(-24 * time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}

	if got := task.expireStaleProposals(slog.Default(), now); got != 1 {
		t.Fatalf("expired = %d, want 1", got)
	}
	if _, _, ok := meta.ProposalInfo(generation.MetaEvolveSystemPrompt); ok {
		t.Fatal("stale .proposed must be discarded")
	}
	if _, _, ok := meta.ProposalInfo(generation.MetaSkillJudgeSystemPrompt); !ok {
		t.Fatal("fresh .proposed must survive")
	}
	if len(settled) != 1 || !strings.HasPrefix(settled[0], generation.MetaEvolveSystemPrompt+"|"+generation.MetaEvolveSystemPrompt+".proposed|verdict expired") {
		t.Fatalf("settle callback = %v", settled)
	}
	revs, err := tr.RecentMetaRevisions(5)
	if err != nil || len(revs) == 0 {
		t.Fatalf("ledger: %v / %v", revs, err)
	}
	newest := revs[0]
	if newest.Artifact != generation.MetaEvolveSystemPrompt || newest.Action != metaActionExpired || newest.FromVersion == "" {
		t.Fatalf("expiry row = %+v", newest)
	}
	// "No verdict" is its own label: counted, but never a decision.
	sig := tr.operatorUtilitySignals()
	if sig.Expired7d != 1 || sig.Adopted7d != 0 || sig.Rejected7d != 0 || sig.LastDecisionAt != 0 {
		t.Fatalf("utility signals = %+v", sig)
	}
	// Idempotent: a second sweep finds nothing stale.
	if got := task.expireStaleProposals(slog.Default(), now); got != 0 {
		t.Fatalf("second sweep expired %d", got)
	}
}

// Legacy pending files without a ledger row fall back to the file's mtime.
func TestExpireStaleProposalsUsesFileAgeWithoutLedgerRow(t *testing.T) {
	tr := newTestTracker(t)
	metaDir := filepath.Join(t.TempDir(), "meta")
	meta := generation.NewMetaArtifacts(metaDir, slog.Default())
	task := &MetaEvolutionTask{Meta: meta, Tracker: tr}
	path, err := meta.WriteProposal(generation.MetaGenesisSystemPrompt, strings.Repeat("orphan proposal. ", 20))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if got := task.expireStaleProposals(slog.Default(), time.Now()); got != 1 {
		t.Fatalf("expired = %d, want 1", got)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("orphan .proposed must be discarded")
	}
}

// The window is an operator knob; an unparsable or non-positive value keeps
// the default.
func TestMetaVerdictExpiryEnvOverride(t *testing.T) {
	t.Setenv("DENEB_META_VERDICT_EXPIRY_DAYS", "")
	if got := metaVerdictExpiry(); got != defaultMetaVerdictExpiryDays*24*time.Hour {
		t.Fatalf("default = %v", got)
	}
	t.Setenv("DENEB_META_VERDICT_EXPIRY_DAYS", "3")
	if got := metaVerdictExpiry(); got != 3*24*time.Hour {
		t.Fatalf("override = %v", got)
	}
	t.Setenv("DENEB_META_VERDICT_EXPIRY_DAYS", "-1")
	if got := metaVerdictExpiry(); got != defaultMetaVerdictExpiryDays*24*time.Hour {
		t.Fatalf("bad override = %v", got)
	}
}
