package server

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	runtimeheartbeat "github.com/choiceoh/deneb/gateway-go/internal/runtime/heartbeat"
)

func TestIdleReviewDueReturnsTrueWhenStaleOrNeverReviewed(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	staleAfter := 6 * time.Hour
	retryGap := 2 * time.Hour
	ms := func(t time.Time) int64 { return t.UnixMilli() }

	cases := []struct {
		name         string
		lastReviewMs int64
		lastOK       bool
		staleAfter   time.Duration
		lastAttempt  time.Time
		want         bool
	}{
		{"disabled", ms(now.Add(-24 * time.Hour)), true, 0, time.Time{}, false},
		{"fresh review holds the lane", ms(now.Add(-1 * time.Hour)), true, staleAfter, time.Time{}, false},
		{"stale review fires", ms(now.Add(-7 * time.Hour)), true, staleAfter, time.Time{}, true},
		{"never reviewed fires", 0, true, staleAfter, time.Time{}, true},
		{"recent attempt blocks retry", ms(now.Add(-7 * time.Hour)), true, staleAfter, now.Add(-30 * time.Minute), false},
		{"old attempt allows retry", ms(now.Add(-7 * time.Hour)), true, staleAfter, now.Add(-3 * time.Hour), true},
		// A failed review re-arms after the retry gap, not the full window —
		// a transient model outage must not suppress the backstop for 6h.
		{"failed review retries after gap", ms(now.Add(-3 * time.Hour)), false, staleAfter, time.Time{}, true},
		{"failed review still paced within gap", ms(now.Add(-1 * time.Hour)), false, staleAfter, time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := idleReviewDue(now, tc.lastReviewMs, tc.lastOK, tc.staleAfter, tc.lastAttempt, retryGap); got != tc.want {
				t.Fatalf("idleReviewDue = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIdleReviewableSessionKeyAcceptsUserAndCronWorkSessions(t *testing.T) {
	cases := map[string]bool{
		"client:main":                      true,
		"client:main:abc123":               true,
		"client:main:dream":                false, // dream loop, not a user surface
		"client:puppet-worktree":           false, // puppet-seat test session
		"cron:email-analysis-full:123":     true,  // work lane: never nudges, so this is its only path in
		"cron:morning-letter:123":          true,
		"cron:weekly-ref-audit:123":        true,
		"system:skill-review:client:main":  false, // reviewing a review fork
		"system:mailpoll":                  false, // keeps no transcript in this dir
		"submain:heartbeat":                false,
		"client:lt-3902894-6":              false, // live-test harness (mock_native_client.py)
		"lt-verify-card-3":                 false, // live-test scratch session
		"livetest:think-ko":                false,
		"telegram:7074071666:propus-x:123": false, // retired channel prefix
	}
	for key, want := range cases {
		if got := idleReviewableSessionKey(key); got != want {
			t.Errorf("idleReviewableSessionKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestRecentRealSessionKeysReturnsNewestFilteredKeys(t *testing.T) {
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
	write("client:main:abc.jsonl", 2*time.Hour)   // newest reviewable client session
	write("client:main:dream.jsonl", time.Minute) // excluded despite recency
	write("client:lt-4242-1.jsonl", time.Minute)  // live-test harness, excluded despite recency
	write("cron:morning-letter:1.jsonl", 1*time.Hour)
	write("notes.txt", time.Minute) // non-transcript file

	got, err := recentRealSessionKeys(dir, 2)
	if err != nil {
		t.Fatalf("recentRealSessionKeys: %v", err)
	}
	want := []string{"cron:morning-letter:1", "client:main:abc"}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys[%d] = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}

	keys, err := recentRealSessionKeys(filepath.Join(dir, "missing"), 3)
	if keys != nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing dir should surface fs.ErrNotExist with no keys, got (%v, %v)", keys, err)
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

func TestIdleReviewStaleAfterFromEnvParsesOrFallsBackToDefault(t *testing.T) {
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
// review loop. ChatHandler only needs to report ChatReady: with no
// HEARTBEAT.md, no signals, and no nudges the tick returns before any turn.
func TestHeartbeatRunReachesIdleReviewLaneBeforeEarlyReturn(t *testing.T) {
	called := false
	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatalf("tz: %v", err)
	}
	task := runtimeheartbeat.NewTask(runtimeheartbeat.TaskConfig{
		Now:         func() time.Time { return time.Date(2026, 7, 11, 12, 0, 0, 0, kst) },
		ChatHandler: readyChatStub{},
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		HomeDir:     t.TempDir(),
		IdleSkillReview: func(context.Context) (bool, string) {
			called = true
			return false, ""
		},
	})
	if err := task.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatal("idleSkillReview lane was not reached by Run()")
	}
}

// readyChatStub admits heartbeat ticks without constructing a full chat.Handler
// (zero Handler fails ChatReady because abort is nil).
type readyChatStub struct{}

func (readyChatStub) ChatReady() bool { return true }

func (readyChatStub) RunSync(context.Context, chatport.SyncRequest) (*chatport.SyncResult, error) {
	return &chatport.SyncResult{}, nil
}

// TestIdleSkillReviewLane_ProductionGate proves the wiring-level invariant: a
// non-production state dir (dev/live-test) gets a nil lane — it must never
// run live reviews into production Propus state.
func TestIdleSkillReviewLaneReturnsNilForNonProductionStateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	srv := &Server{ServerRuntime: &ServerRuntime{}, GenesisSubsystem: &GenesisSubsystem{}}
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Setenv("DENEB_STATE_DIR", t.TempDir()) // dev-style redirect
	if lane := srv.idleSkillReviewLaneIfProduction(home); lane != nil {
		t.Fatal("non-production state dir must disable the lane")
	}

	t.Setenv("DENEB_STATE_DIR", "")
	if lane := srv.idleSkillReviewLaneIfProduction(home); lane == nil {
		t.Fatal("production state dir must enable the lane")
	}
}
