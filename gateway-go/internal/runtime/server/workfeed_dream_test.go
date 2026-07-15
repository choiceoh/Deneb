package server

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
)

func TestPostDreamWorkfeedCardCreatesCardOnlyWhenPagesChange(t *testing.T) {
	dir := t.TempDir()
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			workFeedStore:   domainbind.NewWorkFeedStore(filepath.Join(dir, "feed.jsonl")),
			nativeSyncStore: domainbind.NewNativeSyncStore(filepath.Join(dir, "sync.jsonl")),
		},
	}

	// No-change cycles and nil reports post nothing.
	s.postDreamWorkfeedCard(nil)
	s.postDreamWorkfeedCard(&domainbind.DreamReport{WikiUpdatesProposed: 3})
	items, _, err := s.workFeedStore.List(10, true)
	if err != nil || len(items) != 0 {
		t.Fatalf("no-change cycle must not post: items=%d err=%v", len(items), err)
	}

	// A page-changing cycle posts exactly one card.
	s.postDreamWorkfeedCard(&domainbind.DreamReport{
		WikiPagesCreated: 1, WikiPagesUpdated: 4, WikiUpdatesProposed: 5,
	})
	items, _, err = s.workFeedStore.List(10, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one dream card, got %d (err=%v)", len(items), err)
	}
	if items[0].Source != domainbind.SourceDream || items[0].Title != "위키 드림: 1 생성 · 4 갱신" {
		t.Errorf("card = %+v", items[0])
	}

	// A digest-only cycle (Phase 3d wrote 현재 상태 sections, no synthesis
	// creates/updates) still changed wiki pages and must post a card.
	s.postDreamWorkfeedCard(&domainbind.DreamReport{WikiProjectDigests: 3})
	items, _, err = s.workFeedStore.List(10, true)
	if err != nil || len(items) != 2 {
		t.Fatalf("digest-only cycle must post a card: items=%d err=%v", len(items), err)
	}
	if items[0].Title != "위키 드림: 프로젝트 근황 3건 갱신" {
		t.Errorf("digest-only card = %+v", items[0])
	}

	// Combined cycle mentions the digest count alongside creates/updates.
	s.postDreamWorkfeedCard(&domainbind.DreamReport{
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
