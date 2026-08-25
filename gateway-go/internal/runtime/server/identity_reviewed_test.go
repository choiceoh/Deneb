package server

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

func newIdentityAckServer(t *testing.T) (*Server, *wiki.Store) {
	t.Helper()
	dir := t.TempDir()
	wstore, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("wiki store: %v", err)
	}
	return &Server{
		logger:          slog.Default(),
		MemorySubsystem: &MemorySubsystem{wikiStore: wstore},
	}, wstore
}

func writePersonT(t *testing.T, s *wiki.Store, rel, title, body string) {
	t.Helper()
	page := wiki.NewPage(title, "인물", nil)
	page.Body = body
	if err := s.WritePage(rel, page); err != nil {
		t.Fatalf("WritePage %q: %v", rel, err)
	}
}

// The chip stamps what the page claims TODAY, not the evidence printed on a
// possibly stale card — stamping the card's copy would silence a domain the
// operator never saw.
func TestMarkIdentityReviewed_StampsCurrentEvidenceAndEndsTheQuestion(t *testing.T) {
	s, store := newIdentityAckServer(t)
	writePersonT(t, store, "인물/김성환.md", "김성환",
		"- 이메일: upshgo@topsolar.kr, shkim@bmenergy.co.kr")

	s.markIdentityReviewed(workfeed.Item{}, "identity_reviewed:homonym:인물/김성환.md")

	page, err := store.ReadPage("인물/김성환.md")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(page.Meta.IdentityReviewed) != 2 {
		t.Fatalf("identity_reviewed = %v, want both company domains", page.Meta.IdentityReviewed)
	}
	if got := store.HomonymPersonPages(5); len(got) != 0 {
		t.Errorf("확인했는데 스캔이 계속 물어봄: %+v", got)
	}
}

func TestMarkIdentityReviewed_DuplicateGroupRecordsEachPeer(t *testing.T) {
	s, store := newIdentityAckServer(t)
	writePersonT(t, store, "인물/이영민.md", "이영민", "- 이메일: a@one.co.kr")
	writePersonT(t, store, "인물/이영민-차장.md", "이영민 차장", "- 이메일: b@two.co.kr")

	s.markIdentityReviewed(workfeed.Item{},
		"identity_reviewed:person-duplicate:인물/이영민.md|인물/이영민-차장.md")

	for path, peer := range map[string]string{
		"인물/이영민.md":    "dup:인물/이영민-차장.md",
		"인물/이영민-차장.md": "dup:인물/이영민.md",
	} {
		page, err := store.ReadPage(path)
		if err != nil {
			t.Fatalf("ReadPage %q: %v", path, err)
		}
		if len(page.Meta.IdentityReviewed) != 1 || page.Meta.IdentityReviewed[0] != peer {
			t.Errorf("%s identity_reviewed = %v, want [%s]", path, page.Meta.IdentityReviewed, peer)
		}
	}
	if got := store.DuplicatePersonGroups(5); len(got) != 0 {
		t.Errorf("양쪽 확인 후에도 그룹이 남음: %+v", got)
	}
}

// A malformed or empty action must never write.
func TestMarkIdentityReviewed_IgnoresMalformedActions(t *testing.T) {
	s, store := newIdentityAckServer(t)
	writePersonT(t, store, "인물/김성환.md", "김성환", "- 이메일: a@one.co.kr, b@two.co.kr")
	for _, id := range []string{"identity_reviewed:", "identity_reviewed:homonym:", "deadline_done:x"} {
		s.markIdentityReviewed(workfeed.Item{}, id)
	}
	page, _ := store.ReadPage("인물/김성환.md")
	if len(page.Meta.IdentityReviewed) != 0 {
		t.Errorf("잘못된 액션이 페이지를 건드림: %v", page.Meta.IdentityReviewed)
	}
}
