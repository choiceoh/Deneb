package wiki

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

func newCompareDreamer(t *testing.T) *WikiDreamer {
	t.Helper()
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { store.Close() })
	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	wd.SetWorkspaceDir(t.TempDir())
	wd.SetRulesEvolution(true)
	return wd
}

// The compiled default rules must always satisfy the contract gate — if this
// fails, either the gate or the default rules drifted and every revision
// would be rejected (or worse, an invariant line was dropped from the rules).
func TestValidateDreamRules_DefaultRulesPass(t *testing.T) {
	if err := validateDreamRules(defaultWikiSynthesisRules); err != nil {
		t.Fatalf("default synthesis rules must pass the contract gate: %v", err)
	}
}

func TestValidateDreamRules_RejectsInvariantLoss(t *testing.T) {
	stripped := strings.ReplaceAll(defaultWikiSynthesisRules, "JSON 배열만", "자유 형식으로")
	if err := validateDreamRules(stripped); err == nil {
		t.Error("dropping the output contract must fail the gate")
	}
	if err := validateDreamRules("## 다른 헤더\n" + defaultWikiSynthesisRules); err == nil {
		t.Error("wrong leading header must fail the gate")
	}
	if err := validateDreamRules("## 규칙\n짧음"); err == nil {
		t.Error("undersized rules must fail the gate")
	}
}

func TestParseDreamCompareVerdict_NormalizesDrift(t *testing.T) {
	v, err := parseDreamCompareVerdict("```json\n{\"winner\":\"둘 다\",\"weaknesses\":[\"라우팅오류\",\"자유텍스트\",\"중복제안\",\"출처부실\",\"과잉제안\"],\"rationale\":\"근거\"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if v.Winner != "tie" {
		t.Errorf("unknown winner should default to tie, got %q", v.Winner)
	}
	if len(v.Weaknesses) != 3 {
		t.Fatalf("weaknesses must filter to vocab and cap at 3: %v", v.Weaknesses)
	}
	for _, w := range v.Weaknesses {
		if w == "자유텍스트" {
			t.Error("out-of-vocab weakness must be dropped")
		}
	}
}

func TestRefineDreamCompareVerdict_StripsUndershootWhenProjectWritten(t *testing.T) {
	v := dreamCompareVerdict{Winner: "previous", Weaknesses: []string{"과소제안", "라우팅오류"}}
	got := refineDreamCompareVerdict(v, &dreamCycle{
		proposal: dreamProposalReport{Proposed: []dreamUpdatePreview{{Path: "프로젝트/pl2-kia-epc-002/대표.md"}}},
	})
	if len(got.Weaknesses) != 1 || got.Weaknesses[0] != "라우팅오류" {
		t.Fatalf("과소제안 must drop when a project page was written, got %v", got.Weaknesses)
	}

	kept := refineDreamCompareVerdict(v, &dreamCycle{
		proposal: dreamProposalReport{Proposed: []dreamUpdatePreview{{Path: "기타/이란.md"}}},
	})
	if len(kept.Weaknesses) != 2 {
		t.Fatalf("기타-only cycle must keep 과소제안, got %v", kept.Weaknesses)
	}
}

func TestDreamCompareLedger_RoundTripAndMalformedSkip(t *testing.T) {
	wd := newCompareDreamer(t)
	if err := wd.appendDreamCompareEntry(dreamCompareEntry{Ts: 1, Kind: "compare", Winner: "current"}); err != nil {
		t.Fatal(err)
	}
	if err := wd.appendDreamCompareEntry(dreamCompareEntry{Ts: 2, Kind: "revision", FromHash: "a", ToHash: "b"}); err != nil {
		t.Fatal(err)
	}
	// Inject a malformed line between valid ones.
	f, err := os.OpenFile(wd.dreamComparePath(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{broken json\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := wd.appendDreamCompareEntry(dreamCompareEntry{Ts: 3, Kind: "compare", Winner: "previous"}); err != nil {
		t.Fatal(err)
	}

	entries := wd.readDreamCompareEntries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 valid entries (malformed skipped), got %d", len(entries))
	}
	if entries[1].Kind != "revision" || entries[2].Winner != "previous" {
		t.Errorf("entries mangled: %+v", entries)
	}
}

func TestRotateDreamCompareLedger_KeepsTail(t *testing.T) {
	wd := newCompareDreamer(t)
	for i := 0; i < dreamCompareLedgerKeep+50; i++ {
		if err := wd.appendDreamCompareEntry(dreamCompareEntry{Ts: int64(i), Kind: "compare", Winner: "tie"}); err != nil {
			t.Fatal(err)
		}
	}
	wd.rotateDreamCompareLedger(wd.dreamComparePath())
	entries := wd.readDreamCompareEntries()
	if len(entries) != dreamCompareLedgerKeep {
		t.Fatalf("rotation should keep %d entries, got %d", dreamCompareLedgerKeep, len(entries))
	}
	if entries[0].Ts != 50 {
		t.Errorf("rotation must keep the TAIL (oldest kept ts=50), got %d", entries[0].Ts)
	}
}

// Below the comparison floor, or without a recurring weakness, no revision is
// attempted (returns before the LLM guard, so a nil client proves the early
// return).
func TestMaybeReviseDreamRules_ConditionsHold(t *testing.T) {
	wd := newCompareDreamer(t)
	now := dentime.Now().UnixMilli()

	// Too few comparisons.
	for i := 0; i < dreamRulesMinComparisons-1; i++ {
		mustAppend(t, wd, dreamCompareEntry{Ts: now, Kind: "compare", Winner: "current", Weaknesses: []string{"라우팅오류"}})
	}
	if err := wd.maybeReviseDreamRules(context.Background()); err != nil {
		t.Fatalf("below-floor history must be a quiet no-op: %v", err)
	}

	// Enough comparisons but no weakness recurs (client nil → would panic if
	// it tried the LLM call; reaching the client guard without error is the
	// assertion).
	wd2 := newCompareDreamer(t)
	for i := 0; i < dreamRulesMinComparisons; i++ {
		mustAppend(t, wd2, dreamCompareEntry{Ts: now, Kind: "compare", Winner: "tie"})
	}
	if err := wd2.maybeReviseDreamRules(context.Background()); err != nil {
		t.Fatalf("no recurring weakness must be a quiet no-op: %v", err)
	}
}

// A post-revision loss streak restores the .bak and records a rollback event
// — no LLM involved.
func TestMaybeReviseDreamRules_RollbackOnLossStreak(t *testing.T) {
	wd := newCompareDreamer(t)
	rulesPath := filepath.Join(wd.workspaceDir, wikiDreamRulesFile)
	if err := os.WriteFile(rulesPath+".bak", []byte("## 규칙\n(이전 규칙)"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rulesPath, []byte("## 규칙\n(개정된 규칙 — 퇴행)"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := dentime.Now().UnixMilli()
	mustAppend(t, wd, dreamCompareEntry{Ts: now - 400, Kind: "revision", FromHash: "old", ToHash: "new"})
	mustAppend(t, wd, dreamCompareEntry{Ts: now - 300, Kind: "compare", Winner: "previous"})
	mustAppend(t, wd, dreamCompareEntry{Ts: now - 200, Kind: "compare", Winner: "tie"})
	mustAppend(t, wd, dreamCompareEntry{Ts: now - 100, Kind: "compare", Winner: "previous"})

	if err := wd.maybeReviseDreamRules(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != "## 규칙\n(이전 규칙)" {
		t.Errorf("rules must be restored from .bak, got %q", restored)
	}
	entries := wd.readDreamCompareEntries()
	last := entries[len(entries)-1]
	if last.Kind != "rollback" || last.FromHash != "new" || last.ToHash != "old" {
		t.Errorf("rollback event wrong: %+v", last)
	}

	// Idempotence: the rollback event now terminates the era, so a second
	// pass must not roll back again.
	if err := wd.maybeReviseDreamRules(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries = wd.readDreamCompareEntries()
	if entries[len(entries)-1].Kind != "rollback" || len(entries) != 5 {
		t.Errorf("second pass must not append another event: %d entries", len(entries))
	}
}

// A revision followed by winning comparisons must NOT roll back.
func TestMaybeReviseDreamRules_NoRollbackWhenWinning(t *testing.T) {
	wd := newCompareDreamer(t)
	rulesPath := filepath.Join(wd.workspaceDir, wikiDreamRulesFile)
	if err := os.WriteFile(rulesPath, []byte("## 규칙\n(개정된 규칙)"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := dentime.Now().UnixMilli()
	mustAppend(t, wd, dreamCompareEntry{Ts: now - 400, Kind: "revision", FromHash: "old", ToHash: "new"})
	mustAppend(t, wd, dreamCompareEntry{Ts: now - 300, Kind: "compare", Winner: "previous"})
	mustAppend(t, wd, dreamCompareEntry{Ts: now - 200, Kind: "compare", Winner: "current"})
	mustAppend(t, wd, dreamCompareEntry{Ts: now - 100, Kind: "compare", Winner: "previous"})

	if err := wd.maybeReviseDreamRules(context.Background()); err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != "## 규칙\n(개정된 규칙)" {
		t.Error("a current-win inside the window must veto the rollback")
	}
}

func TestCaptureDreamSelfComparison_DisabledIsNoOp(t *testing.T) {
	wd := newCompareDreamer(t)
	wd.SetRulesEvolution(false)
	cycle := &dreamCycle{
		proposal:     dreamProposalReport{Proposed: []dreamUpdatePreview{{Path: "업무/a.md"}}},
		prevProposal: &dreamProposalReport{Proposed: []dreamUpdatePreview{{Path: "업무/b.md"}}},
	}
	wd.captureDreamSelfComparison(context.Background(), cycle)
	if entries := wd.readDreamCompareEntries(); len(entries) != 0 {
		t.Errorf("disabled lane must write nothing, got %d entries", len(entries))
	}
	if len(cycle.phaseErrors) != 0 {
		t.Errorf("disabled lane must not error: %v", cycle.phaseErrors)
	}
}

func TestLoadPrevDreamProposal_RoundTrip(t *testing.T) {
	wd := newCompareDreamer(t)
	if got := wd.loadPrevDreamProposal(); got != nil {
		t.Fatalf("missing report should load nil, got %+v", got)
	}
	report := dreamProposalReport{GeneratedAt: "2026-07-20T00:00:00Z", Proposed: []dreamUpdatePreview{{Path: "업무/x.md", Title: "X"}}}
	if err := wd.saveDreamProposalReport(report); err != nil {
		t.Fatal(err)
	}
	got := wd.loadPrevDreamProposal()
	if got == nil || len(got.Proposed) != 1 || got.Proposed[0].Path != "업무/x.md" {
		t.Errorf("round trip mangled: %+v", got)
	}
}

func TestTopWeakness_DeterministicTieBreak(t *testing.T) {
	counts := map[string]int{"과잉제안": 2, "라우팅오류": 2}
	if w, c := topWeakness(counts); w != "라우팅오류" || c != 2 {
		t.Errorf("ties must break by vocabulary order, got %s/%d", w, c)
	}
}

func mustAppend(t *testing.T, wd *WikiDreamer, e dreamCompareEntry) {
	t.Helper()
	if err := wd.appendDreamCompareEntry(e); err != nil {
		t.Fatal(err)
	}
}
