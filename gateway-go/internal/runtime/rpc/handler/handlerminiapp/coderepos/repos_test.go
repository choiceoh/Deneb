package coderepos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/coderepo"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "api")
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("make repo: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return resolved
}

// A gateway without the store must simply not advertise these methods, rather
// than register handlers that nil-panic on the first call.
func TestMethodsAreAbsentWithoutAStore(t *testing.T) {
	if got := Methods(CodeReposDeps{}); got != nil {
		t.Errorf("Methods(no store) = %d methods, want nil", len(got))
	}
}

func TestMethodsExposeTheAllowlistSurface(t *testing.T) {
	m := Methods(CodeReposDeps{Store: coderepo.New(t.TempDir(), nil)})
	for _, name := range []string{"miniapp.repos.list", "miniapp.repos.register", "miniapp.repos.unregister"} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing method %q", name)
		}
	}
}

// The store owns validation; what matters here is that its refusal reaches the
// caller as an error instead of being swallowed into a success.
func TestRegisterSurfacesStoreRefusal(t *testing.T) {
	prod := gitRepo(t)
	store := coderepo.New(t.TempDir(), []string{prod})
	m := Methods(CodeReposDeps{Store: store})

	rpctest.MustErr(t, rpctest.Call(m, "miniapp.repos.register", map[string]any{"path": prod}))
	rpctest.MustErr(t, rpctest.Call(m, "miniapp.repos.register", map[string]any{}))
}

func TestRegisterThenListRoundTrip(t *testing.T) {
	repo := gitRepo(t)
	m := Methods(CodeReposDeps{Store: coderepo.New(t.TempDir(), nil)})

	rpctest.MustOK(t, rpctest.Call(m, "miniapp.repos.register",
		map[string]any{"path": repo, "name": "내 레포"}))

	listed := rpctest.Result[struct {
		Repos []codeRepoOut `json:"repos"`
	}](t, rpctest.Call(m, "miniapp.repos.list", map[string]any{}))
	if len(listed.Repos) != 1 {
		t.Fatalf("repos = %+v, want one row", listed.Repos)
	}
	if listed.Repos[0].Name != "내 레포" || listed.Repos[0].Path != repo {
		t.Errorf("row = %+v, want the registered repo", listed.Repos[0])
	}
}

// Un-registering revokes eligibility; it must not read as a file deletion.
func TestUnregisterReportsThatNoFilesWereDeleted(t *testing.T) {
	repo := gitRepo(t)
	store := coderepo.New(t.TempDir(), nil)
	added, err := store.Register(repo, "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := Methods(CodeReposDeps{Store: store})

	got := rpctest.Result[struct {
		Unregistered bool `json:"unregistered"`
		DeletedFiles bool `json:"deletedFiles"`
	}](t, rpctest.Call(m, "miniapp.repos.unregister", map[string]any{"id": added.ID}))
	if !got.Unregistered || got.DeletedFiles {
		t.Errorf("got %+v, want unregistered without deleting files", got)
	}
	if _, err := os.Stat(repo); err != nil {
		t.Errorf("the working tree was removed: %v", err)
	}
}

func TestUnregisterRequiresAnID(t *testing.T) {
	m := Methods(CodeReposDeps{Store: coderepo.New(t.TempDir(), nil)})
	rpctest.MustErr(t, rpctest.Call(m, "miniapp.repos.unregister", map[string]any{}))
}
