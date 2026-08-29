package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/coderepo"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

func registeredRepo(t *testing.T) (*coderepo.Store, coderepo.Repo) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "api")
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("make repo: %v", err)
	}
	store := coderepo.New(t.TempDir(), nil)
	repo, err := store.Register(dir, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return store, repo
}

func TestResolveSessionWorkspaceReturnsBoundRepoPath(t *testing.T) {
	store, repo := registeredRepo(t)
	bindings := map[string]string{"client:cygnus:a": repo.ID}

	if got := resolveSessionWorkspace(store, bindings, "client:cygnus:a"); got != repo.Path {
		t.Errorf("workspace = %q, want %q", got, repo.Path)
	}
}

// The allowlist is the authority at every run, not at binding time. A repo the
// operator un-registered must stop being a directory the agent runs in — the
// session falls back to the default rather than keeping revoked access.
func TestResolveSessionWorkspaceFallsBackWhenRepoUnregistered(t *testing.T) {
	store, repo := registeredRepo(t)
	bindings := map[string]string{"client:cygnus:a": repo.ID}

	if err := store.Unregister(repo.ID); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if got := resolveSessionWorkspace(store, bindings, "client:cygnus:a"); got != "" {
		t.Errorf("workspace = %q, want empty (binding revoked)", got)
	}
}

func TestResolveSessionWorkspaceEmptyForUnboundOrMissingInputs(t *testing.T) {
	store, repo := registeredRepo(t)
	bindings := map[string]string{"client:cygnus:a": repo.ID}

	if got := resolveSessionWorkspace(store, bindings, "client:main"); got != "" {
		t.Errorf("unbound session workspace = %q, want empty", got)
	}
	if got := resolveSessionWorkspace(store, bindings, ""); got != "" {
		t.Errorf("empty key workspace = %q, want empty", got)
	}
	if got := resolveSessionWorkspace(nil, bindings, "client:cygnus:a"); got != "" {
		t.Errorf("no store workspace = %q, want empty", got)
	}
}

func TestSessionReposRoundTripThroughSidecar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-repos.json")
	want := map[string]string{"client:cygnus:a": "api-1234", "client:main": "web-5678"}

	if err := saveSessionRepos(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadSessionRepos(path)
	if len(got) != len(want) {
		t.Fatalf("loaded %d bindings, want %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("binding[%q] = %q, want %q", k, got[k], v)
		}
	}
}

// A missing or corrupt sidecar must degrade to "no bindings" — every
// conversation then uses the default workspace, which is where they all worked
// before this existed. Refusing to start would be worse than forgetting.
func TestLoadSessionReposDegradesToEmpty(t *testing.T) {
	dir := t.TempDir()
	if got := loadSessionRepos(filepath.Join(dir, "absent.json")); len(got) != 0 {
		t.Errorf("missing file = %v, want empty", got)
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := loadSessionRepos(corrupt); len(got) != 0 {
		t.Errorf("corrupt file = %v, want empty", got)
	}
}

// Bindings for deleted conversations must not accumulate in the sidecar.
func TestSnapshotSessionReposDropsDeadSessions(t *testing.T) {
	bindings := map[string]string{"alive": "api-1", "gone": "api-2"}
	sessions := []*session.Session{{Key: "alive"}, nil}

	got := snapshotSessionRepos(bindings, sessions)
	if _, ok := got["gone"]; ok {
		t.Error("a deleted session's binding survived the sweep")
	}
	if got["alive"] != "api-1" {
		t.Errorf("live binding = %q, want api-1", got["alive"])
	}
}

// The binding must reach a RUN, not just resolve in isolation: a conversation
// pointed at a repo works there, and an unbound one stays on the default.
func TestServerSessionWorkspaceDirReflectsBinding(t *testing.T) {
	store, repo := registeredRepo(t)
	s := &Server{ServerRuntime: &ServerRuntime{codeRepos: store, sessionRepos: map[string]string{}}}

	if got := s.SessionWorkspaceDir("client:cygnus:a"); got != "" {
		t.Errorf("unbound session = %q, want the default (empty)", got)
	}

	s.sessionRepos["client:cygnus:a"] = repo.ID
	if got := s.SessionWorkspaceDir("client:cygnus:a"); got != repo.Path {
		t.Errorf("bound session = %q, want %q", got, repo.Path)
	}
}

// Binding to something the operator never registered must be refused — the
// allowlist would be pointless if a bind call could name any path.
func TestBindSessionRepoRejectsUnregisteredRepo(t *testing.T) {
	store, _ := registeredRepo(t)
	s := &Server{ServerRuntime: &ServerRuntime{codeRepos: store, sessionRepos: map[string]string{}}}

	if err := s.BindSessionRepo("client:cygnus:a", "not-registered"); err == nil {
		t.Error("binding an unregistered repo must fail")
	}
	if err := s.BindSessionRepo("", "whatever"); err == nil {
		t.Error("an empty session key must fail")
	}
}

// Only Cygnus conversations are cleaned up automatically (operator decision):
// a 업무 conversation's tree is not something a delete sweep should decide about.
func TestReleaseSessionWorktreeOnlyTouchesCygnusSessions(t *testing.T) {
	store, repo := registeredRepo(t)
	s := &Server{ServerRuntime: &ServerRuntime{
		codeRepos:    store,
		sessionRepos: map[string]string{"client:main:work": repo.ID},
	}}
	s.logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	// A non-Cygnus session must be left entirely alone — no lookup, no removal.
	s.releaseSessionWorktree("client:main:work")
	if got := s.BoundSessionRepo("client:main:work"); got != repo.ID {
		t.Errorf("binding = %q, want it untouched", got)
	}
}

// Forgetting a conversation must clear the binding in BOTH places: the sidecar
// and the in-memory map the run path reads. Clearing only the file would leave
// a deleted conversation resolving to a workspace until the next restart.
func TestDropStoredSessionRepoClearsMemoryToo(t *testing.T) {
	store, repo := registeredRepo(t)
	s := &Server{ServerRuntime: &ServerRuntime{
		codeRepos:    store,
		sessionRepos: map[string]string{"client:cygnus:a": repo.ID},
	}}

	s.dropStoredSessionRepo("client:cygnus:a")
	if got := s.BoundSessionRepo("client:cygnus:a"); got != "" {
		t.Errorf("binding = %q, want cleared in memory", got)
	}
	if got := s.SessionWorkspaceDir("client:cygnus:a"); got != "" {
		t.Errorf("workspace = %q, want the default after forgetting", got)
	}
}
