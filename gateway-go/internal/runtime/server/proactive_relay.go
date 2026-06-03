package server

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
)

// Compile-time interface compliance — same notifier satisfies both the
// autonomous service (wiki dreaming) and gmail polling.
var (
	_ autonomous.Notifier = (*relayNotifier)(nil)
	_ gmailpoll.Notifier  = (*relayNotifier)(nil)
)

// proactiveRelayDeps delivers a pre-composed body to the native client's 업무
// chat (client:main transcript + live push) without routing through the LLM.
//
// All proactive output (cron reports, gmail summaries, wiki dreaming) lands
// here since the Telegram bot was retired (2026-06). The body is sent verbatim
// and a matching assistant message is appended to the session transcript so a
// follow-up user turn ("더 자세히 알려줘") has the proactive content in context.
type proactiveRelayDeps struct {
	transcriptStore toolctx.TranscriptStore
	logger          interface{ Error(string, ...any) } // *slog.Logger subset

	// pushHub fans a {title, body} frame out to connected native clients when a
	// report arrives, so the app raises a notification live instead of waiting
	// for its next heartbeat poll. nil in older wiring/tests; the push is then
	// skipped (the report still lands in the transcript).
	pushHub *clientPushHub

	// workFeed records each proactive report as a first-class native work item.
	// Best-effort only: transcript delivery remains the source of truth.
	workFeed interface {
		Append(workfeed.Item) (workfeed.Item, error)
	}
}

// relay delivers content to the native client (업무 transcript + live push).
// sessionKey is accepted for signature compatibility with existing callers but
// is ignored — all proactive output lands in client:main. Returns (false, nil)
// when the relay has no transcript store (older wiring / tests).
func (d proactiveRelayDeps) relay(_ context.Context, _, content string) (bool, error) {
	if strings.TrimSpace(content) == "" {
		return false, nil
	}
	return d.relayNative(content)
}

// relayNative delivers a proactive report to the native client only: it appends
// the body to the 업무 (client:main) transcript so the app shows it, and live-
// pushes a one-line preview so a connected app notifies immediately. Returns
// (false, nil) when no transcript store is wired (older wiring or tests) so
// the caller treats it as not-delivered.
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
	if d.workFeed != nil {
		if _, err := d.workFeed.Append(workfeed.Item{
			Source:     workfeed.SourceProactive,
			Title:      "업무 리포트",
			Summary:    workfeed.Preview(content, 180),
			Body:       content,
			SessionKey: nativeWorkSessionKey,
		}); err != nil && d.logger != nil {
			d.logger.Error("proactive native relay: work feed append failed",
				"sessionKey", nativeWorkSessionKey, "error", err)
		}
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
// reads. Kept in sync with DenebGatewayClient.topicSessionKey (threadId "0" →
// "client:main").
const nativeWorkSessionKey = "client:main"

// nativeWorkSessionKeyTo is the "to" half of nativeWorkSessionKey. Used as the
// cron DefaultTo so a job without an explicit delivery target resolves to a
// valid recipient — the handoff rebuilds "client:" + "main" = client:main and
// the relay routes it to the native 업무 chat.
const nativeWorkSessionKeyTo = "main"

// relayNotifier adapts proactiveRelayDeps to the Notifier interface used by
// both the autonomous service (wiki dreaming) and gmailpoll. It binds a session
// key at construction so Notify(ctx, message) delivers there.
type relayNotifier struct {
	deps       proactiveRelayDeps
	sessionKey string
}

// Notify satisfies autonomous.Notifier and gmailpoll.Notifier. Returns the
// underlying send error; delivery-not-wired (relay returns false with no error)
// is treated as a silent no-op.
func (n *relayNotifier) Notify(ctx context.Context, message string) error {
	_, err := n.deps.relay(ctx, n.sessionKey, message)
	return err
}

// notifierForSession binds the relay to a session key and returns a Notifier
// ready to plug into autonomous.Service or gmailpoll.Service. Always returns a
// non-nil notifier because the native relay requires only a transcript store,
// not a Telegram plugin.
func (d proactiveRelayDeps) notifierForSession(sessionKey string) *relayNotifier {
	return &relayNotifier{deps: d, sessionKey: sessionKey}
}
