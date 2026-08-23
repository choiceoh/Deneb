package server

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

func TestPostDreamWorkfeedCardCreatesCardOnlyWhenPagesChange(t *testing.T) {
	dir := t.TempDir()
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			workFeedStore:   workfeed.NewStore(filepath.Join(dir, "feed.jsonl")),
			nativeSyncStore: nativesync.NewStore(filepath.Join(dir, "sync.jsonl")),
		},
	}

	// No-change cycles and nil reports post nothing.
	s.postDreamWorkfeedCard(nil)
	s.postDreamWorkfeedCard(&autonomous.DreamReport{WikiUpdatesProposed: 3})
	items, _, err := s.workFeedStore.List(10, true)
	if err != nil || len(items) != 0 {
		t.Fatalf("no-change cycle must not post: items=%d err=%v", len(items), err)
	}

	// A page-changing cycle posts exactly one card.
	s.postDreamWorkfeedCard(&autonomous.DreamReport{
		WikiPagesCreated: 1, WikiPagesUpdated: 4, WikiUpdatesProposed: 5,
	})
	items, _, err = s.workFeedStore.List(10, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one dream card, got %d (err=%v)", len(items), err)
	}
	if items[0].Source != workfeed.SourceDream || items[0].Title != "위키 드림: 1 생성 · 4 갱신" {
		t.Errorf("card = %+v", items[0])
	}

	// A digest-only cycle (Phase 3d wrote 현재 상태 sections, no synthesis
	// creates/updates) still changed wiki pages and must post a card.
	s.postDreamWorkfeedCard(&autonomous.DreamReport{WikiProjectDigests: 3})
	items, _, err = s.workFeedStore.List(10, true)
	if err != nil || len(items) != 2 {
		t.Fatalf("digest-only cycle must post a card: items=%d err=%v", len(items), err)
	}
	if items[0].Title != "위키 드림: 프로젝트 근황 3건 갱신" {
		t.Errorf("digest-only card = %+v", items[0])
	}

	// Combined cycle mentions the digest count alongside creates/updates.
	s.postDreamWorkfeedCard(&autonomous.DreamReport{
		WikiPagesCreated: 2, WikiPagesUpdated: 1, WikiProjectDigests: 4,
	})
	items, _, err = s.workFeedStore.List(10, true)
	if err != nil || len(items) != 3 {
		t.Fatalf("combined cycle must post a card: items=%d err=%v", len(items), err)
	}
	if items[0].Title != "위키 드림: 2 생성 · 1 갱신 · 근황 4" {
		t.Errorf("combined card = %+v", items[0])
	}
}

// The card closes the 5.7 loop (corrections considered) and surfaces the W15
// 정비 signal (verify advisories) in summary + body so the operator never has
// to open the notifier message to know maintenance is pending.
func TestPostDreamWorkfeedCardSurfacesMaintFindingsAndCorrections(t *testing.T) {
	dir := t.TempDir()
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			workFeedStore:   workfeed.NewStore(filepath.Join(dir, "feed.jsonl")),
			nativeSyncStore: nativesync.NewStore(filepath.Join(dir, "sync.jsonl")),
		},
	}
	s.postDreamWorkfeedCard(&autonomous.DreamReport{
		WikiPagesUpdated:      2,
		VerifyFindings:        []string{"동일한 ID(제목 상이): a.md vs b.md — id 충돌 수리 대상"},
		VerifyFindingsRepeat:  2,
		CorrectionsConsidered: 1,
	})
	items, _, err := s.workFeedStore.List(10, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one dream card, got %d (err=%v)", len(items), err)
	}
	card := items[0]
	if !strings.Contains(card.Summary, "정정 1건 반영") || !strings.Contains(card.Summary, "정비 필요 3건") {
		t.Fatalf("summary = %q", card.Summary)
	}
	if !strings.Contains(card.Body, "## 정비 필요") || !strings.Contains(card.Body, "id 충돌 수리 대상") {
		t.Fatalf("body must list first-time findings, got %q", card.Body)
	}
	if !strings.Contains(card.Body, "이전 정정 1건") {
		t.Fatalf("body must note corrections considered, got %q", card.Body)
	}
}

// Per-page 되돌리기 chips mint from the cycle ledger in its canonical page
// order; without a ledger record (or with a single page) only the whole-cycle
// revert ships.
func TestPostDreamWorkfeedCardMintsPerPageRevertActions(t *testing.T) {
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	wstore, err := wiki.NewStore(wikiDir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("wiki store: %v", err)
	}
	// Same ledger file dream_revert.go writes (.dream-cycle-changes.jsonl).
	ledger := `{"atMs":1,"commit":"abc123","learned":["프로젝트/a/대표.md","기타/b.md"],"merged":["업무/c.md"]}` + "\n"
	if err := os.WriteFile(filepath.Join(wikiDir, ".dream-cycle-changes.jsonl"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			wikiStore:       wstore,
			workFeedStore:   workfeed.NewStore(filepath.Join(dir, "feed.jsonl")),
			nativeSyncStore: nativesync.NewStore(filepath.Join(dir, "sync.jsonl")),
		},
	}

	// Ledger-backed cycle: 확인 + 전체 되돌리기 + one mark chip per page.
	s.postDreamWorkfeedCard(&autonomous.DreamReport{WikiPagesUpdated: 3, GitCommit: "abc123"})
	items, _, err := s.workFeedStore.List(10, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one dream card, got %d (err=%v)", len(items), err)
	}
	ids := make([]string, 0, len(items[0].Actions))
	for _, a := range items[0].Actions {
		ids = append(ids, a.ID)
	}
	want := []string{
		"dream:ack",
		"dream:revert:abc123",
		"dream:revert-page:abc123:0",
		"dream:revert-page:abc123:1",
		"dream:revert-page:abc123:2",
	}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("action ids = %v, want %v", ids, want)
	}
	if !strings.Contains(items[0].Actions[2].Label, "학습 · 대표") {
		t.Fatalf("page chip label = %q, want op + page name", items[0].Actions[2].Label)
	}
	if items[0].Actions[1].Kind != workfeed.ActionAck {
		t.Fatalf("whole-cycle revert kind = %q, want ack (the card settles — its work is done)", items[0].Actions[1].Kind)
	}
	if items[0].Actions[2].Kind != workfeed.ActionMark {
		t.Fatalf("page chip kind = %q, want mark (card survives page repairs)", items[0].Actions[2].Kind)
	}

	// No ledger for the commit → only 확인 + 전체 되돌리기.
	s.postDreamWorkfeedCard(&autonomous.DreamReport{WikiPagesUpdated: 1, GitCommit: "zzz999"})
	items, _, err = s.workFeedStore.List(10, true)
	if err != nil || len(items) != 2 {
		t.Fatalf("expected two dream cards, got %d (err=%v)", len(items), err)
	}
	if got := len(items[0].Actions); got != 2 {
		t.Fatalf("ledger-less cycle actions = %d, want 2", got)
	}
}
