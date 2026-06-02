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

// proactiveNativeOnly routes all proactive reports (cron jobs, gmail poll, wiki
// dreaming) to the native client only: the operator's decision (2026-06-01,
// after retiring the Telegram Mini App) is that proactive output lands in the
// app's 업무 chat (client:main transcript + live push) and never in Telegram.
// Flip to false to restore Telegram proactive delivery.
const proactiveNativeOnly = true

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
	// activeHome resolves the homeSessionKey sentinel ("telegram:home") to the
	// live active-home chat ID — ActiveHome (set by /use-forum) first, else the
	// static telegram.chatID. Returns 0 when neither is available. Read lazily
	// at send time so a mid-session migration is followed without re-wiring the
	// proactive notifiers. May be nil (older wiring / tests); the sentinel then
	// stays unresolved and the relay no-ops.
	activeHome func() int64

	// pushHub fans a {title, body} frame out to connected native clients when a
	// report mirrors to the 업무 topic, so the app raises a notification live
	// instead of waiting for its next heartbeat poll. nil in older wiring/tests;
	// the push is then skipped (the report still lands in the transcript).
	pushHub *clientPushHub

	// nativeOnly routes every proactive report to the native client only (the
	// 업무/client:main transcript + a live push) and skips Telegram entirely.
	// Set from proactiveNativeOnly at construction.
	nativeOnly bool
}

// relay delivers content to sessionKey's channel and records it in the
// transcript. Returns (true, nil) when delivered. Returns (false, nil)
// when the session key can't be resolved to a wired channel — the caller
// uses that signal to fall back to an alternate delivery path.
func (d proactiveRelayDeps) relay(ctx context.Context, sessionKey, content string) (bool, error) {
	if strings.TrimSpace(content) == "" {
		return false, nil
	}
	if d.nativeOnly {
		// All proactive output goes to the native client's 업무 chat only. The
		// Telegram target (sessionKey) is intentionally ignored — every
		// proactive report is a work report and lands in client:main.
		return d.relayNative(content)
	}
	sessionKey, ok := d.resolveHome(sessionKey)
	if !ok {
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
		chatPart, threadPart := splitTelegramTarget(target)
		chatID, err := strconv.ParseInt(chatPart, 10, 64)
		if err != nil {
			return false, fmt.Errorf("proactive relay: parse chatID %q: %w", chatPart, err)
		}
		threadID, _ := strconv.ParseInt(threadPart, 10, 64)
		if _, err := telegram.SendText(ctx, client, chatID, content, telegram.SendOptions{ThreadID: threadID}); err != nil {
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
		// Mirror into the native client's 업무 topic. The Android client reads
		// the General/업무 forum thread (threadID 0, i.e. no ":thread:" suffix)
		// from the "client:main" session, separate from the Telegram-keyed
		// transcript above. Surfaces proactive reports (morning-letter,
		// email-analysis) in the app's 업무 chat in addition to Telegram —
		// no second send, no LLM. Named topics (코딩/잡담) are excluded so work
		// reports don't pollute them. See DenebGatewayClient.topicSessionKey:
		// threadId "0" → "client:main".
		if mirrorsToNativeWork(channel, target) {
			if err := d.transcriptStore.Append(nativeWorkSessionKey, msg); err != nil && d.logger != nil {
				d.logger.Error("proactive relay: native mirror append failed",
					"sessionKey", nativeWorkSessionKey, "error", err)
			}
			// Live push to connected native clients so the app notifies now,
			// not at the next heartbeat. Best-effort: an asleep/disconnected
			// client misses this and picks the report up from the transcript.
			if d.pushHub != nil {
				d.pushHub.publish(clientPushEvent{
					Title: "Deneb",
					Body:  pushPreview(content),
				})
			}
		}
	}
	return true, nil
}

// relayNative delivers a proactive report to the native client only: it appends
// the body to the 업무 (client:main) transcript so the app shows it, and live-
// pushes a one-line preview so a connected app notifies immediately. No Telegram
// send. Returns (false, nil) when no transcript store is wired (older wiring or
// tests) so the caller treats it as not-delivered.
func (d proactiveRelayDeps) relayNative(content string) (bool, error) {
	if d.transcriptStore == nil {
		// No transcript store wired means every proactive report (morning
		// letter, mail analysis) is silently dropped in native-only mode — the
		// user observes nothing arriving. Surface it so a misconfigured startup
		// is diagnosable instead of mysteriously quiet.
		if d.logger != nil {
			d.logger.Error("proactive native relay: no transcript store wired — report dropped",
				"sessionKey", nativeWorkSessionKey)
		}
		return false, nil
	}
	msg := toolctx.NewTextChatMessage("assistant", content, time.Now().UnixMilli())
	if err := d.transcriptStore.Append(nativeWorkSessionKey, msg); err != nil {
		if d.logger != nil {
			d.logger.Error("proactive native relay: transcript append failed",
				"sessionKey", nativeWorkSessionKey, "error", err)
		}
		return false, err
	}
	if d.pushHub != nil {
		d.pushHub.publish(clientPushEvent{
			Title: "Deneb",
			Body:  pushPreview(content),
		})
	}
	return true, nil
}

// pushPreview trims a relayed body to a notification-sized single line. The full
// report is in the transcript; the push is just the nudge to open it.
func pushPreview(content string) string {
	s := strings.TrimSpace(content)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	const max = 140
	if len([]rune(s)) > max {
		s = string([]rune(s)[:max]) + "…"
	}
	return s
}

// nativeWorkSessionKey is the session the Android client's 업무 (General) topic
// reads — mirror target for proactive reports so they appear in the app's work
// chat alongside Telegram. Kept in sync with DenebGatewayClient.topicSessionKey
// (threadId "0" → "client:main").
const nativeWorkSessionKey = "client:main"

// mirrorsToNativeWork reports whether a relayed body should also be appended to
// the native client's 업무 transcript. True only for a Telegram General-topic
// target (no ":thread:" suffix, or thread 0) — named forum topics (코딩/잡담)
// map to their own native sessions and must not receive work reports.
func mirrorsToNativeWork(channel, target string) bool {
	if channel != "telegram" {
		return false
	}
	_, threadPart := splitTelegramTarget(target)
	return threadPart == "" || threadPart == "0"
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

// splitTelegramTarget pulls an optional ":thread:N" suffix off a target so the
// relay can route into a forum topic. For non-forum chats the suffix is
// absent and threadPart is "".
func splitTelegramTarget(target string) (chatPart, threadPart string) {
	if idx := strings.Index(target, ":thread:"); idx >= 0 {
		return target[:idx], target[idx+len(":thread:"):]
	}
	return target, ""
}

// homeSessionKey is the sentinel proactive target that resolves to the live
// active home at send time. Proactive senders (gmail poll, dreaming) wire their
// notifier with this constant so delivery follows /use-forum migrations and
// topic restructures without re-wiring or a gateway restart.
const homeSessionKey = "telegram:home"

// resolveHome rewrites the homeSessionKey sentinel to the concrete active-home
// session key (e.g. "telegram:-1003946703971"). A bare home chat (no
// ":thread:") lands in the supergroup's General topic, which Telegram forbids
// deleting — the most restructure-proof proactive target. Non-sentinel keys
// pass through unchanged (ok=true). Returns ok=false only when the sentinel
// can't be resolved (no active home and no static chatID), so the caller
// no-ops instead of attempting an invalid send.
func (d proactiveRelayDeps) resolveHome(sessionKey string) (string, bool) {
	if sessionKey != homeSessionKey {
		return sessionKey, true
	}
	if d.activeHome != nil {
		if id := d.activeHome(); id != 0 {
			return "telegram:" + strconv.FormatInt(id, 10), true
		}
	}
	return "", false
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
	// In native-only mode delivery doesn't need the Telegram plugin; otherwise a
	// nil plugin means there's no channel to reach, so skip wiring.
	if !d.nativeOnly && d.telegramPlug == nil {
		return nil
	}
	return &relayNotifier{deps: d, sessionKey: sessionKey}
}
