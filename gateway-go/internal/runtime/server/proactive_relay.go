package server

import (
	"context"
	"encoding/base64"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/push"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/denebui"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
)

// Compile-time interface compliance — same notifier satisfies both the
// autonomous service (wiki dreaming) and gmail polling.
var (
	_ autonomous.Notifier = (*relayNotifier)(nil)
	_ gmailpoll.Notifier  = (*relayNotifier)(nil)
)

const (
	// Work-feed card sizing for proactive reports. Title is one line in the UI;
	// the summary shows ~2 lines.
	workFeedTitleMaxRunes   = 40
	workFeedSummaryMaxRunes = 180

	// genericTitleMaxRunes is the length below which a title (e.g. "분석", "보고")
	// is treated as too generic on its own; extractCardTitle then folds in the
	// next sub-heading ("분석 — 왜 지금 왔는가").
	genericTitleMaxRunes = 6

	// contentlessSubstanceMaxRunes bounds the multi-line contentless check: a
	// body whose substantive text (markers/emoji/whitespace removed) exceeds
	// this is treated as a real report regardless of any "없음" fragment.
	contentlessSubstanceMaxRunes = 120
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

	// behaviorLog records each relay decision (delivered/suppressed/dropped/
	// error) under system:proactive so the autonomous-delivery funnel is
	// observable: how often it fires and how much is suppressed and why. nil-safe;
	// nil in older wiring/tests.
	behaviorLog *agentlog.Writer

	// pushHub fans a {title, body} frame out to connected native clients when a
	// report arrives, so the app raises a notification live instead of waiting
	// for its next heartbeat poll. nil in older wiring/tests; the push is then
	// skipped (the report still lands in the transcript).
	pushHub *clientPushHub

	// pushFCM delivers the same {title, body} to registered device tokens via
	// FCM when no client holds a live SSE connection (app fully closed / Doze) —
	// i.e. when pushHub.publish reaches nobody. nil (dormant) unless FCM
	// credentials are configured; nil-safe. The report is always in the
	// transcript regardless, so this is additive, not load-bearing.
	pushFCM *push.Notifier

	// workFeed records each proactive report as a first-class native work item.
	// Best-effort only: transcript delivery remains the source of truth.
	workFeed interface {
		Append(workfeed.Item) (workfeed.Item, error)
	}

	// cardTitler names a mail-report work-feed card from its body using the
	// lightweight model — the 📬 icon already says "mail", so the card title
	// should be the email's subject, not the generic "메일 분석 리포트" heading the
	// analysis model writes. Best-effort: returns "" on any failure, and the
	// deterministic extractCardTitle heuristic is the fallback. nil in older
	// wiring/tests (then the heuristic is used directly).
	cardTitler func(content string) string

	// nativeSync is a durable cursor-based outbox for native clients. It makes
	// proactive transcript changes recoverable even when the SSE push is missed.
	nativeSync interface {
		Append(nativesync.AppendInput) (nativesync.Event, error)
	}

	// sessions registers the delivery target in the session manager. The native
	// drawer (miniapp.sessions.recent) lists Manager.List(), so a brand-new
	// sub-session that exists only as a transcript file — client:main:dream's
	// first delivery — would otherwise stay invisible until the next restart's
	// transcript rescan. nil in older wiring/tests; registration is then skipped.
	sessions interface {
		EnsureVisible(key, channel string, at int64)
	}
}

// markSessionVisible registers a successful delivery's target session in the
// session manager (create-if-missing in the startup-restore shape, then bump
// UpdatedAt so the drawer sorts it by its newest message). Only native session
// shapes register — restorableTranscriptSession is the same predicate the
// startup rescan uses, so the two paths cannot disagree.
func (d proactiveRelayDeps) markSessionVisible(sessionKey string, ts int64) {
	if d.sessions == nil {
		return
	}
	channel, ok := restorableTranscriptSession(sessionKey)
	if !ok {
		return
	}
	d.sessions.EnsureVisible(sessionKey, channel, ts)
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

// relayCollapsed is relay() with the report delivered as a collapsed
// title-only card (deneb-ui accordion) in the 업무 chat — the user taps it to
// expand the full analysis in place instead of the long prose landing as a
// visible wall of text. Used for per-mail analyses; the work-feed card, push
// preview, and suppression gates all still see the raw prose body.
func (d proactiveRelayDeps) relayCollapsed(_ context.Context, _, content string) (bool, error) {
	if strings.TrimSpace(content) == "" {
		return false, nil
	}
	return d.relayNativeToOpts(nativeWorkSessionKey, content, true)
}

// relayNative delivers a proactive report to the primary native 업무 chat
// (client:main). Thin wrapper over relayNativeTo for the callers that always
// target the main session.
func (d proactiveRelayDeps) relayNative(content string) (bool, error) {
	return d.relayNativeTo(nativeWorkSessionKey, content)
}

// relayNativeTo delivers a proactive report to a specific native session: it
// appends the body to that session's transcript so the app shows it, live-pushes
// a one-line preview, and — for the main 업무 feed (client:main) only — raises a
// work-feed card. sessionKey defaults to client:main when empty. Returns
// (false, nil) when no transcript store is wired (older wiring or tests).
func (d proactiveRelayDeps) relayNativeTo(sessionKey, content string) (bool, error) {
	return d.relayNativeToOpts(sessionKey, content, false)
}

// relayNativeToOpts is relayNativeTo with the collapse switch: when collapse is
// true the transcript message is wrapped as a collapsed deneb-ui accordion
// (title-only card, tap to expand) while every other consumer of the body —
// suppression gates, work-feed card extraction, push/sync previews — keeps
// reading the raw prose.
func (d proactiveRelayDeps) relayNativeToOpts(sessionKey, content string, collapse bool) (bool, error) {
	target := sessionKey
	if target == "" {
		target = nativeWorkSessionKey
	}
	// Respect the NO_REPLY silent-reply contract: a proactive turn that correctly
	// signals "nothing to report" with the token (alone or trailing) must be
	// suppressed — not delivered as a literal "NO_REPLY" work-feed card + push.
	// isContentlessProactive below only catches the chatty prose variants
	// ("메일이 없습니다" etc.); the bare token needs this explicit strip.
	origLen := len(content) // raw body size, recorded on every relay decision
	if content = chat.StripSilentToken(content); strings.TrimSpace(content) == "" {
		d.logProactive("suppressed", "silent_token", origLen, "")
		return false, nil
	}
	// Drop the model's leading working-narration preamble — "전체 맥락 파악됐습니다.
	// 분석 결과 정리합니다." (then "---" then the actual report) — before it reaches
	// the work-feed card title/summary, the client:main transcript, or the push.
	// A cron/morning-letter turn sometimes opens its terminal (no-tool) turn with
	// this meta sentence about its own process; because it sits atop a single
	// terminal turn, the per-turn isInterimNarration filter (tool-count based)
	// never sees it. See stripProactiveMetaPreamble.
	content = stripProactiveMetaPreamble(content)
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
		d.logProactive("suppressed", "contentless", origLen, pushPreview(content))
		return false, nil
	}

	// The main 업무 feed (client:main) delivers proactive reports to the work FEED
	// only — not the chat transcript — so the chat stays a place to ask, not a wall
	// of pushed reports. The feed card carries the full body, read in the 피드 screen
	// (PR #2448). Sub-sessions (e.g. a dream side-thread) and the no-feed-store
	// fallback still mirror into their transcript so nothing is silently dropped.
	feedOnly := target == nativeWorkSessionKey && d.workFeed != nil

	if !feedOnly {
		if d.transcriptStore == nil {
			// No transcript store wired means every proactive report (morning
			// letter, mail analysis) is silently dropped in native-only mode — the
			// user observes nothing arriving. Surface it so a misconfigured startup
			// is diagnosable instead of mysteriously quiet.
			if d.logger != nil {
				d.logger.Error("proactive native relay: no transcript store wired — report dropped",
					"sessionKey", target)
			}
			d.logProactive("dropped", "no_transcript_store", origLen, "")
			return false, nil
		}
		// Collapsed delivery: the transcript carries a title-only accordion card
		// that expands in place; the raw prose stays inside its markdown child, so
		// follow-up turns in client:main still have the full analysis in context.
		// A body whose title can't be derived falls back to plain prose delivery.
		transcriptBody := content
		if collapse {
			if title, titleLine := extractCardTitle(content); strings.TrimSpace(title) != "" {
				transcriptBody = denebui.CollapsedReportFence(title, collapsedReportBody(content, title, titleLine))
			}
		}
		msg := toolctx.NewTextChatMessage("assistant", transcriptBody, time.Now().UnixMilli())
		if err := d.transcriptStore.Append(target, msg); err != nil {
			if d.logger != nil {
				d.logger.Error("proactive native relay: transcript append failed",
					"sessionKey", target, "error", err)
			}
			d.logProactive("error", "append_failed", origLen, "")
			return false, err
		}
		d.markSessionVisible(target, msg.Timestamp)
		if d.nativeSync != nil {
			if _, err := d.nativeSync.Append(nativesync.TranscriptAppended(
				target,
				"assistant",
				pushPreview(content),
				msg.Timestamp,
			)); err != nil && d.logger != nil {
				d.logger.Error("proactive native relay: native sync append failed",
					"sessionKey", target, "error", err)
			}
		}
	}
	if d.workFeed != nil && target == nativeWorkSessionKey {
		// Derive a human title + summary from the body rather than the fixed
		// "업무 리포트" + first-line slice that leaked markdown markers ("### …",
		// "---") into every card. An empty title falls back to the store's
		// defaultTitle ("업무 리포트"). See workfeed_extract.go.
		title, titleLine := extractCardTitle(content)
		// Mail reports get the envelope card icon (SourceMailReport) and a better
		// title: the analysis model writes a generic "메일 분석 리포트" heading, so the
		// lightweight model names it from the email's real subject (cardTitler); the
		// deterministic extractCardTitle subject is the fallback.
		isMail := isMailReportBody(content)
		source := workfeed.SourceProactive
		if isMail {
			source = workfeed.SourceMailReport
			if d.cardTitler != nil {
				if t := d.cardTitler(content); t != "" {
					title = t
				}
			}
		}
		if _, err := d.workFeed.Append(workfeed.Item{
			Source:     source,
			Title:      title,
			Summary:    extractCardSummary(content, titleLine),
			Body:       content,
			SessionKey: target,
		}); err != nil && d.logger != nil {
			d.logger.Error("proactive native relay: work feed append failed",
				"sessionKey", target, "error", err)
		}
	}
	if d.pushHub != nil {
		d.pushHub.publish(clientPushEvent{
			Title: "Deneb",
			Body:  pushPreview(content),
		})
		// Fallback: when no client holds a live SSE connection, the frame above
		// reached nobody (app fully closed or in Android Doze). Push via FCM so
		// closed devices still raise the notification. Nil-safe (dormant until
		// credentials are configured) and skipped while a client is connected —
		// the live frame already delivered. Fire-and-forget; the report is in the
		// 업무 feed (or, for feed-less sessions, the transcript) regardless.
		if d.pushFCM != nil && d.pushHub.subscriberCount() == 0 {
			d.pushFCM.DeliverFallback("Deneb", pushPreview(content))
		}
	}
	d.logProactive("delivered", "", origLen, pushPreview(content))
	return true, nil
}

// collapsedReportBody returns content with its leading title line removed when
// that exact line became the accordion title — otherwise the expanded card
// would open by repeating its own header as the first heading. Folded titles
// ("분석 — 왜 지금 왔는가", where titleLine is the sub-heading) and clipped titles
// don't match the stripped line, so those bodies stay intact.
func collapsedReportBody(content, title, titleLine string) string {
	if stripMarkdownLine(titleLine) != title {
		return content
	}
	lines := strings.Split(content, "\n")
	want := strings.TrimSpace(titleLine)
	for i, l := range lines {
		if strings.TrimSpace(l) != want {
			continue
		}
		rest := append(append([]string{}, lines[:i]...), lines[i+1:]...)
		// Drop leading blanks and now-orphaned horizontal rules so the
		// expanded card doesn't open with a stray divider.
		start := 0
		for start < len(rest) {
			if t := strings.TrimSpace(rest[start]); t == "" || isHorizontalRule(t) {
				start++
				continue
			}
			break
		}
		return strings.Join(rest[start:], "\n")
	}
	return content
}

// logProactive records one relay decision to the behavioral event log so the
// proactive funnel (fire → suppress / deliver) is queryable after the fact.
// nil-safe via Writer.LogEvent.
func (d proactiveRelayDeps) logProactive(decision, reason string, contentLen int, preview string) {
	d.behaviorLog.LogEvent(agentlog.SessionProactive, agentlog.TypeProactiveRelay, agentlog.ProactiveRelayData{
		Decision:   decision,
		Reason:     reason,
		ContentLen: contentLen,
		Preview:    preview,
	})
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
// Conservative by design. A single-line body is matched on its raw text (≤120
// rune). A multi-line body is reduced to its substantive text (markdown
// markers, emoji, and whitespace removed) — so a "변경 없음" wrapped in headers
// and blank lines is still caught — but only when that substance is short
// (≤contentlessSubstanceMaxRunes). A genuine multi-section report has long
// substance and is left untouched even if it contains "없음" somewhere (e.g.
// "긴급 메일 없음, 단 X 확인 필요" inside a brief).
func isContentlessProactive(content string) bool {
	s := strings.TrimSpace(content)
	if s == "" {
		return true
	}
	if !strings.Contains(s, "\n") {
		if len([]rune(s)) > 120 {
			return false
		}
		return containsContentlessFragment(s, false)
	}
	body := substantiveText(s)
	if len([]rune(body)) > contentlessSubstanceMaxRunes {
		return false
	}
	return containsContentlessFragment(body, true)
}

// containsContentlessFragment reports whether s contains any "nothing to report"
// fragment. With collapsed, fragments are compared with their spaces removed —
// substantiveText drops whitespace, so "변경 없음" must match as "변경없음".
func containsContentlessFragment(s string, collapsed bool) bool {
	for _, frag := range contentlessProactiveFragments {
		if collapsed {
			frag = strings.ReplaceAll(frag, " ", "")
		}
		if strings.Contains(s, frag) {
			return true
		}
	}
	return false
}

// metaPreambleMaxRunes bounds how long a leading paragraph may be and still count
// as throwaway working-narration. Observed leaks are all under ~50 runes; a real
// report lede that opens on the subject runs longer. The signal match below is
// the primary discriminator — this is a secondary guard so an unusually long
// sentence that happens to contain a signal word is never mistaken for narration.
const metaPreambleMaxRunes = 100

// metaPreambleMinRemainderRunes is the minimum report content that must survive a
// strip. Below this the original is kept untouched, so a body that is *only* a
// short status line is never reduced to a near-empty card.
const metaPreambleMinRemainderRunes = 30

// metaPreambleSignals mark a leading paragraph as the model narrating its own
// process — gathering context, finishing analysis, starting to write, detecting a
// trigger, or framing the deliverable — rather than reporting. Matched only
// against a short leading paragraph that real content follows (see
// stripProactiveMetaPreamble), so a sentence containing one of these mid-report
// is never touched.
var metaPreambleSignals = []string{
	// 맥락/정보 수집 단계 서술
	"맥락을 확보", "맥락 확보", "맥락을 파악", "맥락 파악", "맥락이 파악",
	"충분한 맥락", "충분한 정보", "전체 맥락",
	"파악됐습니다", "파악했습니다", "파악 완료",
	// 분석/정리/작성/수집/업데이트 완료·전환 서술
	"분석 완료", "분석을 완료", "분석이 완료", "분석 결과 정리", "분석 결과를 정리",
	"정리합니다", "정리하겠습니다", "정리해서 보고", "정리할게요", "정리했습니다",
	"작성한다", "작성합니다", "작성하겠습니다", "작성할게요",
	"수집 완료", "수집했습니다",
	"업데이트까지 끝", "업데이트 완료",
	// 보고 행위 자기언급
	"보고드릴게요", "보고드리겠습니다", "보고하겠습니다", "보고할게요",
	// 트리거 감지 서술 (실시간 메일 분석)
	"도착 감지", "감지됐", "감지되어", "감지했",
	// 산출물 자기언급
	"발송 내용입니다", "보낼 내용입니다", "전달할 내용입니다", "작성한 내용입니다",
}

// metaPreambleFillerPrefixes mark an AI-filler opener some proactive turns
// prepend before the real report. A proactive report has no user to acknowledge,
// so any of these atop a card is throwaway. Matched as a prefix of a short
// leading paragraph only.
var metaPreambleFillerPrefixes = []string{
	"좋아요", "좋습니다", "알겠습니다", "알겠어요", "물론입니다", "물론이죠",
	"네, ", "네,", "넵 ", "넵,", "그럼 ", "자, ",
}

// stripProactiveMetaPreamble removes a leading working-narration paragraph (and
// an immediately following horizontal-rule divider) from a proactive body.
//
// A model composing a cron/morning-letter report sometimes opens its final turn
// with a meta sentence about its own process — "전체 맥락 파악됐습니다. 분석 결과
// 정리합니다." then "---" then the actual report — which then leaks verbatim into
// the 업무 리포트 card title, summary, and the client:main transcript. That preamble
// sits atop a single terminal (no-tool) turn, so the per-turn isInterimNarration
// filter cannot catch it; this post-process strip does.
//
// Conservative by design: it removes only the FIRST paragraph, and only when that
// paragraph (1) opens with a letter/digit — not an emoji/markdown header marker,
// so titles like "📬 메일 분석 보고" and "## 분석" are exempt — (2) is short, (3)
// matches a meta/filler signal, and (4) leaves substantial report content behind.
// A greeting ("...아침입니다 🐾") and direct subject analysis ("이 이메일은 ...")
// match no signal and pass through unchanged.
func stripProactiveMetaPreamble(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return content
	}
	first, rest, found := splitFirstParagraph(trimmed)
	if !found || !isMetaPreambleParagraph(first) {
		return content
	}
	rest = strings.TrimSpace(rest)
	// A divider ("---", "━━━…") often separates the preamble from the body — drop
	// a leading divider paragraph so the card does not open on a bare rule.
	if next, after, ok := splitFirstParagraph(rest); ok && isDividerLine(next) {
		rest = strings.TrimSpace(after)
	} else if isDividerLine(rest) {
		// rest is only a divider: stripping leaves no body, so keep the original.
		return content
	}
	if utf8.RuneCountInString(rest) < metaPreambleMinRemainderRunes {
		return content
	}
	return rest
}

// isMetaPreambleParagraph reports whether a leading paragraph is throwaway
// working narration rather than report content. See stripProactiveMetaPreamble
// for the guarantees this upholds.
func isMetaPreambleParagraph(para string) bool {
	p := strings.TrimSpace(para)
	if p == "" || utf8.RuneCountInString(p) > metaPreambleMaxRunes {
		return false
	}
	// A line that opens with anything other than a letter or digit is structural —
	// a markdown heading (#, >, -, *, |), a bold title (**…**), a divider, or an
	// emoji-led header (📬/📋/📊/☀️). Real report ledes and headers live here;
	// throwaway narration is always prose, opening on a Hangul/Latin word.
	r, _ := utf8.DecodeRuneInString(p)
	if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
		return false
	}
	for _, pre := range metaPreambleFillerPrefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	for _, sig := range metaPreambleSignals {
		if strings.Contains(p, sig) {
			return true
		}
	}
	return false
}

// splitFirstParagraph splits text at the first blank line into (first, rest).
// found is false when there is no blank line (a single paragraph), in which case
// first == text and rest == "". Callers trim the parts as needed.
func splitFirstParagraph(text string) (first, rest string, found bool) {
	lines := strings.Split(text, "\n")
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			return strings.Join(lines[:i], "\n"), strings.Join(lines[i+1:], "\n"), true
		}
	}
	return text, "", false
}

// isDividerLine reports whether s is a horizontal-rule divider — markdown
// ("---", "***", "___") or a unicode box-drawing rule ("━━━…", "─────", "═══").
// Requires at least 3 rule runes so a short word is never mistaken for a divider.
func isDividerLine(s string) bool {
	t := strings.TrimSpace(s)
	if utf8.RuneCountInString(t) < 3 {
		return false
	}
	for _, r := range t {
		switch r {
		case '-', '*', '_', '=', '—', '–', '━', '─', '═', ' ':
		default:
			return false
		}
	}
	return true
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
	d.markSessionVisible(nativeWorkSessionKey, msg.Timestamp)
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

// dreamWorkSessionKey is a dedicated client:main sub-session for Aurora Dream
// lifecycle notifications, keeping memory-consolidation status (often "변경 없음"
// or diagnostics) out of the primary 업무 feed while remaining a restorable native
// session the user can open. See isRestorableNativeSessionKey (client:main:<id>).
const dreamWorkSessionKey = nativeWorkSessionKey + ":dream"

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
func (n *relayNotifier) Notify(_ context.Context, message string) error {
	_, err := n.deps.relayNativeTo(n.sessionKey, message)
	return err
}

// notifierForSession binds the relay to a session key and returns a Notifier
// ready to plug into autonomous.Service or gmailpoll.Service. Always returns a
// non-nil notifier because the native relay requires only a transcript store,
// not a Telegram plugin.
func (d proactiveRelayDeps) notifierForSession(sessionKey string) *relayNotifier {
	return &relayNotifier{deps: d, sessionKey: sessionKey}
}
