package wiki

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestGraphContext_Live runs the traversal against the real on-disk wiki so we
// can eyeball that seed resolution and edges surface real project relationships.
// Skipped in CI (no wiki there). Read-only: a temp diary dir keeps it from
// touching prod diary state.
//
//	DENEB_WIKI_GRAPH_LIVE=1 DENEB_WIKI_GRAPH_Q=비금도 \
//	  go test -run TestGraphContext_Live -v ./internal/domain/wiki/
func TestGraphContext_Live(t *testing.T) {
	if os.Getenv("DENEB_WIKI_GRAPH_LIVE") == "" {
		t.Skip("set DENEB_WIKI_GRAPH_LIVE=1 to run against ~/.deneb/wiki")
	}
	home, _ := os.UserHomeDir()
	store, err := NewStore(filepath.Join(home, ".deneb", "wiki"), t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	q := os.Getenv("DENEB_WIKI_GRAPH_Q")
	if q == "" {
		q = "대한전선"
	}
	res, err := store.GraphContext(context.Background(), q, 15)
	if err != nil {
		t.Fatalf("GraphContext: %v", err)
	}
	t.Logf("query=%q → seed=%q found=%v neighbors=%d", q, res.SeedPath, res.Found, len(res.Neighbors))
	for _, n := range res.Neighbors {
		t.Logf("  %.1f  %s  [%s]  (%s)", n.Score, n.Title, strings.Join(n.Reasons, " · "), n.Path)
	}
}

func TestGraphContext(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))

	// 서림철강 first so the backlink from 대한전선 lands on an existing page.
	seorim := NewPage("서림철강 순천공장 풍력", "프로젝트", []string{"풍력"})
	seorim.Meta.Importance = 0.7
	mustWritePage(t, store, "프로젝트/서림철강.md", seorim)

	// 대한전선 → explicit Related[] edge to 서림철강.
	daehan := NewPage("대한전선 당진 2차", "프로젝트", []string{"태양광"})
	daehan.Meta.Related = []string{"프로젝트/서림철강.md"}
	daehan.Meta.Importance = 0.9
	mustWritePage(t, store, "프로젝트/대한전선.md", daehan)

	// A mail-analysis page that points at 대한전선 — an incoming edge.
	mail := NewPage("6/3 대한전선 구조검토 메일", "mail-analysis", nil)
	mail.Meta.Related = []string{"프로젝트/대한전선.md"}
	mustWritePage(t, store, "mail-analysis/m1.md", mail)

	// Seed resolves by title substring ("대한전선" → 대한전선 당진 2차).
	res, err := store.GraphContext(context.Background(), "대한전선", 10)
	if err != nil {
		t.Fatalf("GraphContext: %v", err)
	}
	if !res.Found {
		t.Fatal("seed not found by title substring")
	}
	if res.SeedPath != "프로젝트/대한전선.md" {
		t.Errorf("seedPath = %q, want 프로젝트/대한전선.md", res.SeedPath)
	}

	got := map[string]bool{}
	for _, n := range res.Neighbors {
		got[n.Path] = true
	}
	if !got["프로젝트/서림철강.md"] {
		t.Errorf("missing related neighbor 서림철강; neighbors=%v", res.Neighbors)
	}
	if !got["mail-analysis/m1.md"] {
		t.Errorf("missing connected neighbor m1; neighbors=%v", res.Neighbors)
	}

	// Unknown entity resolves to nothing (no false seed).
	none, err := store.GraphContext(context.Background(), "존재하지않는엔티티-xyz", 10)
	if err != nil {
		t.Fatalf("GraphContext(unknown): %v", err)
	}
	if none.Found {
		t.Errorf("unknown query should not resolve a seed, got %+v", none)
	}

	// Limit is respected.
	capped, _ := store.GraphContext(context.Background(), "대한전선", 1)
	if len(capped.Neighbors) > 1 {
		t.Errorf("limit=1 returned %d neighbors", len(capped.Neighbors))
	}
}

func mustWritePage(t *testing.T, store *Store, path string, page *Page) {
	t.Helper()
	if err := store.WritePage(path, page); err != nil {
		t.Fatalf("WritePage %s: %v", path, err)
	}
}
