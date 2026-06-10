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

func TestSnapshotGit_ReturnsHashAndStat(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mustWrite(t, store, "프로젝트/hash.md", &Page{
		Meta: Frontmatter{ID: "hash", Title: "해시 테스트", Category: "프로젝트"},
		Body: "본문",
	})
	ctx := context.Background()
	hash := store.SnapshotGit(ctx, "with hash")
	if hash == "" {
		t.Fatal("commit must return a short hash")
	}
	if stat := store.GitSnapshotStat(ctx, hash); !strings.Contains(stat, "changed") {
		t.Errorf("diffstat missing summary line: %q", stat)
	}
	// Noop snapshot returns "".
	if got := store.SnapshotGit(ctx, "noop"); got != "" {
		t.Errorf("noop snapshot returned hash %q", got)
	}
}

func TestFormatWikiChangeSummary(t *testing.T) {
	out := formatWikiChangeSummary("abc1234", " a.md | 2 +-\n 3 files changed, 10 insertions(+)", "/home/u/.deneb/wiki",
		[]string{"프로젝트/a.md", "인물/b.md", "c.md", "d.md", "e.md", "f.md", "g.md"})
	for _, want := range []string{"abc1234", "3 files changed", "revert --no-edit abc1234", "외 1", "프로젝트/a.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q in %q", want, out)
		}
	}
}
