package chat

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func newSteerTestHandler() *Handler {
	return &Handler{
		abort:  NewAbortTracker(),
		steer:  NewSteerQueue(),
		logger: slog.Default(),
	}
}

func markActiveRun(h *Handler, sessionKey string) {
	h.abort.Register("active-"+sessionKey, &AbortEntry{
		SessionKey: sessionKey,
		ClientRun:  "active-" + sessionKey,
		CancelFn:   func(error) {},
		ExpiresAt:  time.Now().Add(time.Hour),
	})
}

func TestTrySteerIntoActiveRun_FoldsShortFollowUp(t *testing.T) {
	h := newSteerTestHandler()
	markActiveRun(h, "client:main")

	res, handled := h.trySteerIntoActiveRun("client:main", "아 내일 말고 모레로 해줘", &SyncOptions{})
	if !handled {
		t.Fatal("expected the mid-run follow-up to be steered")
	}
	if res.StopReason != "steered" || res.Text == "" {
		t.Fatalf("unexpected ack: %+v", res)
	}
	notes := h.steer.Drain("client:main")
	if len(notes) != 1 || !strings.Contains(notes[0], "모레") {
		t.Fatalf("steer queue missing the note: %v", notes)
	}
}

func TestTrySteerIntoActiveRun_Gates(t *testing.T) {
	h := newSteerTestHandler()
	markActiveRun(h, "client:main")

	cases := []struct {
		name    string
		session string
		message string
		opts    *SyncOptions
	}{
		{"no active run", "client:other", "짧은 정정", &SyncOptions{}},
		{"nil opts (non-interactive caller)", "client:main", "짧은 정정", nil},
		{"API messages traffic", "client:main", "짧은 정정", &SyncOptions{Messages: make([]llm.Message, 1)}},
		{"autonomous ephemeral surface", "client:main", "짧은 정정", &SyncOptions{EphemeralUser: true}},
		{"auto-delivered surface (cron)", "client:main", "짧은 정정", &SyncOptions{AutoDeliveredOutput: true}},
		{"blank message", "client:main", "   ", &SyncOptions{}},
		{"long message is a new request", "client:main", strings.Repeat("가", steerMaxRunes+1), &SyncOptions{}},
	}
	for _, c := range cases {
		if _, handled := h.trySteerIntoActiveRun(c.session, c.message, c.opts); handled {
			t.Errorf("%s: must NOT steer", c.name)
		}
	}
	if notes := h.steer.Drain("client:main"); len(notes) != 0 {
		t.Errorf("gated cases leaked a steer note: %v", notes)
	}
}
