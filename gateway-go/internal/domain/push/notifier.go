package push

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// sender is the FCM send capability the notifier needs. Satisfied by *FCMSender;
// kept as an interface so the notifier is unit-testable without real creds.
type sender interface {
	Send(ctx context.Context, deviceToken, title, body string, data map[string]string) SendResult
}

// tokenStore is the device-token capability the notifier needs.
type tokenStore interface {
	Tokens() []DeviceToken
	Prune(tokens []string) (int, error)
}

// fallbackDeliveryTimeout bounds one fan-out across all registered device
// tokens. Derived from the server shutdown context so a delivery in flight can
// be cancelled on graceful shutdown.
const fallbackDeliveryTimeout = 30 * time.Second

// Notifier delivers a proactive notification to every registered device token
// via FCM. It is the fallback used when no native client holds a live SSE
// connection (app fully closed / Doze) — see runtime/server/proactive_relay.go.
type Notifier struct {
	store       tokenStore
	sender      sender
	logger      *slog.Logger
	broadcast   func(event string, payload any)
	shutdownCtx context.Context
}

// NotifierDeps wires a Notifier.
type NotifierDeps struct {
	Store       tokenStore
	Sender      sender
	Logger      *slog.Logger
	Broadcast   func(event string, payload any) // operator-visible failure mirror
	ShutdownCtx context.Context
}

// NewNotifier builds a Notifier. Returns nil when the sender or store is absent
// (dormant integration), so callers can leave the proactive-relay field nil and
// the fallback is simply skipped.
func NewNotifier(deps NotifierDeps) *Notifier {
	if deps.Sender == nil || deps.Store == nil {
		return nil
	}
	ctx := deps.ShutdownCtx
	if ctx == nil {
		ctx = context.Background()
	}
	return &Notifier{
		store:       deps.Store,
		sender:      deps.Sender,
		logger:      deps.Logger,
		broadcast:   deps.Broadcast,
		shutdownCtx: ctx,
	}
}

// DeliverFallback pushes {title, body} to all registered device tokens via FCM.
// It is fire-and-forget (async) so it never blocks the proactive relay, and
// nil-safe so a dormant integration is a no-op. Dead tokens are pruned; a
// complete failure to reach any device is logged Error + broadcast, since a
// user-observable proactive notification was dropped (see .claude/rules/logging.md).
func (n *Notifier) DeliverFallback(title, body string) {
	if n == nil {
		return
	}
	// Snapshot synchronously (cheap, lock-guarded) so the relay goroutine isn't
	// held for the network sends below.
	tokens := n.store.Tokens()
	if len(tokens) == 0 {
		// No registered device — the report is still in the transcript; the app
		// shows it on next open. Not an error.
		if n.logger != nil {
			n.logger.Debug("push fallback: no registered device tokens; skipping FCM")
		}
		return
	}
	safego.GoWithSlog(n.logger, "push-fcm-fallback", func() {
		ctx, cancel := context.WithTimeout(n.shutdownCtx, fallbackDeliveryTimeout)
		defer cancel()

		var (
			delivered int
			dead      []string
			authFail  bool
			lastErr   error
		)
		for _, t := range tokens {
			res := n.sender.Send(ctx, t.Token, title, body, map[string]string{"kind": "proactive"})
			switch {
			case res.OK:
				delivered++
			case res.Permanent:
				dead = append(dead, t.Token)
				lastErr = res.Err
			case res.AuthFailed:
				authFail = true
				lastErr = res.Err
			default:
				lastErr = res.Err
			}
		}
		if len(dead) > 0 {
			if removed, err := n.store.Prune(dead); err != nil {
				if n.logger != nil {
					n.logger.Error("push fallback: pruning dead tokens failed", "error", errStr(err))
				}
			} else if n.logger != nil {
				n.logger.Info("push fallback: pruned stale device tokens", "count", removed)
			}
		}
		n.report(len(tokens), delivered, authFail, lastErr)
	})
}

// report logs + (on total failure) broadcasts the outcome. Nobody receiving the
// push is a user-observable drop → Error + broadcast.
func (n *Notifier) report(total, delivered int, authFail bool, lastErr error) {
	switch {
	case delivered == total:
		if n.logger != nil {
			n.logger.Info("push fallback delivered", "devices", delivered)
		}
	case delivered > 0:
		if n.logger != nil {
			n.logger.Warn("push fallback partial delivery",
				"delivered", delivered, "total", total, "error", errStr(lastErr))
		}
	default:
		reason := "fcm_send_failed"
		if authFail {
			reason = "fcm_auth_failed" // operator must fix the service account
		}
		if n.logger != nil {
			n.logger.Error("push fallback failed: proactive notification not delivered to any device",
				"reason", reason, "devices", total, "error", errStr(lastErr))
		}
		if n.broadcast != nil {
			n.broadcast("push.delivery_failed", map[string]any{
				"reason":  reason,
				"devices": total,
				"error":   errStr(lastErr),
			})
		}
	}
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
