package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// The digest posts at most weekly and only when fresh dream reports exist:
// first run baselines silently, the weekly gap gates posting, and the state
// bump only follows a successful card post.
// The text sparkline scales to the series' own range (a flat series reads
// flat, a slide reads as a slide) and the digest body carries the trend line.
func TestSparklineBarsScalesToSeriesRange(t *testing.T) {
	if got := sparklineBars(nil); got != "" {
		t.Fatalf("empty series = %q", got)
	}
	if got := sparklineBars([]float64{60, 70, 80, 90}); got != "▁ ▃ ▅ █" {
		t.Fatalf("rising series = %q, want ▁ ▃ ▅ █", got)
	}
	if got := sparklineBars([]float64{90, 60}); got != "█ ▁" {
		t.Fatalf("falling series = %q, want █ ▁", got)
	}
	if got := sparklineBars([]float64{75, 75}); got != "▅ ▅" {
		t.Fatalf("flat series = %q, want mid bars ▅ ▅", got)
	}
}

func TestAggregateDreamReportsBuildsQualityTrend(t *testing.T) {
	entries := []dreamReportEntry{
		{AtMs: 1, Report: autonomous.DreamReport{WikiPagesUpdated: 1, QualityScore: 60}},
		{AtMs: 2, Report: autonomous.DreamReport{WikiPagesUpdated: 1}}, // unscored → skipped
		{AtMs: 3, Report: autonomous.DreamReport{WikiPagesUpdated: 1, QualityScore: 80}},
	}
	stats := aggregateDreamReports(entries, 0, 10)
	if len(stats.QualityTrend) != 2 || stats.QualityTrend[0] != 60 || stats.QualityTrend[1] != 80 {
		t.Fatalf("trend = %v, want [60 80]", stats.QualityTrend)
	}
	if stats.QualityAvg != 70 {
		t.Fatalf("avg = %v, want 70", stats.QualityAvg)
	}
}

func TestDreamDigestTaskPostsWeeklyOnlyWithFreshReports(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("DENEB_STATE_DIR", dir)
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			workFeedStore:   workfeed.NewStore(filepath.Join(dir, "feed.jsonl")),
			nativeSyncStore: nativesync.NewStore(filepath.Join(dir, "sync.jsonl")),
		},
	}
	task := &DreamDigestTask{Server: s}

	// Standing rollup + first run: baselines silently, no card.
	appendTestDreamReport(t, 10, 4, 2)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertFeedCount(t, s, 0)

	// Fresh reports but the weekly gap has not elapsed: still silent.
	appendTestDreamReport(t, 1, 0, 0)
	backdateDreamDigestState(t, 23*time.Hour)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertFeedCount(t, s, 0)

	// Weekly gap elapsed with fresh reports: exactly one digest card.
	backdateDreamDigestState(t, 8*24*time.Hour)
	appendTestDreamReport(t, 3, 1, 1)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	items := listFeed(t, s)
	if len(items) != 1 {
		t.Fatalf("expected one digest card, got %d", len(items))
	}
	card := items[0]
	if card.Source != dreamDigestSource || len(card.Actions) != 2 {
		t.Fatalf("card = %+v", card)
	}
	if card.Actions[0].Kind != workfeed.ActionAck || card.Actions[1].Kind != workfeed.ActionFollowUp {
		t.Fatalf("actions = %+v", card.Actions)
	}
	if !strings.Contains(card.Actions[1].Prompt, "다이제스트") {
		t.Fatalf("feedback prompt must carry the digest summary: %q", card.Actions[1].Prompt)
	}
	if !strings.Contains(card.Body, "학습 14") || !strings.Contains(card.Body, "3회") { // all window reports aggregate
		t.Fatalf("body must aggregate the window: %q", card.Body)
	}

	// Repeat run immediately: state was bumped, nothing new to post.
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertFeedCount(t, s, 1)

	// Weekly gap elapsed but no fresh reports: still silent.
	backdateDreamDigestState(t, 8*24*time.Hour)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertFeedCount(t, s, 1)
}

func appendTestDreamReport(t *testing.T, learned, merged, expired int) {
	t.Helper()
	s := &Server{logger: slog.Default()}
	s.appendDreamReport(&autonomous.DreamReport{
		FactsLearned: learned,
		FactsMerged:  merged,
		FactsExpired: expired,
	})
}

// backdateDreamDigestState rewrites the digest state so the last digest
// appears older than gap (simulating elapsed time without sleeping).
func backdateDreamDigestState(t *testing.T, age time.Duration) {
	t.Helper()
	state := dreamDigestState{LastDigestAtMs: time.Now().Add(-age).UnixMilli()}
	out, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dreamDigestStatePath(), out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFeedCount(t *testing.T, s *Server, want int) {
	t.Helper()
	items, _, err := s.workFeedStore.List(10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != want {
		t.Fatalf("expected %d feed items, got %d", want, len(items))
	}
}

func listFeed(t *testing.T, s *Server) []workfeed.Item {
	t.Helper()
	items, _, err := s.workFeedStore.List(10, true)
	if err != nil {
		t.Fatal(err)
	}
	return items
}
