package server

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// The same person page can raise both a contact-conflict finding and a homonym
// finding. Deciding one must not silence the other, so the homonym decision is
// keyed apart from the page path the conflict card uses.
func TestHomonymDecisionKeyIsNamespaced(t *testing.T) {
	page := "인물/김성환.md"
	if got := homonymDecisionKey(page); got == page {
		t.Fatalf("homonym decision key collides with the conflict key: %q", got)
	}
	if !strings.HasSuffix(homonymDecisionKey(page), page) {
		t.Errorf("key %q should still name the page", homonymDecisionKey(page))
	}
}

// An automatic split caused the 2026-07-28 over-merge incident, so the card's
// whole job is putting the evidence in front of a human — never an apply action.
func TestPostWikiMaintHomonymCardNamesEvidenceAndProposesNothing(t *testing.T) {
	s, _ := newWikiMaintTestServer(t)
	h := wiki.HomonymPerson{
		PagePath: "인물/김성환.md",
		Title:    "김성환",
		Domains:  []string{"bmenergy.co.kr", "newco.com"},
	}

	if err := s.postWikiMaintHomonymCard(h); err != nil {
		t.Fatalf("postWikiMaintHomonymCard: %v", err)
	}

	items, _, err := s.workFeedStore.List(10, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("posted %d cards (err=%v), want 1", len(items), err)
	}
	card := items[0]
	if card.Source != wikiMaintSource || card.RefType != "person-homonym" {
		t.Errorf("card source/refType = %q/%q", card.Source, card.RefType)
	}
	if card.RefID != homonymDecisionKey(h.PagePath) {
		t.Errorf("card refID = %q, want the namespaced key", card.RefID)
	}
	if !card.Question || len(card.Actions) != 2 {
		t.Errorf("card must be a question card with two ack chips: %+v", card)
	}
	for _, want := range h.Domains {
		if !strings.Contains(card.Body, want) {
			t.Errorf("card body omits the evidence %q:\n%s", want, card.Body)
		}
	}
	if !strings.Contains(card.Body, "자동 분리는 하지 않습니다") {
		t.Errorf("card body must say the split stays manual:\n%s", card.Body)
	}
}

// A homonym decision quiets that finding for 30 days; the task must not re-post.
func TestWikiMaintTaskHomonymDecisionQuiets(t *testing.T) {
	s, task := newWikiMaintTestServer(t)
	h := wiki.HomonymPerson{PagePath: "인물/김성환.md", Title: "김성환", Domains: []string{"a.co.kr", "b.com"}}
	if err := s.postWikiMaintHomonymCard(h); err != nil {
		t.Fatalf("post: %v", err)
	}
	items, _, _ := s.workFeedStore.List(10, true)
	if err := s.handleWikiMaintCardAction(items[0], workfeedApproveAction, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, it := range mustList(t, s) {
		if it.RefType == "person-homonym" && it.RefID == homonymDecisionKey(h.PagePath) && it.ID != items[0].ID {
			t.Fatalf("re-posted a decided homonym finding: %+v", it)
		}
	}
}

func mustList(t *testing.T, s *Server) []workfeed.Item {
	t.Helper()
	items, _, err := s.workFeedStore.List(20, true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return items
}
