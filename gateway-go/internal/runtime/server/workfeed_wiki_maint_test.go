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

// A card whose finding was fixed must leave the inbox by itself. Before this,
// repairing the wiki left the card standing and the operator still had to press
// 확인 — a question already answered, re-asked.
func TestWikiMaintRetiresCardsWhoseFindingIsResolved(t *testing.T) {
	dir := t.TempDir()
	wstore, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("wiki store: %v", err)
	}
	page := wiki.NewPage("김성환", "인물", nil)
	page.Body = "- 이메일: upshgo@topsolar.kr, shkim@bmenergy.co.kr"
	if err := wstore.WritePage("인물/김성환.md", page); err != nil {
		t.Fatal(err)
	}
	feed := workfeed.NewStore(filepath.Join(dir, "feed.jsonl"))
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			wikiStore:       wstore,
			workFeedStore:   feed,
			nativeSyncStore: nativesync.NewStore(filepath.Join(dir, "sync.jsonl")),
		},
	}
	h, ok := wstore.HomonymPersonFor("인물/김성환.md")
	if !ok {
		t.Fatal("두 신원 페이지가 후보로 안 잡힘")
	}
	if err := s.postWikiMaintHomonymCard(h); err != nil {
		t.Fatal(err)
	}

	// Still open → the card stays.
	s.retireResolvedWikiMaintCards(context.Background())
	if items, _, _ := feed.List(10, false); len(items) != 1 {
		t.Fatalf("근거가 살아 있는데 카드가 회수됨: %d", len(items))
	}

	// Operator answers in the wiki (identity_reviewed) → the card must go.
	page, _ = wstore.ReadPage("인물/김성환.md")
	page.Meta.IdentityReviewed = wstore.PersonCompanyDomains("인물/김성환.md")
	if err := wstore.WritePage("인물/김성환.md", page); err != nil {
		t.Fatal(err)
	}
	s.retireResolvedWikiMaintCards(context.Background())
	if items, _, _ := feed.List(10, false); len(items) != 0 {
		t.Errorf("해소된 카드가 피드에 남음: %+v", items)
	}
}

// A ref shape the lane cannot evaluate must never be retired — silencing a card
// we did not check is worse than leaving it.
func TestWikiMaintKeepsUnknownRefTypes(t *testing.T) {
	dir := t.TempDir()
	feed := workfeed.NewStore(filepath.Join(dir, "feed.jsonl"))
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			workFeedStore:   feed,
			nativeSyncStore: nativesync.NewStore(filepath.Join(dir, "sync.jsonl")),
		},
	}
	if _, err := feed.Append(workfeed.Item{
		Source: wikiMaintSource, RefType: "something-new", RefID: "인물/x.md",
		Title: "위키 정비: 새 종류", Status: workfeed.StatusUnread,
	}); err != nil {
		t.Fatal(err)
	}
	s.retireResolvedWikiMaintCards(context.Background())
	if items, _, _ := feed.List(10, false); len(items) != 1 {
		t.Errorf("평가할 수 없는 카드를 회수함: %+v", items)
	}
}

// The lane rescans every 12h, so an unanswered finding must not accumulate a
// card per cycle — that daily restack is what the operator experienced as being
// asked again and again (08-24 and 08-25 both carried the same three people).
func TestWikiMaintDoesNotRestackAnOpenCard(t *testing.T) {
	dir := t.TempDir()
	wstore, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	page := wiki.NewPage("김성환", "인물", nil)
	page.Body = "- 이메일: upshgo@topsolar.kr, shkim@bmenergy.co.kr"
	if err := wstore.WritePage("인물/김성환.md", page); err != nil {
		t.Fatal(err)
	}
	feed := workfeed.NewStore(filepath.Join(dir, "feed.jsonl"))
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			wikiStore:       wstore,
			workFeedStore:   feed,
			nativeSyncStore: nativesync.NewStore(filepath.Join(dir, "sync.jsonl")),
		},
	}
	h, ok := wstore.HomonymPersonFor("인물/김성환.md")
	if !ok {
		t.Fatal("후보가 안 잡힘")
	}
	for i := 0; i < 3; i++ { // three 12h cycles with no operator answer
		if err := s.postWikiMaintHomonymCard(h); err != nil {
			t.Fatal(err)
		}
	}
	items, _, _ := feed.List(10, false)
	if len(items) != 1 {
		t.Errorf("사이클마다 카드가 쌓임: %d장", len(items))
	}
}
