package wiki

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitLogCount(t *testing.T, dir string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-list: %v (%s)", err, out)
	}
	n := strings.TrimSpace(string(out))
	if n == "" {
		return 0
	}
	c := 0
	for _, ch := range n {
		c = c*10 + int(ch-'0')
	}
	return c
}

func TestSnapshotGit_CommitsOnlyOnChange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mustWrite(t, store, "프로젝트/snap.md", &Page{
		Meta: Frontmatter{ID: "snap", Title: "스냅샷 테스트", Category: "프로젝트"},
		Body: "버전 1",
	})

	ctx := context.Background()
	store.SnapshotGit(ctx, "first snapshot")
	if _, err := os.Stat(filepath.Join(store.Dir(), ".git")); err != nil {
		t.Fatalf("repo not initialized: %v", err)
	}
	if got := gitLogCount(t, store.Dir()); got != 1 {
		t.Fatalf("want 1 commit, got %d", got)
	}

	// No changes → no new commit.
	store.SnapshotGit(ctx, "noop snapshot")
	if got := gitLogCount(t, store.Dir()); got != 1 {
		t.Errorf("noop snapshot must not commit; got %d commits", got)
	}

	// Derived state files are ignored, page edits are committed.
	if err := os.WriteFile(filepath.Join(store.Dir(), ".semantic-cache.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	store.SnapshotGit(ctx, "cache only")
	if got := gitLogCount(t, store.Dir()); got != 1 {
		t.Errorf("ignored files must not trigger commits; got %d", got)
	}
	mustWrite(t, store, "프로젝트/snap.md", &Page{
		Meta: Frontmatter{ID: "snap", Title: "스냅샷 테스트", Category: "프로젝트"},
		Body: "버전 2",
	})
	store.SnapshotGit(ctx, "page edit")
	if got := gitLogCount(t, store.Dir()); got != 2 {
		t.Errorf("page edit must commit; got %d commits", got)
	}
}
