package streaming

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// BroadcastRawFunc sends pre-serialized event data to all matching subscribers.
type BroadcastRawFunc func(event string, data []byte) int

// Stream event names matching the TypeScript wire format. Exported so
// transport adapters (e.g. the miniapp SSE bridge's BroadcastRawFunc filter)
// can match events without duplicating the strings.
const (
	EventChat     = "chat"
	EventDelta    = "chat.delta"
	EventTool     = "chat.tool"
	EventThinking = "chat.thinking"
)

// Limits for broadcast payloads.
const (
	maxBroadcastResultLen = 4096
)

// thinkingThrottle bounds EmitThinking's broadcast rate. The agent loop fires
// OnThinking once per reasoning delta — potentially hundreds per turn — while
// subscribers only need a liveness signal plus a slowly-updating preview, so
// anything beyond one frame every couple of seconds is noise on the wire.
const thinkingThrottle = 2 * time.Second

// Thinking preview sizing: the tail buffer keeps enough recent reasoning text
// to recover whole sentences after whitespace collapsing; the preview itself is
// chip-sized (the native client renders it as "깊이 생각 중: <preview>" in the
// waiting chip).
const (
	thinkingTailRunes    = 512
	thinkingPreviewRunes = 64
	// thinkingPreviewMinRunes suppresses the preview until enough reasoning
	// accumulated to read as a phrase — the first pulse fires on the very
	// first delta, and "깊이 생각 중: 발신" is worse than the bare indicator.
	// It gates only in-progress fragments; a finished sentence shows even short.
	thinkingPreviewMinRunes = 12
	// thinkingSentenceMinRunes is the floor for treating a terminated span as a
	// real sentence (vs a blip like "음..."); below it latestSentence scans back.
	thinkingSentenceMinRunes = 6
)

// sentenceTerminatorCutset trims trailing sentence punctuation/space so a chip
// line ends on a word, not "수법입니다." or "음...".
const sentenceTerminatorCutset = ".!?。！？… "

// Broadcaster relays agent streaming events to WebSocket clients
// via the gateway's raw broadcast function. All methods are safe to call
// when broadcastRaw is nil (they silently no-op).
type Broadcaster struct {
	broadcastRaw BroadcastRawFunc
	sessionKey   string
	clientRunID  string
	seq          atomic.Int64
	// lastThinkingNs is the monotonic-ish wall clock (UnixNano) of the last
	// EmitThinking broadcast, used to throttle the per-reasoning-delta firehose.
	lastThinkingNs atomic.Int64
	// thinkingMu guards thinkingTail, the rolling tail of recent reasoning text
	// that throttled EmitThinking frames condense into a chip-sized preview.
	thinkingMu   sync.Mutex
	thinkingTail []rune
}

// NewBroadcaster creates a new Broadcaster for a given session/run.
func NewBroadcaster(broadcastRaw BroadcastRawFunc, sessionKey, clientRunID string) *Broadcaster {
	return &Broadcaster{
		broadcastRaw: broadcastRaw,
		sessionKey:   sessionKey,
		clientRunID:  clientRunID,
	}
}

// EmitDelta broadcasts a streaming text delta to WS clients.
func (sb *Broadcaster) EmitDelta(text string) {
	if text == "" {
		return
	}
	sb.emit(EventDelta, map[string]any{
		"delta": text,
	})
}

// EmitThinking broadcasts a reasoning-in-progress liveness signal, throttled
// to at most one frame per thinkingThrottle. delta is the reasoning text chunk
// that triggered the pulse: every chunk is accumulated into a rolling tail, and
// each throttled frame carries a chip-sized `preview` of the most recent
// reasoning so the client's "깊이 생각 중" indicator can narrate the live
// thought instead of sitting static for the whole reasoning stretch.
func (sb *Broadcaster) EmitThinking(delta string) {
	sb.appendThinking(delta)
	now := time.Now().UnixNano()
	last := sb.lastThinkingNs.Load()
	if now-last < int64(thinkingThrottle) {
		return
	}
	// CAS so concurrent reasoning deltas can't double-emit within one window.
	if !sb.lastThinkingNs.CompareAndSwap(last, now) {
		return
	}
	payload := map[string]any{}
	if preview := sb.thinkingPreview(); preview != "" {
		payload["preview"] = preview
	}
	sb.emit(EventThinking, payload)
}

// appendThinking adds a reasoning chunk to the rolling tail, trimming the
// front so the buffer never exceeds thinkingTailRunes.
func (sb *Broadcaster) appendThinking(delta string) {
	if delta == "" {
		return
	}
	sb.thinkingMu.Lock()
	defer sb.thinkingMu.Unlock()
	sb.thinkingTail = append(sb.thinkingTail, []rune(delta)...)
	if over := len(sb.thinkingTail) - thinkingTailRunes; over > 0 {
		sb.thinkingTail = sb.thinkingTail[over:]
	}
}

// thinkingPreview condenses the rolling reasoning tail into one chip-sized,
// human-readable line. The model narrates verbose, parenthesized thoughts
// ("(우선 발신자 주소부터 확인해야 합니다. …)"), so the old last-N-rune slice
// surfaced mid-sentence fragments with a dangling paren ("…정보를 바로 제공하게)").
// Instead we strip the narration noise and show the most recent *complete*
// sentence from its head, so the chip reads as a coherent thought
// ("발신자 주소부터 확인해야 합니다…").
func (sb *Broadcaster) thinkingPreview() string {
	sb.thinkingMu.Lock()
	tail := string(sb.thinkingTail)
	sb.thinkingMu.Unlock()
	return cleanThinkingPreview(tail)
}

// cleanThinkingPreview renders a raw reasoning tail into a chip line:
//  1. strip the wrapper/markdown punctuation the model sprinkles in
//     (parentheses around thoughts, *, #, >, backticks, brackets);
//  2. collapse whitespace;
//  3. keep the most recent sentence that ends in a terminator (falling back to
//     the in-progress fragment when none has completed yet);
//  4. drop a leading discourse marker (아, 우선, 그리고, Okay, Let me …);
//  5. show it from the head, with a trailing ellipsis only when it overflows
//     the chip — never a leading one, so the thought starts at its beginning.
//
// Returns "" when too little readable text has accumulated.
func cleanThinkingPreview(raw string) string {
	cleaned := strings.Join(strings.Fields(stripReasoningNoise(raw)), " ")
	if cleaned == "" {
		return ""
	}
	sentence, complete := latestSentence(cleaned)
	sentence = strings.TrimRight(stripLeadingFiller(sentence), sentenceTerminatorCutset)
	runes := []rune(sentence)
	if len(runes) == 0 {
		return ""
	}
	// Suppress only sub-readable in-progress fragments — a finished sentence is
	// shown even when short ("계좌가 바뀌었다").
	if !complete && len(runes) < thinkingPreviewMinRunes {
		return ""
	}
	if len(runes) <= thinkingPreviewRunes {
		return sentence
	}
	return strings.TrimSpace(string(runes[:thinkingPreviewRunes])) + "…"
}

// reasoningNoiseReplacer drops the delimiter punctuation models wrap thoughts in
// (so the chip never shows a dangling "(" or ")") plus markdown emphasis,
// heading, quote, and code markers — keeping the inner words intact.
var reasoningNoiseReplacer = strings.NewReplacer(
	"(", "", ")", "", "（", "", "）", "",
	"[", "", "]", "", "「", "", "」", "",
	"*", "", "#", "", "`", "", ">", "",
)

func stripReasoningNoise(s string) string {
	return reasoningNoiseReplacer.Replace(s)
}

// latestSentence returns the most recent complete sentence in s and true, or
// the trailing in-progress fragment and false when nothing has terminated yet.
// A "complete" sentence must clear thinkingSentenceMinRunes once its opener and
// terminators are discounted, so terminator-only blips ("음...") never win and
// the scan falls back to the previous real sentence.
func latestSentence(s string) (string, bool) {
	runes := []rune(s)
	var sentences []string
	start := 0
	for i := 0; i < len(runes); i++ {
		if !isSentenceTerminator(runes[i]) {
			continue
		}
		// Consume a run of terminators so "..." stays with its sentence.
		j := i
		for j+1 < len(runes) && isSentenceTerminator(runes[j+1]) {
			j++
		}
		if seg := strings.TrimSpace(string(runes[start : j+1])); seg != "" {
			sentences = append(sentences, seg)
		}
		start = j + 1
		i = j
	}
	for k := len(sentences) - 1; k >= 0; k-- {
		core := strings.TrimRight(stripLeadingFiller(sentences[k]), sentenceTerminatorCutset)
		if len([]rune(core)) < thinkingSentenceMinRunes {
			continue
		}
		if isMetaThought(sentences[k]) {
			continue // model self-talk, not the user's task — keep scanning back
		}
		return sentences[k], true
	}
	// Nothing has completed yet — narrate the live, in-progress fragment.
	if trailing := strings.TrimSpace(string(runes[start:])); trailing != "" {
		return trailing, false
	}
	if len(sentences) > 0 {
		return sentences[len(sentences)-1], true
	}
	return strings.TrimSpace(s), false
}

func isSentenceTerminator(r rune) bool {
	switch r {
	case '.', '!', '?', '。', '！', '？', '…':
		return true
	}
	return false
}

// leadingFillers are opener words safe to drop when followed by a separator.
// Conservative on purpose — only unambiguous interjections/conjunctions.
var leadingFillers = []string{
	"그리고", "그래서", "그러면", "그런데", "그럼", "우선", "먼저", "이제",
	"또한", "사실", "일단", "아", "음", "자",
	"okay", "ok", "so", "well", "hmm", "now", "first", "next", "then",
	"let me", "let's",
}

// metaThoughtMarkers flag a reasoning sentence as model self-talk about its own
// output format or identity rather than the user's task. Surfacing these in the
// chip is jarring ("나는 단일 챗봇...", "최종 응답은 한국어로...") and even leaks
// the role-play framing, so latestSentence skips them. Conservative on purpose:
// these phrases almost never appear in a genuine task thought.
var metaThoughtMarkers = []string{
	"챗봇", "역할극", "롤플레이", "언어 모델", "언어모델", "시스템 프롬프트",
	"최종 응답", "최종 답변", "응답은 한국어", "답변은 한국어", "마크다운",
	"language model", "roleplay", "role-play", "role play", "system prompt", "as an ai",
}

func isMetaThought(s string) bool {
	low := strings.ToLower(s)
	for _, m := range metaThoughtMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// stripLeadingFiller drops a single leading discourse marker — an interjection
// or conjunction the model opens a thought with (아,/우선/그리고/Okay,/Let me …)
// — so the preview starts on the substantive clause. Only fires when the marker
// is followed by a separator, so it never bites into a real word ("아이디어").
func stripLeadingFiller(s string) string {
	t := strings.TrimSpace(s)
	low := strings.ToLower(t)
	for _, f := range leadingFillers {
		if !strings.HasPrefix(low, f) {
			continue
		}
		rest := t[len(f):]
		trimmed := strings.TrimLeft(rest, " ,，、:：")
		if trimmed != rest && trimmed != "" {
			return strings.TrimSpace(trimmed)
		}
	}
	return t
}

// EmitToolStart broadcasts a tool invocation start event. detail is an
// optional short human hint extracted from the tool input (query, command,
// file name) — omitted from the payload when empty.
func (sb *Broadcaster) EmitToolStart(name, toolUseID, detail string) {
	payload := map[string]any{
		"state":     "started",
		"tool":      name,
		"toolUseId": toolUseID,
	}
	if detail != "" {
		payload["detail"] = detail
	}
	sb.emit(EventTool, payload)
}

// EmitToolResult broadcasts a tool execution result event.
func (sb *Broadcaster) EmitToolResult(name, toolUseID, result string, isError bool) {
	sb.emit(EventTool, map[string]any{
		"state":     "completed",
		"tool":      name,
		"toolUseId": toolUseID,
		"result":    truncateForBroadcast(result, maxBroadcastResultLen),
		"isError":   isError,
	})
}

// EmitComplete broadcasts the final chat completion event.
func (sb *Broadcaster) EmitComplete(text string, usage llm.TokenUsage) {
	sb.emit(EventChat, map[string]any{
		"state": "done",
		"text":  text,
		"usage": map[string]int{
			"inputTokens":  usage.InputTokens,
			"outputTokens": usage.OutputTokens,
		},
	})
}

// EmitError broadcasts an error event for the run.
func (sb *Broadcaster) EmitError(errMsg string) {
	sb.emit(EventChat, map[string]any{
		"state": "error",
		"error": errMsg,
	})
}

// EmitStarted broadcasts that the agent run has started.
func (sb *Broadcaster) EmitStarted() {
	sb.emit(EventChat, map[string]any{
		"state": "started",
	})
}

// EmitAborted broadcasts that the agent run was aborted.
func (sb *Broadcaster) EmitAborted(partialText string) {
	sb.emit(EventChat, map[string]any{
		"state": "aborted",
		"text":  partialText,
	})
}

// emit is the shared broadcast path. It injects common fields (sessionKey,
// clientRunId, seq) and serializes to JSON. No-ops when broadcastRaw is nil.
func (sb *Broadcaster) emit(event string, payload map[string]any) {
	if sb.broadcastRaw == nil {
		return
	}
	payload["sessionKey"] = sb.sessionKey
	payload["clientRunId"] = sb.clientRunID
	payload["seq"] = sb.seq.Add(1)
	data, err := json.Marshal(map[string]any{
		"event":   event,
		"payload": payload,
	})
	if err != nil {
		return
	}
	sb.broadcastRaw(event, data)
}

// truncateForBroadcast caps a string to at most maxLen bytes to prevent
// oversized WS frames. The cut is rounded back to a UTF-8 rune boundary so
// multi-byte characters (notably Korean) never split across the truncation.
func truncateForBroadcast(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "… (이하 생략)"
}
