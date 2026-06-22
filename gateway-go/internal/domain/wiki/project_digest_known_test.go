package wiki

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestKnownProjectNames verifies the digest anchor: the distinct 프로젝트/<name>
// buckets, folding sub-pages and single-page projects, excluding non-project
// categories — so a digest can only ever name a real, navigable bucket.
func TestKnownProjectNames(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	for _, tc := range []struct {
		path, title, cat string
	}{
		{"프로젝트/영산고/계약.md", "계약", "프로젝트/영산고"}, // folder project, two pages
		{"프로젝트/영산고/일정.md", "일정", "프로젝트/영산고"}, // → folds to one "영산고"
		{"프로젝트/남도풍력.md", "남도풍력", "프로젝트"},     // single-page project
		{"인물/김민준.md", "김민준", "인물"},           // not a project → excluded
	} {
		p := NewPage(tc.title, tc.cat, nil)
		p.Body = "# " + tc.title
		if err := store.WritePage(tc.path, p); err != nil {
			t.Fatalf("WritePage(%s): %v", tc.path, err)
		}
	}

	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	got := wd.knownProjectNames()

	want := []string{"남도풍력", "영산고"} // sorted, deduped, 인물 excluded
	if len(got) != len(want) {
		t.Fatalf("knownProjectNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("knownProjectNames() = %v, want %v", got, want)
		}
	}
}
