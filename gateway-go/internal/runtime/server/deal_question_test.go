package server

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
)

func discardServer(t *testing.T) (*Server, *domainbind.WikiStore) {
	t.Helper()
	dir := t.TempDir()
	ws, err := domainbind.NewWikiStore(dir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("wiki store: %v", err)
	}
	return &Server{
		MemorySubsystem: &MemorySubsystem{wikiStore: ws},
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, ws
}

// A team answer is recorded onto the deal page — the visible "불확실 → 질문 → 기록"
// close. "딜 아님" records nothing.
func TestRecordDealQuestionAnswerUpdatesPagePreservingBody(t *testing.T) {
	s, ws := discardServer(t)
	const path = "프로젝트/완도.md"
	if err := ws.WritePage(path, &domainbind.Page{Meta: domainbind.Frontmatter{Title: "완도군청"}, Body: "기존 본문"}); err != nil {
		t.Fatalf("seed page: %v", err)
	}

	s.recordDealQuestionAnswer(domainbind.Item{RefType: "wiki", RefID: path}, "dept:pl1")

	page, err := ws.ReadPage(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(page.Body, "1팀") {
		t.Errorf("team not recorded in body: %q", page.Body)
	}
	if !strings.Contains(page.Body, "기존 본문") {
		t.Errorf("original body lost: %q", page.Body)
	}

	// "딜 아님" (none) must not modify the page.
	before := page.Body
	s.recordDealQuestionAnswer(domainbind.Item{RefType: "wiki", RefID: path}, "dept:none")
	after, _ := ws.ReadPage(path)
	if after.Body != before {
		t.Errorf("none answer must not modify page:\nbefore %q\nafter  %q", before, after.Body)
	}
}

// A missing/empty RefID or wiki page is a no-op, not a panic.
func TestRecordDealQuestionAnswerIgnoresMissingTarget(t *testing.T) {
	s, _ := discardServer(t)
	s.recordDealQuestionAnswer(domainbind.Item{RefID: ""}, "dept:pl1")           // empty path
	s.recordDealQuestionAnswer(domainbind.Item{RefID: "프로젝트/없음.md"}, "dept:pl2") // missing page
}
