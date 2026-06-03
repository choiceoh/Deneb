package server

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
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

	// nativeSync is a durable cursor-based outbox for native clients. It makes
	// proactive transcript changes recoverable even when the SSE push is missed.
	nativeSync interface {
		Append(nativesync.AppendInput) (nativesync.Event, error)
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
	// Respect the NO_REPLY silent-reply contract: a proactive turn that correctly
	// signals "nothing to report" with the token (alone or trailing) must be
	// suppressed — not delivered as a literal "NO_REPLY" work-feed card + push.
	// isContentlessProactive below only catches the chatty prose variants
	// ("메일이 없습니다" etc.); the bare token needs this explicit strip.
	if content = chat.StripSilentToken(content); strings.TrimSpace(content) == "" {
		return false, nil
	}
	// Floor: drop "nothing to report" pings before they reach the transcript,
	// work feed, or push. A proactive agent turn that ignores its NO_REPLY
	// contract and writes a chatty "읽지 않은 메일이 없습니다" (an email-check cron
	// firing every poll cycle), a dreaming "변경 없음", or a "(분석 실패)" stub would
	// otherwise pile up as 업무 리포트 work-feed cards + pushes — the
	// over-notification the project forbids. Reported as not-delivered
	// (false, nil): benign for callers (cron logs "not-delivered" without
	// erroring), and the raw agent output is still kept in the cron run log for
	// diagnosis.
	if isContentlessProactive(content) {
		return false, nil
	}
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
	if d.nativeSync != nil {
		if _, err := d.nativeSync.Append(nativesync.TranscriptAppended(
			nativeWorkSessionKey,
			"assistant",
			pushPreview(content),
			msg.Timestamp,
		)); err != nil && d.logger != nil {
			d.logger.Error("proactive native relay: native sync append failed",
				"sessionKey", nativeWorkSessionKey, "error", err)
		}
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

// contentlessProactiveFragments mark a proactive body as carrying nothing
// actionable: an email-check turn that found no mail, a dreaming cycle with no
// changes, or an analysis stub. Matched only against short single-line bodies
// (see isContentlessProactive) so a real multi-section report that merely
// mentions one of these is never affected.
var contentlessProactiveFragments = []string{
	"분석 실패",    // gmailpoll batch-analyze stub "(분석 실패)"
	"변경 없음",    // autonomous dreaming: nothing consolidated this cycle
	"검색 결과 없음", // "검색 결과 없음 — 읽지 않은 ... 없습니다"
	"알림이 없",    // "읽지 않은 카카오메일 알림이 없습니다"
	"알림 없음",    // 조사 없는 변형: "읽지 않은 카카오메일 알림 없음"
	"메일이 없",    // "분석할 새 메일이 없습니다"
	"메일 없어요",   // 캐주얼 변형: "분석할 메일 없어요" (actionable brief는 "...필요해요"로 끝남)
	"패스할게요",    // "...없으니 패스할게요"
}

// isContentlessProactive reports whether a proactive body is a "nothing to
// report" ping that should never reach the user. It is a backstop for proactive
// agent turns (notably an email-check cron) that ignore the NO_REPLY contract
// and emit a chatty "없습니다" line anyway; without it each such line lands as a
// 업무 리포트 work-feed card + push every poll cycle.
//
// Conservative by design: only short (≤120 rune), single-line bodies are
// eligible, so a genuine multi-section report that happens to contain "없음"
// (e.g. "긴급 메일 없음, 단 X 확인 필요" inside a brief) is left untouched.
func isContentlessProactive(content string) bool {
	s := strings.TrimSpace(content)
	if s == "" {
		return true
	}
	if strings.Contains(s, "\n") || len([]rune(s)) > 120 {
		return false
	}
	for _, frag := range contentlessProactiveFragments {
		if strings.Contains(s, frag) {
			return true
		}
	}
	return false
}

// deliverNativeImage appends an image attachment (e.g. the rendered 주간업무보고
// form) to the native 업무 chat with a short caption, and live-pushes a
// notification. The caption is the message body — the native chat skips
// empty-content assistant messages, so a non-empty caption is required for the
// bubble (and its image) to render at all. Best-effort: returns (false, nil)
// when no transcript store is wired or the image is empty.
func (d proactiveRelayDeps) deliverNativeImage(caption string, pngBytes []byte) (bool, error) {
	if d.transcriptStore == nil || len(pngBytes) == 0 {
		return false, nil
	}
	msg := toolctx.NewTextChatMessage("assistant", caption, time.Now().UnixMilli())
	msg.Attachments = []toolctx.ChatAttachment{{
		Type:     "image",
		MimeType: "image/png",
		Data:     base64.StdEncoding.EncodeToString(pngBytes),
		Name:     "weekly-report.png",
		Size:     int64(len(pngBytes)),
	}}
	if err := d.transcriptStore.Append(nativeWorkSessionKey, msg); err != nil {
		if d.logger != nil {
			d.logger.Error("proactive native image: transcript append failed",
				"sessionKey", nativeWorkSessionKey, "error", err)
		}
		return false, err
	}
	if d.pushHub != nil {
		d.pushHub.publish(clientPushEvent{Title: "Deneb", Body: caption})
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
