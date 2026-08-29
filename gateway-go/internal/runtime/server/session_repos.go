package server

// Session → code repository binding.
//
// A conversation can be pointed at one registered repository; its runs then work
// there instead of the server-wide workspace. The binding lives in a sidecar
// (~/.deneb/session-repos.json) for the same reason labels and pins do: the
// gateway hot-swaps every few minutes, and a binding that vanished on restart
// would silently move the agent back to the default workspace mid-task.
//
// Bindings store the repository ID, never its path. An id resolves through the
// registry, so un-registering a repo makes every session bound to it fall back
// to the default rather than keep working somewhere the operator revoked.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/coderepo"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

func sessionReposStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".deneb", "session-repos.json"), nil
}

// loadSessionRepos reads sessionKey → repoID. Shares the label store's JSON
// codec (both are string maps keyed by session) rather than carrying a second
// copy of it. A missing or corrupt file degrades to no bindings, which means
// every conversation uses the default workspace — where they all worked before
// this feature existed.
func loadSessionRepos(path string) map[string]string {
	return loadSessionLabels(path)
}

// saveSessionRepos writes the bindings atomically, key-sorted for a stable diff.
func saveSessionRepos(path string, bindings map[string]string) error {
	return saveSessionLabels(path, bindings)
}

// resolveSessionWorkspace returns the directory a session's runs should work in,
// or "" to leave the run on the server-wide default.
//
// Returning "" for an unresolvable binding is deliberate: a repo the operator
// un-registered, or a stale entry from an older state dir, must not become a
// path the agent still runs commands in. The allowlist is the authority every
// time, not the moment the binding was made.
func resolveSessionWorkspace(store *coderepo.Store, bindings map[string]string, sessionKey string) string {
	if store == nil || sessionKey == "" {
		return ""
	}
	id := bindings[sessionKey]
	if id == "" {
		return ""
	}
	repo, ok := store.Lookup(id)
	if !ok {
		return ""
	}
	return repo.Path
}

// snapshotSessionRepos keeps bindings for sessions that still exist, dropping
// the rest so the sidecar does not accumulate entries for deleted conversations.
func snapshotSessionRepos(bindings map[string]string, sessions []*session.Session) map[string]string {
	live := make(map[string]struct{}, len(sessions))
	for _, s := range sessions {
		if s != nil {
			live[s.Key] = struct{}{}
		}
	}
	out := make(map[string]string, len(bindings))
	for key, id := range bindings {
		if _, ok := live[key]; ok {
			out[key] = id
		}
	}
	return out
}

// initCodeRepos builds the single allowlist store and loads the session
// bindings. Called once during startup wiring; both the RPC surface and the run
// path read through these so a registration is visible everywhere at once.
func (s *Server) initCodeRepos() {
	s.codeRepos = coderepo.New(config.ResolveStateDir(), protectedRepoRoots())
	bindings := map[string]string{}
	if path, err := sessionReposStorePath(); err == nil {
		bindings = loadSessionRepos(path)
	}
	s.sessionReposMu.Lock()
	s.sessionRepos = bindings
	s.sessionReposMu.Unlock()
}

// SessionWorkspaceDir returns the directory a session's runs should work in, or
// "" for the server-wide default.
func (s *Server) SessionWorkspaceDir(sessionKey string) string {
	s.sessionReposMu.Lock()
	bindings := make(map[string]string, len(s.sessionRepos))
	for k, v := range s.sessionRepos {
		bindings[k] = v
	}
	s.sessionReposMu.Unlock()
	return resolveSessionWorkspace(s.codeRepos, bindings, sessionKey)
}

// BindSessionRepo points a conversation at a registered repository, or clears
// the binding when repoID is empty. Persisted immediately rather than on the
// periodic sweep: a hot-swap seconds later must not lose where the operator
// just said this conversation works.
func (s *Server) BindSessionRepo(sessionKey, repoID string) error {
	if strings.TrimSpace(sessionKey) == "" {
		return errors.New("sessionKey가 비어 있습니다")
	}
	if repoID != "" && s.codeRepos != nil {
		if _, ok := s.codeRepos.Lookup(repoID); !ok {
			return fmt.Errorf("등록되지 않은 저장소입니다: %s", repoID)
		}
	}

	s.sessionReposMu.Lock()
	if s.sessionRepos == nil {
		s.sessionRepos = map[string]string{}
	}
	if repoID == "" {
		delete(s.sessionRepos, sessionKey)
	} else {
		s.sessionRepos[sessionKey] = repoID
	}
	snapshot := make(map[string]string, len(s.sessionRepos))
	for k, v := range s.sessionRepos {
		snapshot[k] = v
	}
	s.sessionReposMu.Unlock()

	path, err := sessionReposStorePath()
	if err != nil {
		return err
	}
	return saveSessionRepos(path, snapshot)
}

// BoundSessionRepo reports the repository id a conversation is bound to, or ""
// for the server-wide default.
func (s *Server) BoundSessionRepo(sessionKey string) string {
	s.sessionReposMu.Lock()
	defer s.sessionReposMu.Unlock()
	return s.sessionRepos[sessionKey]
}
