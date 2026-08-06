package push

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// sender is the FCM send capability the notifier needs. Satisfied by *FCMSender;
// kept as an interface so the notifier is unit-testable without real creds.
type sender interface {
	Send(ctx context.Context, deviceToken, title, body string, data map[string]string) SendResult
	Ready(ctx context.Context) error
}

// tokenStore is the device-token capability the notifier needs.
type tokenStore interface {
	Tokens() []DeviceToken
	Prune(tokens []string) (int, error)
}

const (
	// fallbackDeliveryTimeout bounds one fan-out across all registered device
	// tokens. Derived from the server shutdown context so a delivery in flight can
	// be cancelled on graceful shutdown.
	fallbackDeliveryTimeout = 30 * time.Second
	// deliveryReadyTimeout bounds the foreground registration probe. The native
	// client uses this result to decide whether background SSE can be dropped.
	deliveryReadyTimeout = 3 * time.Second
)

// Notifier delivers a proactive notification to every registered device token
// via FCM. It is the fallback used when no native client holds a live SSE
// connection (app fully closed / Doze) — see runtime/proactive/proactive_relay.go.
type Notifier struct {
	store       tokenStore
	sender      sender
	logger      *slog.Logger
	broadcast   func(event string, payload rawJSON)
	shutdownCtx context.Context
}

// NotifierDeps wires a Notifier.
type NotifierDeps struct {
	Store       tokenStore
	Sender      sender
	Logger      *slog.Logger
	Broadcast   func(event string, payload rawJSON) // operator-visible failure mirror
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
// complete auth/config/payload failure is logged Error + broadcast, while
// retryable external unavailability is Warn + broadcast because the report is
// already durable in the work feed/transcript.
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
			transient bool
			hardFail  bool
			lastErr   error
		)
		for _, t := range tokens {
			res := n.sender.Send(ctx, t.Token, title, body, map[string]string{"kind": "proactive"})
			switch {
			case res.OK:
				delivered++
			case res.Permanent:
				dead = append(dead, t.Token)
				hardFail = true
				lastErr = res.Err
			case res.AuthFailed:
				authFail = true
				lastErr = res.Err
			case res.Transient:
				transient = true
				lastErr = res.Err
			default:
				hardFail = true
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
		n.report(len(tokens), delivered, authFail, transient, hardFail, lastErr)
	})
}

// DeliveryEnabled reports whether FCM can currently mint an access token. A
// failed probe means the phone should keep background SSE alive instead of
// trusting the FCM handoff.
func (n *Notifier) DeliveryEnabled(ctx context.Context) bool {
	if n == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, deliveryReadyTimeout)
	defer cancel()
	if err := n.sender.Ready(probeCtx); err != nil {
		if n.logger != nil {
			n.logger.Warn("FCM push fallback unavailable; background SSE remains required", "error", errStr(err))
		}
		return false
	}
	return true
}

// report logs + (on total failure) broadcasts the outcome. Nobody receiving the
// push due auth/config/payload failure is Error; retryable external dependency
// failures are Warn because the report is already durable in the app surfaces.
func (n *Notifier) report(total, delivered int, authFail, transient, hardFail bool, lastErr error) {
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
		logError := true
		if authFail {
			reason = "fcm_auth_failed" // operator must fix the service account
		} else if transient && !hardFail {
			reason = "fcm_unavailable"
			logError = false
		}
		if n.logger != nil {
			if logError {
				n.logger.Error("push fallback failed: proactive notification not delivered to any device",
					"reason", reason, "devices", total, "error", errStr(lastErr))
			} else {
				n.logger.Warn("push fallback failed: proactive notification not delivered to any device",
					"reason", reason, "devices", total, "error", errStr(lastErr))
			}
		}
		if n.broadcast != nil {
			raw, err := json.Marshal(map[string]any{
				"reason":  reason,
				"devices": total,
				"error":   errStr(lastErr),
			})
			if err != nil {
				if n.logger != nil {
					n.logger.Warn("push delivery_failed marshal failed", "error", err)
				}
			} else {
				n.broadcast("push.delivery_failed", raw)
			}
		}
	}
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
