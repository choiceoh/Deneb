package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
)

func TestIdleReviewDue_Pacing(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	staleAfter := 6 * time.Hour
	retryGap := 2 * time.Hour
	ms := func(t time.Time) int64 { return t.UnixMilli() }

	cases := []struct {
		name         string
		lastReviewMs int64
		staleAfter   time.Duration
		lastAttempt  time.Time
		want         bool
	}{
		{"disabled", ms(now.Add(-24 * time.Hour)), 0, time.Time{}, false},
		{"fresh review holds the lane", ms(now.Add(-1 * time.Hour)), staleAfter, time.Time{}, false},
		{"stale review fires", ms(now.Add(-7 * time.Hour)), staleAfter, time.Time{}, true},
		{"never reviewed fires", 0, staleAfter, time.Time{}, true},
		{"recent attempt blocks retry", ms(now.Add(-7 * time.Hour)), staleAfter, now.Add(-30 * time.Minute), false},
		{"old attempt allows retry", ms(now.Add(-7 * time.Hour)), staleAfter, now.Add(-3 * time.Hour), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := idleReviewDue(now, tc.lastReviewMs, tc.staleAfter, tc.lastAttempt, retryGap); got != tc.want {
				t.Fatalf("idleReviewDue = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIdleReviewableSessionKey(t *testing.T) {
	cases := map[string]bool{
		"client:main":                      true,
		"client:main:abc123":               true,
		"client:main:dream":                false, // dream loop, not a user surface
		"client:puppet-worktree":           false, // puppet-seat test session
		"cron:morning-letter:123":          false, // cron never nudges — same here
		"system:skill-review:client:main":  false, // reviewing a review fork
		"lt-verify-card-3":                 false, // live-test scratch session
		"telegram:7074071666:propus-x:123": false, // retired channel prefix
	}
	for key, want := range cases {
		if got := idleReviewableSessionKey(key); got != want {
			t.Errorf("idleReviewableSessionKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestRecentRealSessionKeys_FilterAndOrder(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		mod := time.Now().Add(-age)
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	write("client:main.jsonl", 3*time.Hour)
	write("client:main:abc.jsonl", 1*time.Hour)   // newest reviewable
	write("client:main:dream.jsonl", time.Minute) // excluded despite recency
	write("cron:morning-letter:1.jsonl", time.Minute)
	write("notes.txt", time.Minute) // non-transcript file

	got := recentRealSessionKeys(dir, 2)
	want := []string{"client:main:abc", "client:main"}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys[%d] = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}

	if keys := recentRealSessionKeys(filepath.Join(dir, "missing"), 3); keys != nil {
		t.Fatalf("missing dir should yield nil, got %v", keys)
	}
}

func TestIdleSkillReviewLane_QuietWithoutGenesis(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate from the host state (package convention)
	srv := &Server{ServerRuntime: &ServerRuntime{}, GenesisSubsystem: &GenesisSubsystem{}}
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	lane := srv.newIdleSkillReviewLane()
	if fired, detail := lane(t.Context()); fired || detail != "" {
		t.Fatalf("lane without genesis deps fired: %v %q", fired, detail)
	}
}

func TestIdleReviewStaleAfterFromEnv(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Setenv("DENEB_SKILL_IDLE_REVIEW_AFTER", "")
	if d := idleReviewStaleAfterFromEnv(logger); d != defaultIdleReviewStaleAfter {
		t.Fatalf("default = %v", d)
	}
	t.Setenv("DENEB_SKILL_IDLE_REVIEW_AFTER", "0")
	if d := idleReviewStaleAfterFromEnv(logger); d != 0 {
		t.Fatalf("disabled = %v", d)
	}
	t.Setenv("DENEB_SKILL_IDLE_REVIEW_AFTER", "12h")
	if d := idleReviewStaleAfterFromEnv(logger); d != 12*time.Hour {
		t.Fatalf("12h = %v", d)
	}
	t.Setenv("DENEB_SKILL_IDLE_REVIEW_AFTER", "banana")
	if d := idleReviewStaleAfterFromEnv(logger); d != defaultIdleReviewStaleAfter {
		t.Fatalf("invalid should fall back to default, got %v", d)
	}
}

// TestHeartbeatRun_ReachesIdleReviewLane proves the Run() ordering contract:
// the idle-review lane executes with the deterministic lanes, BEFORE the
// "nothing to do" early-return — so an otherwise empty tick still feeds the
// review loop. chatHandler only needs to be non-nil here: with no
// HEARTBEAT.md, no signals, and no nudges the tick returns before any turn.
func TestHeartbeatRun_ReachesIdleReviewLane(t *testing.T) {
	if !withinActiveHours(time.Now()) {
		t.Skip("outside heartbeat active hours (KST) — Run() no-ops by design")
	}
	called := false
	task := &heartbeatTask{
		chatHandler: &chat.Handler{},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		homeDir:     t.TempDir(),
		idleSkillReview: func(context.Context) (bool, string) {
			called = true
			return false, ""
		},
	}
	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatal("idleSkillReview lane was not reached by Run()")
	}
}
