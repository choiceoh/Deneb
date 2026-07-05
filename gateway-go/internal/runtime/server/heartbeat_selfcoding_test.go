package server

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func selfCodingTask(t *testing.T, count int, fingerprint string) *heartbeatTask {
	t.Helper()
	return &heartbeatTask{
		homeDir: t.TempDir(),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		proposedSelfCoding: func() (int, string) {
			return count, fingerprint
		},
	}
}

// Pending proposed candidates fire the review nudge; an unchanged pending set
// stays quiet within the retry window (a failing turn must not re-pay a cloud
// turn every 30 minutes) but re-fires past it; a CHANGED set fires immediately.
func TestDetectSelfCodingNudge_FireThrottleRefire(t *testing.T) {
	task := selfCodingTask(t, 2, "2:sc-a:100")
	now := time.Now()

	nudge := task.detectSelfCodingNudge(now)
	if nudge == "" {
		t.Fatal("pending candidates should fire the nudge")
	}
	for _, want := range []string{"2건", "skill_lifecycle", "self_correction_review", "NO_REPLY"} {
		if !strings.Contains(nudge, want) {
			t.Errorf("nudge missing %q:\n%s", want, nudge)
		}
	}

	// Same fingerprint, 30 minutes later: throttled.
	if got := task.detectSelfCodingNudge(now.Add(30 * time.Minute)); got != "" {
		t.Fatalf("unchanged pending set should be throttled, got %q", got)
	}

	// Same fingerprint past the retry window: fires again (queue must not rot).
	if got := task.detectSelfCodingNudge(now.Add(selfCodingRetryInterval + time.Minute)); got == "" {
		t.Fatal("unchanged pending set should re-fire past the retry window")
	}

	// New candidate (fingerprint change): fires at the next tick regardless.
	task.proposedSelfCoding = func() (int, string) { return 3, "3:sc-b:200" }
	if got := task.detectSelfCodingNudge(now.Add(selfCodingRetryInterval + 31*time.Minute)); got == "" {
		t.Fatal("changed pending set should fire immediately")
	}
}

func TestDetectSelfCodingNudge_QuietPaths(t *testing.T) {
	// No counter wired (tracker absent) → lane disabled.
	bare := &heartbeatTask{homeDir: t.TempDir(), logger: slog.Default()}
	if got := bare.detectSelfCodingNudge(time.Now()); got != "" {
		t.Fatalf("nil counter should disable the lane, got %q", got)
	}

	// Empty queue → no nudge, no marker write.
	task := selfCodingTask(t, 0, "")
	if got := task.detectSelfCodingNudge(time.Now()); got != "" {
		t.Fatalf("empty queue should stay quiet, got %q", got)
	}
}

// The self-coding nudge alone must warrant a turn and sit between the user's
// own checks and the research lane in the composed body.
func TestComposeHeartbeatBody_SelfCodingLane(t *testing.T) {
	body := composeHeartbeatBody("", "", "[자가코딩 제안 검토] 2건", "")
	if !strings.Contains(body, "[자가코딩 제안 검토] 2건") || !strings.Contains(body, "등록된 작업은 없습니다") {
		t.Errorf("selfcoding-only body wrong:\n%s", body)
	}

	body = composeHeartbeatBody("신호", "- 작업", "[자가코딩]", "[연구]")
	si := strings.Index(body, "신호")
	ci := strings.Index(body, "- 작업")
	sci := strings.Index(body, "[자가코딩]")
	ri := strings.Index(body, "[연구]")
	if !(si >= 0 && si < ci && ci < sci && sci < ri) {
		t.Errorf("section order wrong (signal=%d content=%d selfcoding=%d research=%d):\n%s",
			si, ci, sci, ri, body)
	}
}
