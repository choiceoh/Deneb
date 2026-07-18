package proactive

import (
	"context"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	runtimesession "github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive/textprep"
)

// contentlessProactiveFragments mark a proactive body as carrying nothing
// actionable: an email-check turn that found no mail, a dreaming cycle with no
// changes, or an analysis stub. Matched only against short single-line bodies
// (see isContentlessProactive) so a real multi-section report that merely
// mentions one of these is never affected.
var contentlessProactiveFragments = []string{
	"분석 실패",    // mail batch-analyze stub "(분석 실패)"
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

// markupFragmentRe matches a body that is just a stray HTML/tool tag the model
// emitted instead of a report ("<pre>", "<tool>", "<function=…>", "</thinking>").
var markupFragmentRe = regexp.MustCompile(`^\s*</?(pre|code|tool|thinking|function|div|span|p|details|summary)\b[^>]*>?\s*$`)

// bareProactiveLabels are generic label strings that, alone, carry no report — a
// card body that is only one of these is the model emitting a heading word with no
// content. Compared against substantiveText (spaces/emoji stripped, so "메일 분석"
// is matched as "메일분석").
var bareProactiveLabels = map[string]bool{
	"분석": true, "메일분석": true, "이메일분석": true, "분석요약": true,
	"분석보고": true, "메일분석보고": true, "메일분석리포트": true,
	"업무리포트": true, "모델튜너분석": true, "분석결과": true,
}

// hasReportStructure reports whether any line is markdown report structure (a
// heading, table row, or list item) — a strong signal the body is a real report,
// not a one-paragraph self-narration.
func hasReportStructure(s string) bool {
	for _, raw := range strings.Split(s, "\n") {
		t := strings.TrimSpace(raw)
		if wfHeaderRe.MatchString(t) || wfBulletRe.MatchString(t) ||
			wfOrderedRe.MatchString(t) || strings.HasPrefix(t, "|") {
			return true
		}
	}
	return false
}

// containsMetaPreambleSignal reports whether s contains any working-narration
// signal phrase (the set stripProactiveMetaPreamble peels by).
func containsMetaPreambleSignal(s string) bool {
	for _, sig := range metaPreambleSignals {
		if strings.Contains(s, sig) {
			return true
		}
	}
	return false
}

// ordinalIndexRe matches a Korean ordinal/index run — one or more digits
// (optionally comma/space-separated, e.g. "9, 10") ending in the position marker
// "번". These are references the narration itself makes to item positions ("메일
// 9, 10번을 확인합니다"), NOT factual tells like amounts (3억), dates (6/25), or
// counts (3건), which carry other unit markers and must stay treated as content.
var ordinalIndexRe = regexp.MustCompile(`\d[\d,\s]*번`)

// stripOrdinalIndices removes "N번"-style position references so the digit-based
// factual-tell test in isNarrationOnlyProactive (and isNarrationSentenceLine's
// executor twin) sees a bare next-step announcement for what it is.
func stripOrdinalIndices(s string) string {
	return ordinalIndexRe.ReplaceAllString(s, "")
}

// isNarrationOnlyProactive reports whether a proactive body is the model's own
// working narration, a stray markup/tool fragment, or a bare generic label — with
// no actual report. These leak as 업무 리포트 cards when a terminal turn emits
// self-talk ("이제 분석 보고를 정리해.", "<tool>", "분석") instead of, or before, a
// report. Distinct from isContentlessProactive ("nothing to report" pings): this is
// "the model narrated its process instead of reporting". Conservative — any report
// structure, real length, or a factual tell (a digit) keeps the card.
func isNarrationOnlyProactive(content string) bool {
	s := strings.TrimSpace(content)
	if s == "" {
		return true
	}
	if hasReportStructure(s) {
		return false
	}
	if markupFragmentRe.MatchString(s) {
		return true
	}
	body := substantiveText(s)
	n := len([]rune(body))
	if n == 0 {
		// Structure stripped to nothing (e.g. only "---") — not narration; leave it to
		// the existing markers-only delivery behavior (empty title → store default).
		return false
	}
	if n > contentlessSubstanceMaxRunes {
		return false // long enough to be real content
	}
	if bareProactiveLabels[body] {
		return true
	}
	// A short, structureless paragraph with a narration signal and no factual tell
	// (a digit) is the model talking about its process, not reporting. Index
	// references the narration makes to item positions — "메일 9, 10번을 확인합니다" —
	// are not factual tells, so strip "N번" ordinals before the digit test; a real
	// report's tells (amounts 3억, dates 6/25, counts 3건) survive unstripped.
	return !strings.ContainsAny(stripOrdinalIndices(body), "0123456789") && containsMetaPreambleSignal(s)
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
	"정리합니다", "정리하겠습니다", "정리해서 보고", "정리할게요", "정리했습니다", "정리해.", "정리한다",
	"작성한다", "작성합니다", "작성하겠습니다", "작성할게요", "작성할게",
	"수집 완료", "수집했습니다",
	"업데이트까지 끝", "업데이트 완료",
	"준비 완료", "준비 완료했",
	"다 모였",
	// 다음 단계로 넘어가는 자기서술 (작업 narration)
	"확인해볼게", "확인해 볼게", "확인할게", "읽어오겠습니다", "읽어올게", "읽어오겠",
	"도구를 활성화", "도구를 켜", "활성화하고",
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

// appendWorkModelFooter returns body with the producing model's name on its own
// line at the very end — the bare name, no label or emoji. The trailing position
// keeps it clear of the work-feed card's title/summary extraction (which read the
// head) and the push preview (first line only).
func appendWorkModelFooter(body, model string) string {
	return strings.TrimRight(body, "\n") + "\n\n" + model
}

// pushPreview trims a relayed body to a notification-sized single line. The full
// report is in the transcript; the push is just the nudge to open it.
func pushPreview(content string) string {
	// A body that opens with a deneb-ui fence would preview as "```deneb-ui" —
	// project cards to their prose first, so the notification carries the
	// card's own headline instead of markup. deneb-html documents strip to a
	// short marker for the same reason (raw HTML must never reach a preview).
	s := strings.TrimSpace(textprep.ReplaceFences(textprep.StripHTMLAnswers(content), textprep.PlainText))
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
const nativeWorkSessionKey = runtimesession.NativeWorkSessionKey

// NativeWorkSessionKey is the primary native work conversation.
const NativeWorkSessionKey = nativeWorkSessionKey

// dreamWorkSessionKey is a dedicated client:main sub-session for Aurora Dream
// lifecycle notifications, keeping memory-consolidation status (often "변경 없음"
// or diagnostics) out of the primary 업무 feed while remaining a restorable native
// session the user can open. See isRestorableNativeSessionKey (client:main:<id>).
const dreamWorkSessionKey = runtimesession.DreamWorkSessionKey

// DreamWorkSessionKey is the dedicated background-dream conversation.
const DreamWorkSessionKey = dreamWorkSessionKey

// nativeWorkSessionKeyTo is the "to" half of nativeWorkSessionKey. Used as the
// cron DefaultTo so a job without an explicit delivery target resolves to a
// valid recipient — the handoff rebuilds "client:" + "main" = client:main and
// the relay routes it to the native 업무 chat.
const nativeWorkSessionKeyTo = runtimesession.NativeWorkSessionTarget

// NativeWorkSessionTarget is the channel target portion of client:main.
const NativeWorkSessionTarget = nativeWorkSessionKeyTo

// relayNotifier adapts proactiveRelayDeps to the Notifier interface used by
// both the autonomous service (wiki dreaming) and mailanalysis. It binds a session
// key at construction so Notify(ctx, message) delivers there.
type relayNotifier struct {
	deps       proactiveRelayDeps
	sessionKey string
	opts       proactiveRelayOptions
}

// Notify satisfies autonomous.Notifier and mailanalysis.Notifier. Returns the
// underlying send error; delivery-not-wired (relay returns false with no error)
// is treated as a silent no-op.
func (n *relayNotifier) Notify(_ context.Context, message string) error {
	_, err := n.deps.relayNativeToOptions(n.sessionKey, message, n.opts)
	return err
}

// notifierForSession binds the relay to a session key and returns a Notifier
// ready to plug into autonomous.Service or mailanalysis.Service. Always returns a
// non-nil notifier because the native relay requires only a transcript store,
// not a Telegram plugin.
func (d proactiveRelayDeps) notifierForSession(sessionKey string) *relayNotifier {
	return &relayNotifier{deps: d, sessionKey: sessionKey}
}

// NotifierForSession adapts the relay to autonomous.Notifier for a session.
func (d proactiveRelayDeps) NotifierForSession(sessionKey string) *relayNotifier {
	return d.notifierForSession(sessionKey)
}

// mailNotifierForSession is the same relay binding, but source-tags the work-feed
// item as a mail report. This keeps the envelope card icon and LLM title/summary
// path stable even when the editable mail-analysis prompt opens with the actual
// subject instead of a generic "메일 분석 리포트" heading.
func (d proactiveRelayDeps) mailNotifierForSession(sessionKey string) *relayNotifier {
	return &relayNotifier{
		deps:       d,
		sessionKey: sessionKey,
		opts: proactiveRelayOptions{
			workFeedSource: workfeed.SourceMailReport,
		},
	}
}

// MailNotifierForSession adapts the relay with mail-report work-feed metadata.
func (d proactiveRelayDeps) MailNotifierForSession(sessionKey string) *relayNotifier {
	return d.mailNotifierForSession(sessionKey)
}
