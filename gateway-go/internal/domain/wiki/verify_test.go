package wiki

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectStaleDeadlines(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	past := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	future := time.Now().AddDate(0, 0, 10).Format("2006-01-02")

	mustWrite(t, store, "deals/overdue.md", &Page{
		Meta: Frontmatter{ID: "overdue", Title: "지난 거래", Category: "거래", Due: past},
		Body: "결제 기한이 지남.",
	})
	mustWrite(t, store, "deals/upcoming.md", &Page{
		Meta: Frontmatter{ID: "upcoming", Title: "예정 거래", Category: "거래", Due: future},
		Body: "아직 기한 전.",
	})
	mustWrite(t, store, "deals/nodue.md", &Page{
		Meta: Frontmatter{ID: "nodue", Title: "기한 없는 거래", Category: "거래"},
		Body: "기한 미정.",
	})

	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	findings := wd.detectStaleDeadlines()

	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 stale finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Type != "stale_deadline" {
		t.Errorf("type = %q, want stale_deadline", f.Type)
	}
	if f.PageA != "deals/overdue.md" {
		t.Errorf("pageA = %q, want deals/overdue.md", f.PageA)
	}
}
