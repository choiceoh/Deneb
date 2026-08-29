package coderepo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// gitRepo makes a directory that passes the registry's repository check.
func gitRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("make repo: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return resolved
}

func TestRegisterAddsAndListsRepo(t *testing.T) {
	state := t.TempDir()
	repo := gitRepo(t, "api")

	s := New(state, nil)
	got, err := s.Register(repo, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if got.Path != repo {
		t.Errorf("path = %q, want %q", got.Path, repo)
	}
	// No name given → the directory's own name, not a blank row.
	if got.Name != "api" {
		t.Errorf("name = %q, want %q", got.Name, "api")
	}
	if len(s.List()) != 1 {
		t.Errorf("list = %d entries, want 1", len(s.List()))
	}
}

// The allowlist is the safety boundary for worktree creation, so the production
// checkout — owned by the deploy timer, where CLAUDE.md forbids agent branches
// and worktrees — must be unregisterable, not merely discouraged.
func TestRegisterRefusesProtectedProductionCheckout(t *testing.T) {
	state := t.TempDir()
	prod := gitRepo(t, "deneb")

	s := New(state, []string{prod})
	if _, err := s.Register(prod, "프로덕션"); !errors.Is(err, ErrProtectedPath) {
		t.Fatalf("register(protected) error = %v, want ErrProtectedPath", err)
	}
	if len(s.List()) != 0 {
		t.Error("a protected path must not land in the registry")
	}
}

func TestRegisterRejectsNonRepoAndRelativePaths(t *testing.T) {
	state := t.TempDir()
	plain := t.TempDir() // a directory, but not a repository

	s := New(state, nil)
	if _, err := s.Register(plain, ""); err == nil {
		t.Error("a directory without .git must be refused")
	}
	if _, err := s.Register("relative/path", ""); err == nil {
		t.Error("a relative path must be refused")
	}
	if _, err := s.Register(filepath.Join(plain, "missing"), ""); err == nil {
		t.Error("a nonexistent path must be refused")
	}
}

// A git WORKTREE has a .git FILE rather than a directory. Rejecting those would
// make the feature useless for exactly the layout it exists to support.
func TestRegisterAcceptsWorktreeWhoseGitIsAFile(t *testing.T) {
	state := t.TempDir()
	wt := filepath.Join(t.TempDir(), "feature")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /elsewhere\n"), 0o600); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	if _, err := New(state, nil).Register(wt, ""); err != nil {
		t.Errorf("worktree register: %v", err)
	}
}

// Registering twice is the operator asking for the same thing again — return
// what they already have instead of erroring or creating a duplicate row.
func TestRegisterIsIdempotentForTheSamePath(t *testing.T) {
	state := t.TempDir()
	repo := gitRepo(t, "api")

	s := New(state, nil)
	first, err := s.Register(repo, "이름")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := s.Register(repo, "다른 이름")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID != second.ID || len(s.List()) != 1 {
		t.Errorf("re-register created a duplicate: %d entries", len(s.List()))
	}
}

// Two repos sharing a basename must not collide into one id.
func TestIDsStayDistinctForSameNamedRepos(t *testing.T) {
	state := t.TempDir()
	a, b := gitRepo(t, "api"), gitRepo(t, "api")

	s := New(state, nil)
	ra, err := s.Register(a, "")
	if err != nil {
		t.Fatalf("register a: %v", err)
	}
	rb, err := s.Register(b, "")
	if err != nil {
		t.Fatalf("register b: %v", err)
	}
	if ra.ID == rb.ID {
		t.Errorf("distinct repos share id %q", ra.ID)
	}
}

func TestUnregisterRemovesFromListAndSurvivesReopen(t *testing.T) {
	state := t.TempDir()
	repo := gitRepo(t, "api")

	s := New(state, nil)
	added, err := s.Register(repo, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := s.Unregister(added.ID); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if len(New(state, nil).List()) != 0 {
		t.Error("unregister did not persist")
	}
	// Un-registering is a permissions change, not a deletion — the directory
	// itself must still be there.
	if _, err := os.Stat(repo); err != nil {
		t.Errorf("unregister removed the directory: %v", err)
	}
}

func TestUnregisterUnknownIDReports(t *testing.T) {
	if err := New(t.TempDir(), nil).Unregister("nope"); err == nil {
		t.Error("unregistering an unknown id must report, not silently pass")
	}
}

func TestRegistryPersistsAcrossReopen(t *testing.T) {
	state := t.TempDir()
	repo := gitRepo(t, "api")

	if _, err := New(state, nil).Register(repo, "내 레포"); err != nil {
		t.Fatalf("register: %v", err)
	}
	reopened := New(state, nil).List()
	if len(reopened) != 1 || reopened[0].Name != "내 레포" {
		t.Fatalf("reopen lost the registry: %+v", reopened)
	}
}

// A corrupt registry must not wedge startup — the gateway boots with an empty
// allowlist (nothing eligible) rather than refusing to start.
func TestCorruptRegistryStartsEmptyInsteadOfFailing(t *testing.T) {
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "code-repos.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := len(New(state, nil).List()); got != 0 {
		t.Errorf("list = %d, want 0", got)
	}
}
