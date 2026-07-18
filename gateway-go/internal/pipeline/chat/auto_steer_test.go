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

func TestTrySteerFoldsShortFollowUpWhenRunActive(t *testing.T) {
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

// TestTrySteerAllowsNativeClientCarveOut proves the native surface folds
// even though it sets AutoDeliveredOutput. miniapp.chat.send ALWAYS sets that
// flag (reply returned as the RPC result, not pushed via the message tool) AND
// Delivery.Channel="client". Blocking steer on the flag alone left it dead on
// the sole native entry (measured live: a mid-turn correction raced a second
// concurrent run); the carve-out restores folding while keeping autonomous
// AutoDeliveredOutput relays (cron/mailpoll — no client delivery) excluded.
func TestTrySteerAllowsNativeClientCarveOut(t *testing.T) {
	h := newSteerTestHandler()
	markActiveRun(h, "client:main")

	nativeOpts := &SyncOptions{
		AutoDeliveredOutput: true,
		Delivery:            &DeliveryContext{Channel: "client", To: "main"},
	}
	res, handled := h.trySteerIntoActiveRun("client:main", "아 남도에코만 봐줘", nativeOpts)
	if !handled {
		t.Fatal("native client + AutoDeliveredOutput must fold (interactive mid-turn correction)")
	}
	if res.StopReason != "steered" {
		t.Fatalf("unexpected ack: %+v", res)
	}
	notes := h.steer.Drain("client:main")
	if len(notes) != 1 || !strings.Contains(notes[0], "남도에코") {
		t.Fatalf("steer queue missing the native note: %v", notes)
	}
}

func TestTrySteerRejectsGatedCases(t *testing.T) {
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
		{"auto-delivered relay, no client channel (cron/mailpoll)", "client:main", "짧은 정정", &SyncOptions{AutoDeliveredOutput: true}},
		{"auto-delivered relay on a non-client channel", "client:main", "짧은 정정", &SyncOptions{AutoDeliveredOutput: true, Delivery: &DeliveryContext{Channel: "telegram", To: "telegram:1"}}},
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

// markActiveAutomationRun registers an autonomous relay (heartbeat/cron)
// on the session — the shape that must NOT absorb a user's message.
func markActiveAutomationRun(h *Handler, sessionKey string) {
	h.abort.Register("auto-"+sessionKey, &AbortEntry{
		SessionKey: sessionKey,
		ClientRun:  "auto-" + sessionKey,
		CancelFn:   func(error) {},
		ExpiresAt:  time.Now().Add(time.Hour),
		Automation: true,
	})
}

// The reported bug: a user types into a session that isn't in an interactive
// turn, but a heartbeat/cron automation run happens to be active on the same
// key (client:main). Auto-steer must decline so the message starts a normal
// new turn instead of folding into the automation transcript.
func TestTrySteerDeclinesWhenOnlyAutomationRunActive(t *testing.T) {
	h := newSteerTestHandler()
	markActiveAutomationRun(h, "client:main")

	res, handled := h.trySteerIntoActiveRun("client:main", "지금 뭐 하고 있어?", &SyncOptions{
		Delivery: &DeliveryContext{Channel: "client", To: "main"},
	})
	if handled || res != nil {
		t.Fatalf("steered into an automation run: handled=%v res=%+v", handled, res)
	}
}

// A concurrent interactive run alongside the automation run DOES steer — the
// user is genuinely mid-turn.
func TestTrySteerFoldsWhenInteractiveRunActiveDespiteAutomation(t *testing.T) {
	h := newSteerTestHandler()
	markActiveAutomationRun(h, "client:main")
	markActiveRun(h, "client:main")

	res, handled := h.trySteerIntoActiveRun("client:main", "아 그거 말고", &SyncOptions{
		Delivery: &DeliveryContext{Channel: "client", To: "main"},
	})
	if !handled || res == nil {
		t.Fatal("interactive run present: steer should fold")
	}
}
