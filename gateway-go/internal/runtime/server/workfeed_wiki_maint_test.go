package server

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

func newWikiMaintTestServer(t *testing.T) (*Server, *WikiMaintTask) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("DENEB_STATE_DIR", dir)
	wstore, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("wiki store: %v", err)
	}
	person := wiki.NewPage("홍길동", "인물", nil)
	person.Meta.Emails = []string{"hong@topsolar.kr"}
	person.Body = "담당: 태안 프로젝트"
	if err := wstore.WritePage("인물/홍길동.md", person); err != nil {
		t.Fatalf("person page: %v", err)
	}
	mail := wiki.NewPage("견적 요청", "프로젝트", nil)
	mail.Body = "**발신** 홍길동 <hong@newco.com>\n\n홍길동 님이 견적 스펙 변경을 요청했습니다."
	if err := wstore.WritePage("프로젝트/pl9-tst-001/메일분석/견적-요청.md", mail); err != nil {
		t.Fatalf("mail analysis page: %v", err)
	}
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			wikiStore:       wstore,
			workFeedStore:   workfeed.NewStore(filepath.Join(dir, "feed.jsonl")),
			nativeSyncStore: nativesync.NewStore(filepath.Join(dir, "sync.jsonl")),
		},
	}
	return s, &WikiMaintTask{Server: s}
}

// One conflict → one decision card; an explicit operator decision quiets the
// same finding for 30 days even after the card is settled.
func TestWikiMaintTaskPostsCardAndDecisionQuiets(t *testing.T) {
	s, task := newWikiMaintTestServer(t)
	ctx := context.Background()

	if err := task.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	items, _, err := s.workFeedStore.List(10, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one wiki-maint card, got %d (err=%v)", len(items), err)
	}
	card := items[0]
	if card.Source != wikiMaintSource || card.RefID != "인물/홍길동.md" {
		t.Fatalf("card = %+v", card)
	}
	if !card.Question || len(card.Actions) != 2 {
		t.Fatalf("card must be a question card with approve/reject chips: %+v", card)
	}
	if !strings.Contains(card.Body, "hong@newco.com") || !strings.Contains(card.Body, "자동 판단하지 않습니다") {
		t.Fatalf("body = %q", card.Body)
	}

	// A re-run while the card is still active posts nothing new.
	if err := task.Run(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if items, _, _ = s.workFeedStore.List(10, true); len(items) != 1 {
		t.Fatalf("re-run duplicated the card: %d items", len(items))
	}

	// Operator decision + settle, then the same finding stays quiet.
	if err := s.handleWikiMaintCardAction(card, workfeedApproveAction, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := s.workFeedStore.Ack(card.ID); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if err := task.Run(ctx); err != nil {
		t.Fatalf("post-decision run: %v", err)
	}
	if items, _, _ = s.workFeedStore.List(10, true); len(items) != 1 {
		t.Fatalf("decided finding must stay quiet for 30d, got %d items", len(items))
	}

	// Unsupported actions keep the card unsettled (error).
	if err := s.handleWikiMaintCardAction(card, "bogus", ""); err == nil {
		t.Fatal("unsupported action must error")
	}
}
