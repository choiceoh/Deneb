package proactive

import (
	"context"
	"encoding/base64"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/push"
	runtimesession "github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativepush"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive/relaylog"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive/textprep"
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

	// weakTitleMinRunes — a NON-heading title at or above this length is almost
	// certainly a whole narration sentence the heuristic grabbed (a proactive body
	// that opens with prose, not a heading), so the lightweight model names it.
	weakTitleMinRunes = 22

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
	transcriptStore toolport.TranscriptStore
	logger          interface { // *slog.Logger subset
		Warn(string, ...any)
		Error(string, ...any)
	}

	// behaviorLog records each relay decision (delivered/suppressed/dropped/
	// error) under system:proactive so the autonomous-delivery funnel is
	// observable: how often it fires and how much is suppressed and why. nil-safe;
	// nil in older wiring/tests.
	behaviorLog *relaylog.Writer

	// pushHub fans a {title, body} frame out to connected native clients when a
	// report arrives, so the app raises a notification live instead of waiting
	// for its next heartbeat poll. nil in older wiring/tests; the push is then
	// skipped (the report still lands in the transcript).
	pushHub *nativepush.Hub

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
	// lightweight model — the 📬 icon already says "mail", so the card title should be
	// the email's subject, not the generic "메일 분석 리포트" heading the main-role synthesis model
	// writes — and returns the card's 2-line summary from the same call. Best-effort:
	// returns ("", "") on any failure, and the deterministic extractCardTitle /
	// extractCardSummary heuristics are the fallbacks (independently — a non-empty
	// title still applies when the summary comes back ""). nil in older wiring/tests
	// (then the heuristics are used directly).
	cardTitler func(content string) (title, summary string)

	// workModel resolves the display name of the model that produced a proactive
	// 업무 feed report, so each feed card can show which model did the work. The
	// main feed's LLM content is uniformly a main-model agent turn (cron morning
	// letter, mail-analysis synthesis, heartbeat, goal, event ingest), so this
	// returns the main role's model. nil-safe (nil in older wiring/tests) and a
	// "" return both leave the body untouched — no footer is appended.
	workModel func() string

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

// Relay is the native proactive-delivery service used by the gateway
// composition root. Its dependencies are supplied through NewRelay.
type Relay = proactiveRelayDeps

// Deps contains the storage, push, observability, and presentation boundaries
// required by Relay. Every optional boundary degrades independently.
type Deps struct {
	TranscriptStore toolport.TranscriptStore
	Logger          interface {
		Warn(string, ...any)
		Error(string, ...any)
	}
	BehaviorLog *relaylog.Writer
	PushHub     *nativepush.Hub
	PushFCM     *push.Notifier
	WorkFeed    interface {
		Append(workfeed.Item) (workfeed.Item, error)
	}
	CardTitler func(content string) (title, summary string)
	WorkModel  func() string
	NativeSync interface {
		Append(nativesync.AppendInput) (nativesync.Event, error)
	}
	Sessions interface {
		EnsureVisible(key, channel string, at int64)
	}
}

// NewRelay constructs a proactive-delivery service from explicit boundaries.
func NewRelay(deps Deps) Relay {
	return proactiveRelayDeps{
		transcriptStore: deps.TranscriptStore,
		logger:          deps.Logger,
		behaviorLog:     deps.BehaviorLog,
		pushHub:         deps.PushHub,
		pushFCM:         deps.PushFCM,
		workFeed:        deps.WorkFeed,
		cardTitler:      deps.CardTitler,
		workModel:       deps.WorkModel,
		nativeSync:      deps.NativeSync,
		sessions:        deps.Sessions,
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
	channel, ok := runtimesession.RestorableTranscriptChannel(sessionKey)
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

type proactiveRelayOptions struct {
	collapse       bool
	workFeedSource string
	// mirrorTranscript also appends the body to the client:main transcript
	// even when the work feed would normally take the delivery alone
	// (feed-only doctrine). Question producers (meeting harvest) need this:
	// miniapp.workfeed.answer routes only the user's typed answer back as the
	// next main-session prompt, so unless the question itself is mirrored
	// into the transcript, the agent receives "승인됐어요" with no clue which
	// meeting/project it refers to and cannot file the outcome.
	mirrorTranscript bool
	refID            string
	forceQuestion    bool
	actions          []workfeed.Action
	// skipContentlessSuppression bypasses the "nothing to report" fragment
	// filter. Heartbeat delivery uses the NO_REPLY contract instead; mixed
	// single-line reports must not be dropped because they mention "알림 없음".
	skipContentlessSuppression bool
}

// Options carries source and transcript-mirroring metadata for an individual
// proactive delivery.
type Options struct {
	WorkFeedSource             string
	MirrorTranscript           bool
	RefID                      string
	ForceQuestion              bool
	Actions                    []workfeed.Action
	SkipContentlessSuppression bool
}

type preparedProactiveDelivery struct {
	target         string
	content        string
	body           string
	originalLength int
}

func (d proactiveRelayDeps) prepareProactiveDelivery(sessionKey, content string, opts proactiveRelayOptions) (preparedProactiveDelivery, bool) {
	target := sessionKey
	if target == "" {
		target = nativeWorkSessionKey
	}
	originalLength := len(content)
	content = textprep.StripSilentReply(content)
	if strings.TrimSpace(content) == "" {
		d.logProactive("suppressed", "silent_token", originalLength, "")
		return preparedProactiveDelivery{}, false
	}

	content = stripProactiveMetaPreamble(textprep.SubstituteLetterTokens(content))
	// Producer-authored card bodies bypass NormalizeFinalReply, so fence
	// emission glitches (split "```"+"deneb-ui" openers, mid-card restarts)
	// would land in the work feed as raw markup. Repair them here — the single
	// choke point every consumer (transcript, feed card, push preview) reads.
	if repairedContent, glitched := textprep.RepairFenceGlitches(content); glitched {
		if d.logger != nil {
			d.logger.Warn("proactive relay: deneb-ui fence glitch repaired", "sessionKey", target)
		}
		content = repairedContent
	}
	if !opts.skipContentlessSuppression && isContentlessProactive(content) {
		d.logProactive("suppressed", "contentless", originalLength, pushPreview(content))
		return preparedProactiveDelivery{}, false
	}
	if isNarrationOnlyProactive(content) {
		d.logProactive("suppressed", "narration", originalLength, pushPreview(content))
		return preparedProactiveDelivery{}, false
	}
	if isSelfImprovementDiagnostic(content) {
		d.logProactive("suppressed", "self_improvement_diagnostic", originalLength, pushPreview(content))
		return preparedProactiveDelivery{}, false
	}

	body := content
	if target == nativeWorkSessionKey && d.workModel != nil {
		if model := strings.TrimSpace(d.workModel()); model != "" {
			body = appendWorkModelFooter(content, model)
		}
	}
	return preparedProactiveDelivery{
		target: target, content: content, body: body, originalLength: originalLength,
	}, true
}

// relayNativeToOpts is relayNativeTo with the collapse switch: when collapse is
// true the transcript message is wrapped as a collapsed deneb-ui accordion
// (title-only card, tap to expand) while every other consumer of the body —
// suppression gates, work-feed card extraction, push/sync previews — keeps
// reading the raw prose.
func (d proactiveRelayDeps) relayNativeToOpts(sessionKey, content string, collapse bool) (bool, error) {
	return d.relayNativeToOptions(sessionKey, content, proactiveRelayOptions{collapse: collapse})
}

// relayNativeToOptions is relayNativeTo with delivery metadata. workFeedSource
// lets source-aware producers (notably Gmail/LMTP mail analysis) preserve their
// card type even when the user-edited prompt no longer starts with a generic
// "메일 분석 리포트" heading.
func (d proactiveRelayDeps) relayNativeToOptions(sessionKey, content string, opts proactiveRelayOptions) (bool, error) {
	delivery, ok := d.prepareProactiveDelivery(sessionKey, content, opts)
	if !ok {
		return false, nil
	}
	target, content, deliverBody := delivery.target, delivery.content, delivery.body
	origLen := delivery.originalLength

	// The main 업무 feed (client:main) delivers proactive reports to the work FEED
	// only — not the chat transcript — so the chat stays a place to ask, not a wall
	// of pushed reports. The feed card carries the full body, read in the 피드 screen
	// (PR #2448). Sub-sessions (e.g. a dream side-thread) and the no-feed-store
	// fallback still mirror into their transcript so nothing is silently dropped.
	feedOnly := target == nativeWorkSessionKey && d.workFeed != nil && !opts.mirrorTranscript

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
		transcriptBody := deliverBody
		// A producer-authored deneb-ui card is already the compact presentation —
		// wrapping it in the accordion would backtick-escape the fence into
		// literal text. Deliver card-first bodies as-is.
		if opts.collapse && !startsWithDenebUIFence(deliverBody) {
			if title, titleLine := extractCardTitle(content); strings.TrimSpace(title) != "" {
				transcriptBody = textprep.CollapsedReportFence(title, collapsedReportBody(deliverBody, title, titleLine))
			}
		}
		msg := toolport.NewTextChatMessage("assistant", transcriptBody, time.Now().UnixMilli())
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
	pushKind, pushRef := d.appendProactiveWorkFeed(target, content, deliverBody, opts)
	if d.pushHub != nil {
		d.pushHub.Publish(nativepush.Event{
			Title: "Deneb",
			Body:  pushPreview(content),
			Kind:  pushKind,
			Ref:   pushRef,
		})
		// Fallback: when no client holds a live SSE connection, the frame above
		// reached nobody (app fully closed or in Android Doze). Push via FCM so
		// closed devices still raise the notification. Nil-safe (dormant until
		// credentials are configured) and skipped while a client is connected —
		// the live frame already delivered. Fire-and-forget; the report is in the
		// 업무 feed (or, for feed-less sessions, the transcript) regardless.
		if d.pushFCM != nil && d.pushHub.MobileSubscriberCount() == 0 {
			d.pushFCM.DeliverFallback("Deneb", pushPreview(content))
		}
	}
	d.logProactive("delivered", "", origLen, pushPreview(content))
	return true, nil
}

// appendProactiveWorkFeed records a main-session delivery as a work-feed card
// and returns the deep-link metadata for the corresponding native push. The
// transcript remains the delivery source of truth, so feed failures are logged
// and deliberately do not fail the relay.
func (d proactiveRelayDeps) appendProactiveWorkFeed(
	target, content, deliverBody string,
	opts proactiveRelayOptions,
) (pushKind, pushRef string) {
	if d.workFeed == nil || target != nativeWorkSessionKey {
		return "", ""
	}

	cardSrc, choices := splitChoicesFence(content)
	cardBody, _ := splitChoicesFence(deliverBody)
	// Every text heuristic (and the LLM titler) reads the card's PROSE: a body
	// that is mostly a deneb-ui fence is otherwise invisible to the
	// fence-skipping scans — the title fell back to the generic "업무 리포트"
	// and the titler echoed "```deneb-ui" as the summary (2026-07-12 live feed).
	extractSrc := textprep.ReplaceFences(textprep.StripHTMLAnswers(cardSrc), textprep.PlainText)
	title, titleLine := extractCardTitle(extractSrc)
	source := strings.TrimSpace(opts.workFeedSource)
	if source == "" {
		source = workfeed.SourceProactive
	}
	isMail := source == workfeed.SourceMailReport || isMailReportBody(extractSrc)
	if isMail {
		source = workfeed.SourceMailReport
	}

	summary := extractCardSummary(extractSrc, titleLine)
	if d.cardTitler != nil && (isMail || isWeakCardTitle(title, titleLine)) {
		if modelTitle, modelSummary := d.cardTitler(extractSrc); modelTitle != "" || modelSummary != "" {
			if modelTitle != "" {
				title = modelTitle
			}
			if modelSummary != "" {
				summary = modelSummary
			}
		}
	}

	appended, err := d.workFeed.Append(workfeed.Item{
		Source:     source,
		Title:      title,
		Summary:    summary,
		Body:       cardBody,
		SessionKey: target,
		RefID:      strings.TrimSpace(opts.refID),
		Question:   opts.forceQuestion || len(choices) > 0 || endsWithQuestionMark(extractSrc),
		Actions:    append(coalesceActions(opts.actions, choiceAnswerActions(choices)), deadlineMarkActions(cardBody)...),
	})
	if err != nil {
		if d.logger != nil {
			d.logger.Error("proactive native relay: work feed append failed",
				"sessionKey", target, "error", err)
		}
		return "", ""
	}
	return nativepush.PushKindWorkfeed, appended.ID
}

// Relay delivers content to the native work session.
func (d proactiveRelayDeps) Relay(ctx context.Context, sessionKey, content string) (bool, error) {
	return d.relay(ctx, sessionKey, content)
}

// RelayCollapsed delivers content as a collapsed transcript report.
func (d proactiveRelayDeps) RelayCollapsed(ctx context.Context, sessionKey, content string) (bool, error) {
	return d.relayCollapsed(ctx, sessionKey, content)
}

// RelayNative delivers content to the default native work session.
func (d proactiveRelayDeps) RelayNative(content string) (bool, error) {
	return d.relayNative(content)
}

// RelayNativeToOptions delivers content with explicit work-feed metadata.
func (d proactiveRelayDeps) RelayNativeToOptions(sessionKey, content string, opts Options) (bool, error) {
	return d.relayNativeToOptions(sessionKey, content, proactiveRelayOptions{
		workFeedSource:             opts.WorkFeedSource,
		mirrorTranscript:           opts.MirrorTranscript,
		refID:                      opts.RefID,
		forceQuestion:              opts.ForceQuestion,
		actions:                    opts.Actions,
		skipContentlessSuppression: opts.SkipContentlessSuppression,
	})
}

// minDeliverableRunes is the substance floor for an auto-published deliverable
// card. A document-analysis turn that produced only a short answer (e.g. "3
// 페이지네요") is below the bar. Structured responses (headings/tables/bullets)
// pass regardless of length via hasReportStructure.
const minDeliverableRunes = 300

// publishDeliverable files an interactive turn's final response as a doc_analysis
// work-feed card — the server-side safety net for the deliverable → 작업 피드
// contract (a document was analyzed but the model did not publish it itself). It
// does NOT touch the transcript (the response already rendered in the chat) and
// raises no push (the user is in the conversation); it only files the trackable
// card. The caller (maybeAutoPublishDeliverable) owns the hard gates — a document
// was ingested this turn, the model did not already publish, non-coding session;
// this method owns the substance floor so a thin or narration-only answer to an
// attached document never becomes a card. Returns (false, nil) when suppressed or
// no feed store is wired.
func (d proactiveRelayDeps) publishDeliverable(content string) (bool, error) {
	if d.workFeed == nil {
		return false, nil
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return false, nil
	}
	// Substance gates — the same suppression the proactive card path uses, plus a
	// length/structure floor (auto-publish is more false-positive-prone than an
	// explicit tool call, so it is stricter).
	if isContentlessProactive(content) || isNarrationOnlyProactive(content) {
		return false, nil
	}
	if !hasReportStructure(content) && utf8.RuneCountInString(content) < minDeliverableRunes {
		return false, nil
	}
	// Same prose projection as the relay card path — a doc analysis whose
	// answer is a deneb-ui card must not title itself "```deneb-ui".
	extractSrc := textprep.ReplaceFences(textprep.StripHTMLAnswers(content), textprep.PlainText)
	title, titleLine := extractCardTitle(extractSrc)
	summary := extractCardSummary(extractSrc, titleLine)
	if d.cardTitler != nil && isWeakCardTitle(title, titleLine) {
		if t, s := d.cardTitler(extractSrc); t != "" || s != "" {
			if t != "" {
				title = t
			}
			if s != "" {
				summary = s
			}
		}
	}
	if _, err := d.workFeed.Append(workfeed.Item{
		Source:     workfeed.SourceDocAnalysis,
		Title:      title,
		Summary:    summary,
		Body:       content,
		SessionKey: nativeWorkSessionKey,
	}); err != nil {
		if d.logger != nil {
			d.logger.Error("auto-publish deliverable: work feed append failed", "error", err)
		}
		return false, err
	}
	return true, nil
}

// PublishDeliverable files a substantial interactive response in the work feed.
func (d proactiveRelayDeps) PublishDeliverable(content string) (bool, error) {
	return d.publishDeliverable(content)
}

// startsWithDenebUIFence reports whether the body's first line opens a
// deneb-ui fence under the EXTRACTOR's contract (textprep.IsFenceOpenLine) —
// a prefix check would bypass the accordion for openers the renderers reject
// ("```deneb-ui extra"), landing literal markup in the transcript.
func startsWithDenebUIFence(body string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	return textprep.IsFenceOpenLine(first)
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
	relaylog.Decision(d.behaviorLog, decision, reason, contentLen, preview)
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
	msg := toolport.NewTextChatMessage("assistant", caption, time.Now().UnixMilli())
	msg.Attachments = []toolport.ChatAttachment{{
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
	// Live-push + FCM fallback so a backgrounded/closed phone still gets the
	// weekly-report image notification (the image itself is in the 업무 chat).
	nativepush.PublishWithFallback(d.pushHub, d.pushFCM, nativepush.Event{Title: "Deneb", Body: caption})
	return true, nil
}

// DeliverNativeImage appends an image report to the native work transcript.
func (d proactiveRelayDeps) DeliverNativeImage(caption string, pngBytes []byte) (bool, error) {
	return d.deliverNativeImage(caption, pngBytes)
}
