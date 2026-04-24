package server

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// Compile-time interface compliance — same notifier satisfies both the
// autonomous service (wiki dreaming) and gmail polling.
var (
	_ autonomous.Notifier = (*relayNotifier)(nil)
	_ gmailpoll.Notifier  = (*relayNotifier)(nil)
)

// proactiveRelayDeps delivers a pre-composed body to a user's channel
// without routing through the LLM.
//
// Before this helper existed, cron job completions were handed to the main
// agent as a "relay this body verbatim" directive. The LLM could — and
// sometimes did — deviate: e.g. call wiki/memory tools and reply with a
// terse action report ("위키 업데이트 완료") instead of the body itself.
// The user saw only the side effect and missed the content.
//
// This helper takes the LLM out of the delivery critical path: the body
// is sent verbatim via the channel plugin, and a matching assistant
// message is appended to the session transcript so a follow-up user turn
// ("더 자세히 알려줘") still has the proactive content in context.
type proactiveRelayDeps struct {
	telegramPlug    *telegram.Plugin
	transcriptStore toolctx.TranscriptStore
	logger          *slog.Logger
}

// relay delivers content to sessionKey's channel and records it in the
// transcript. Returns (true, nil) when delivered. Returns (false, nil)
// when the session key can't be resolved to a wired channel — the caller
// uses that signal to fall back to an alternate delivery path.
func (d proactiveRelayDeps) relay(ctx context.Context, sessionKey, content string) (bool, error) {
	if strings.TrimSpace(content) == "" {
		return false, nil
	}
	channel, target, ok := splitSessionKey(sessionKey)
	if !ok {
		return false, nil
	}

	switch channel {
	case "telegram":
		if d.telegramPlug == nil {
			return false, nil
		}
		client := d.telegramPlug.Client()
		if client == nil {
			return false, nil
		}
		chatID, err := strconv.ParseInt(target, 10, 64)
		if err != nil {
			return false, fmt.Errorf("proactive relay: parse chatID %q: %w", target, err)
		}
		if _, err := telegram.SendText(ctx, client, chatID, content, telegram.SendOptions{}); err != nil {
			return false, fmt.Errorf("proactive relay: telegram send: %w", err)
		}
	default:
		return false, nil
	}

	// Append the delivered body to the transcript as an assistant message
	// so the next user turn is answered in a session that knows what was
	// just relayed. Failure is non-fatal: the user already received the
	// message, and losing transcript context is degraded-but-acceptable.
	if d.transcriptStore != nil {
		msg := toolctx.NewTextChatMessage("assistant", content, time.Now().UnixMilli())
		if err := d.transcriptStore.Append(sessionKey, msg); err != nil && d.logger != nil {
			d.logger.Error("proactive relay: transcript append failed",
				"sessionKey", sessionKey, "error", err)
		}
	}
	return true, nil
}

// splitSessionKey parses "channel:target" (e.g. "telegram:7074071666").
// Returns ok=false when the key has no colon, an empty channel, or an
// empty target.
func splitSessionKey(key string) (channel, target string, ok bool) {
	idx := strings.Index(key, ":")
	if idx <= 0 || idx == len(key)-1 {
		return "", "", false
	}
	return key[:idx], key[idx+1:], true
}

// relayNotifier adapts proactiveRelayDeps to the Notifier interface used
// by both the autonomous service (wiki dreaming) and gmailpoll. It binds
// a session key at construction so Notify(ctx, message) delivers there.
type relayNotifier struct {
	deps       proactiveRelayDeps
	sessionKey string
}

// Notify satisfies autonomous.Notifier and gmailpoll.Notifier (identical
// signatures). Returns the underlying send error; delivery-not-wired
// (relay returns false with no error) is treated as a silent no-op so
// startup ordering — where a notifier may fire before the telegram
// plugin is connected — doesn't surface spurious errors.
func (n *relayNotifier) Notify(ctx context.Context, message string) error {
	_, err := n.deps.relay(ctx, n.sessionKey, message)
	return err
}

// notifierForSession binds the relay to a session key and returns a
// Notifier ready to plug into autonomous.Service or gmailpoll.Service.
// Returns nil when the deps are unusable (no telegram plugin) so the
// caller can skip wiring instead of attaching a notifier that will
// always no-op.
func (d proactiveRelayDeps) notifierForSession(sessionKey string) *relayNotifier {
	if d.telegramPlug == nil {
		return nil
	}
	return &relayNotifier{deps: d, sessionKey: sessionKey}
}
