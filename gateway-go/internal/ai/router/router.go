// Package router holds the model-agnostic reasoning-effort routing policy.
//
// It answers one question — "is this run simple enough to skip the thinking
// phase?" — from a per-model Profile plus a narrow, transport-free Request.
// The policy is pure (no env reads, no config I/O, no provider coupling) so it
// is unit-testable in isolation and reusable from any pipeline that can build a
// Request (interactive chat today; mail analysis / autoreply later).
//
// The mechanics of ACTING on a decision (swapping the agent's thinking config,
// the per-step modulator, escalation/fallback) stay in the chat package, which
// owns the run lifecycle. This package only decides.
//
// Generality lives in the Profile: the heuristics (length gate, hard-signal
// vocabulary, ack set, per-step thresholds) are profile fields, not package
// constants, so a new model inherits DefaultProfile() and an operator can tune
// any knob per model via deneb.json. The current main model resolves to
// DefaultProfile() verbatim, so wiring a model into this layer never changes a
// model that was already configured.
package router

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// Profile is the per-model routing policy. DefaultProfile() is the shared
// baseline (today's tuned constants); modelrole layers the model's template
// toggle and any deneb.json overrides on top. A Profile with Enabled=false or
// an empty ToggleKwarg routes nothing — the inert default for a model that has
// no per-request thinking switch.
type Profile struct {
	// Enabled gates the whole router for this model. Set by the resolver from
	// ToggleKwarg presence (and overridable via config): a model with no
	// per-request off-switch can never be routed.
	Enabled bool

	// ToggleKwarg names the vLLM chat_template_kwargs boolean that disables the
	// model's thinking phase ("thinking" for DeepSeek V4, "enable_thinking" for
	// Qwen3). "" means the model has no per-request toggle. Sourced from
	// modelcaps.ThinkingToggleKwarg so the spelling stays in one place.
	ToggleKwarg string

	// MaxSimpleRunes is the turn-0 query-length gate: a user message longer
	// than this keeps thinking ("long"). The primary lever for routing VOLUME.
	MaxSimpleRunes int

	// Per-step thresholds (Ares e_t = f(turn, o_t)): effort grows with step
	// index and accumulated observation load. StepCeilingTurn is the hard
	// ceiling after which a routed run always reverts to thinking;
	// ObservationRunes/CumulativeRunes size a single batch / the whole run's
	// tool output; HeavyHistoryRunes marks a long assistant message as deep
	// context. Consumed by the chat package's per-step modulator.
	StepCeilingTurn   int
	ObservationRunes  int
	CumulativeRunes   int
	HeavyHistoryRunes int

	// HardSignals are reasoning cues that keep thinking on regardless of length
	// (analysis/planning/code/why…). Deliberately broad — a false-hard only
	// wastes tokens, while a false-easy costs answer quality. Matching is
	// script-aware (see hardSignalHit): a CJK signal matches as a substring
	// (Korean is agglutinative — "분석해줘" must hit "분석"); a Latin signal/stem
	// matches only at a word start ("fix"/"code"/"plan" fire on "fix this" /
	// "code review" / "planning" but NOT inside "prefix" / "decode" / "anywhere"
	// — the substring false-hards that were quietly keeping simple chatter in the
	// thinking phase).
	HardSignals []string

	// PureAcks are messages (punctuation-stripped, exact match) that stay
	// routable even in a heavy thread: pleasantries steer nothing. Exact match,
	// not substring — "안되네" contains "네" but is a steering negative report.
	PureAcks []string
}

// DefaultProfile returns the shared baseline policy — the constants the router
// shipped with, tuned on DSV4-Flash. Enabled/ToggleKwarg are left zero for the
// resolver to fill from the model's capability. Any model with no explicit
// profile resolves to this, so the active main model's behavior is pinned here.
func DefaultProfile() Profile {
	return Profile{
		MaxSimpleRunes:    140,
		StepCeilingTurn:   3,
		ObservationRunes:  2000,
		CumulativeRunes:   8000,
		HeavyHistoryRunes: 1500,
		// Korean-first hard signals with common English equivalents.
		HardSignals: []string{
			// analysis / planning / building
			"분석", "계획", "설계", "구현", "정리", "요약", "작성", "검토", "비교",
			"전략", "보고서", "리뷰", "평가", "조사", "검증", "최적화",
			// code / debugging
			"코드", "디버그", "오류", "버그", "에러", "빌드", "테스트", "리팩",
			// math / logic
			"계산", "증명", "수식",
			// open-ended interrogative — only explanation-seeking "왜"/"why"
			// (reliably reasoning); "어떻게"/"how" were trimmed (they collide
			// with greetings and the "show" substring).
			"왜",
			// english equivalents
			"analyz", "plan", "design", "implement", "debug", "review", "compare",
			"why", "code", "fix", "refactor", "summar", "report",
		},
		PureAcks: []string{
			"고마워", "고마워요", "감사", "감사합니다", "감사해요",
			"잘자", "잘자요", "좋아", "좋아요", "좋네", "좋네요",
			"굿", "오케이", "ㅇㅋ", "ㅋㅋ", "응", "응응", "웅",
			"넵", "네", "네네", "넹", "예", "알겠어", "알겠습니다",
			"ok", "okay", "thanks", "thx", "good",
		},
	}
}

// Request is the transport-free input to a routing decision: the current user
// message, two booleans the caller derives from its own context, and the
// assembled history INCLUDING the current user message (h_t — the same context
// the agent sees).
type Request struct {
	// Message is the current user message text (raw, untrimmed is fine).
	Message string
	// HasAttachments is true when the turn carries image/document attachments;
	// a short caption routinely fronts a complex attachment task.
	HasAttachments bool
	// IsAutomation is true for non-conversational runs (heartbeat, cron, skill
	// review, event ingest, ACP) whose message is a job instruction, not chat.
	// The caller owns the classification (ephemeral markers, session prefixes).
	IsAutomation bool
	// History is the assembled message list including the current user message.
	History []llm.Message
}

// Decision is the router's verdict. ThinkingOff true means the run may skip the
// thinking phase; Reason is a short stable tag for logs and the calibration
// scorecard (e.g. "short-conversational", "hard-signal:분석", "context-heavy").
// The struct intentionally has room to grow (model tier, sampling) without a
// signature change to every caller.
type Decision struct {
	ThinkingOff bool
	Reason      string
}

// Decide returns whether the run's input is obviously simple enough to skip the
// thinking phase, with a short reason. The error asymmetry drives a thinking-on
// bias: automation, attachment-bearing, long, structured, hard-signalled, and
// heavy-thread runs all keep thinking; only short conversational turns route.
//
// The reason tags are stable wire — the agentlog effort scorecard parses them —
// so additions must extend, not rename.
func Decide(p Profile, req Request) Decision {
	if req.IsAutomation {
		return Decision{Reason: "automation"}
	}
	if req.HasAttachments {
		return Decision{Reason: "attachments"}
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return Decision{Reason: "empty"}
	}
	if len([]rune(msg)) > p.MaxSimpleRunes {
		return Decision{Reason: "long"}
	}
	if strings.Contains(msg, "```") || strings.Count(msg, "\n") >= 2 {
		return Decision{Reason: "structured"}
	}
	lower := strings.ToLower(msg)
	// latinTokens are the maximal lowercase-ASCII-letter runs (split on every
	// non-[a-z] rune, so CJK, digits, spaces, and punctuation all break a token).
	// A Latin hard signal matches one of these by word-start prefix; computed once
	// here, not per signal.
	latinTokens := strings.FieldsFunc(lower, func(r rune) bool { return r < 'a' || r > 'z' })
	for _, sig := range p.HardSignals {
		if hardSignalHit(lower, latinTokens, sig) {
			return Decision{Reason: "hard-signal:" + sig}
		}
	}
	// h_t check (Ares decision point #3): a short message steering a heavy
	// thread inherits the thread's effort — unless it is a pure ack that steers
	// nothing.
	if recentContextHeavy(req.History, p.HeavyHistoryRunes) && !isPureAck(msg, p.PureAcks) {
		return Decision{Reason: "context-heavy"}
	}
	return Decision{ThinkingOff: true, Reason: "short-conversational"}
}

// hardSignalHit reports whether a hard signal fires for this message. The match
// is script-aware to kill substring false-hards without weakening real cues:
//
//   - A Latin signal/stem ("fix", "plan", "analyz", "why") matches a token by
//     word-start prefix — "fix this" / "planning" / "analyzing" hit, but "prefix"
//     / "anywhere" / "decode" do NOT (the stem isn't where a word begins). This
//     recovers obviously-simple turns that a raw substring kept thinking on.
//   - A CJK signal ("분석", "왜") matches as a plain substring: Korean is
//     agglutinative, so "분석해줘" must hit "분석" and word-splitting would break it.
//
// The error asymmetry is preserved: every case this newly routes is a genuine
// non-reasoning turn (a stem buried mid-word is not a request to reason), so it
// trims false-hards (wasted tokens) without risking a false-easy (lost quality).
func hardSignalHit(lower string, latinTokens []string, sig string) bool {
	if isLatinStem(sig) {
		for _, t := range latinTokens {
			if strings.HasPrefix(t, sig) {
				return true
			}
		}
		return false
	}
	return strings.Contains(lower, sig)
}

// isLatinStem reports whether sig is a non-empty all-ASCII-letter string (so it
// should match by word start, not substring). A signal carrying any non-Latin
// rune (Korean, a digit) stays on the substring path.
func isLatinStem(sig string) bool {
	for i := 0; i < len(sig); i++ {
		if c := sig[i]; !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return sig != ""
}

// recentContextHeavy reports whether the tail of the assembled history (h_t,
// excluding the current user message) shows deep work in progress: tool
// activity or a long assistant output. Query-only routing would misjudge a
// short follow-up steering such a thread.
func recentContextHeavy(messages []llm.Message, heavyHistoryRunes int) bool {
	if len(messages) < 2 {
		return false
	}
	tail := messages[:len(messages)-1] // exclude the current user message
	start := len(tail) - 6
	if start < 0 {
		start = 0
	}
	for _, m := range tail[start:] {
		for _, b := range llm.ContentToBlocks(m.Content) {
			switch b.Type {
			case "tool_use", "tool_result":
				return true
			case "text":
				if m.Role == "assistant" && len([]rune(b.Text)) > heavyHistoryRunes {
					return true
				}
			}
		}
	}
	return false
}

// isPureAck reports whether msg is a pure pleasantry/ack from the profile's set,
// matched by EXACT equality against the punctuation-stripped, lowercased text.
func isPureAck(msg string, acks []string) bool {
	if len([]rune(msg)) > 12 {
		return false
	}
	// Strip everything but letters/digits so "고마워!!" or "네~" match.
	var b strings.Builder
	for _, r := range strings.ToLower(msg) {
		if ('가' <= r && r <= '힣') || ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') ||
			('ㄱ' <= r && r <= 'ㅣ') {
			b.WriteRune(r)
		}
	}
	stripped := b.String()
	for _, a := range acks {
		if stripped == a {
			return true
		}
	}
	return false
}
