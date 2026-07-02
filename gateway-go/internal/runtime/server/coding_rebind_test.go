package server

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/code"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// TestRebindCodingSession pins the lazy worktree rebind: the in-memory session
// binding is lost on session GC (1h for terminal direct sessions) and on every
// restart, so a code: turn must be able to re-derive it from the durable code
// store — otherwise the turn silently runs unscoped (full 업무 toolset, default
// workspace) and skips the turn-end checkpoint/verify.
func TestRebindCodingSession(t *testing.T) {
	seedStore := func(t *testing.T, denebDir string, sessions ...*code.Session) {
		t.Helper()
		store, err := code.NewStore(filepath.Join(denebDir, "code"))
		if err != nil {
			t.Fatal(err)
		}
		for _, cs := range sessions {
			if err := store.Add(cs); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("rebinds from the durable code store", func(t *testing.T) {
		dir := t.TempDir()
		seedStore(t, dir, &code.Session{
			ID:     "task-1",
			Repo:   code.Repo{Owner: "acme", Name: "api"},
			Branch: "deneb/task-1",
			Dir:    "/data/code/acme/api/wt/task-1",
		})
		s := &Server{
			denebDir:       dir,
			logger:         slog.Default(),
			SessionManager: &SessionManager{sessions: session.NewManager()},
		}

		s.rebindCodingSession("code:task-1")

		sess := s.sessions.Get("code:task-1")
		if sess == nil {
			t.Fatal("session should be created by the rebind")
		}
		if sess.Mode != session.ModeCode || sess.ToolPreset != "coding" {
			t.Errorf("mode/preset = %q/%q, want code/coding", sess.Mode, sess.ToolPreset)
		}
		if sess.WorkspaceDir != "/data/code/acme/api/wt/task-1" {
			t.Errorf("workspaceDir = %q", sess.WorkspaceDir)
		}
	})

	t.Run("missing worktree and unknown task leave the session untouched", func(t *testing.T) {
		dir := t.TempDir()
		gone := &code.Session{
			ID:     "task-gone",
			Repo:   code.Repo{Owner: "acme", Name: "api"},
			Branch: "deneb/task-gone",
			Dir:    "/data/code/acme/api/wt/task-gone",
			Status: code.StatusMissing,
		}
		seedStore(t, dir, gone)
		s := &Server{
			denebDir:       dir,
			logger:         slog.Default(),
			SessionManager: &SessionManager{sessions: session.NewManager()},
		}

		s.rebindCodingSession("code:task-gone") // reconciled missing → skip
		s.rebindCodingSession("code:no-such")   // no store record → skip
		s.rebindCodingSession("client:main")    // not a coding key → skip

		for _, key := range []string{"code:task-gone", "code:no-such", "client:main"} {
			if s.sessions.Get(key) != nil {
				t.Errorf("rebind must not create a session for %q", key)
			}
		}
	})
}

func TestSanitizeCheckpointLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"로그인 폼 검증 추가", "로그인 폼 검증 추가"},
		{"\"헬스체크 엔드포인트 신설.\"\n부연 설명", "헬스체크 엔드포인트 신설"},
		{"- 버그 수정", "버그 수정"},
		{"`config.go` 상수 정리", "config.go` 상수 정리"},
		{"   \n\n", ""},
		{strings.Repeat("가", 80), strings.Repeat("가", 60) + "…"},
	}
	for _, c := range cases {
		if got := sanitizeCheckpointLabel(c.in); got != c.want {
			t.Errorf("sanitizeCheckpointLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
