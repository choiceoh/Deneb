package wiki

import (
	"os"
	"path/filepath"
	"testing"
)

// ListPages must prune backup/hidden directories (operator-made copies, the
// wiki .git repo) so they never surface as phantom 인물/프로젝트 rows, while
// keeping legit project subfolders (기자재/메일분석/…).
func TestListPages_PrunesBackupAndHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	wikiDir := store.dir

	write := func(rel string) {
		p := filepath.Join(wikiDir, rel)
		if mkErr := os.MkdirAll(filepath.Dir(p), 0o755); mkErr != nil {
			t.Fatalf("mkdir %s: %v", rel, mkErr)
		}
		if wErr := os.WriteFile(p, []byte("# page\n"), 0o644); wErr != nil {
			t.Fatalf("write %s: %v", rel, wErr)
		}
	}

	// Live pages (must appear).
	write(filepath.Join("인물", "김민준.md"))
	write(filepath.Join("프로젝트", "군산태양광", "대표.md"))
	write(filepath.Join("프로젝트", "군산태양광", "메일분석", "m1.md")) // legit subfolder
	// Backup / hidden copies (must NOT appear).
	write(filepath.Join("인물", "backup", "김민준.md"))
	write(filepath.Join("백업", "박지훈.md"))
	write(filepath.Join("프로젝트", "군산태양광-backup", "대표.md"))
	write(filepath.Join(".git", "objects", "note.md"))

	pages, err := store.ListPages("")
	if err != nil {
		t.Fatalf("list pages: %v", err)
	}
	got := map[string]bool{}
	for _, p := range pages {
		got[filepath.ToSlash(p)] = true
	}
	for _, want := range []string{"인물/김민준.md", "프로젝트/군산태양광/대표.md", "프로젝트/군산태양광/메일분석/m1.md"} {
		if !got[want] {
			t.Errorf("live page missing from listing: %s (got %v)", want, pages)
		}
	}
	for _, bad := range []string{"인물/backup/김민준.md", "백업/박지훈.md", "프로젝트/군산태양광-backup/대표.md", ".git/objects/note.md"} {
		if got[bad] {
			t.Errorf("backup/hidden page leaked into listing: %s", bad)
		}
	}

	// Category-scoped listing prunes the same way.
	people, err := store.ListPages("인물")
	if err != nil {
		t.Fatalf("list 인물: %v", err)
	}
	for _, p := range people {
		if filepath.ToSlash(p) == "인물/backup/김민준.md" {
			t.Errorf("category listing leaked a backup page")
		}
	}
}
