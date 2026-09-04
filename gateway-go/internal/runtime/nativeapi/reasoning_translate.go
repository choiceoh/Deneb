package nativeapi

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	// One translation call carries at most this many bytes. It keeps a request
	// small enough to finish inside a throttle window, and keeps a long block
	// under the translator's own size cap — which refuses the whole text rather
	// than part of it, so an uncut backlog would translate nothing at all.
	liveReasoningChunkBytes = 4000
	// finish runs on the path of the terminal frame, so the work it may do there
	// is bounded: past this the remainder ships in the model's own language
	// rather than holding the turn open.
	liveReasoningFinishBudget = 8 * time.Second
)

// liveReasoningTranslator renders the live reasoning stream into Korean while it
// streams, so the client's expandable block reads Korean from the start instead
// of standing in English until the done frame flips it.
//
// The reasoning sink fires on the agent's streaming goroutine, so translation
// must not happen there: observe() only records the newest text and, when no
// pass is already running, starts one worker. That worker is the sole writer of
// source/rendered, which is why a pass can read them without racing itself.
//
// Only settled text is translated (see settledReasoningChunk) and each chunk is
// translated exactly once — the growing tail of a half-written line is left for
// a later pass. Combined with the translator's own line-level cache this keeps a
// streamed turn at roughly the character cost of translating the block once, and
// leaves the done frame paying only for the tail it has not seen.
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	liveReasoningTranslator.mu  →  the SSE writer's mutex (inside emit)
//
// emit is deliberately called while mu is held. It is not an external listener
// but the stream's own writeEvent, which takes only the SSE mutex and never
// calls back in here, and holding mu across it is what makes stop() a hard
// barrier: once stop() returns no frame can be written any more, so a late pass
// can never touch the ResponseWriter after the handler has returned.
type liveReasoningTranslator struct {
	ctx       context.Context
	translate func(context.Context, string) (string, bool)
	emit      func(text string)
	logger    *slog.Logger
	// finishBudget caps the terminal frame's tail work; a field so tests can
	// shrink it without sleeping for the real one.
	finishBudget time.Duration

	mu sync.Mutex
	// pending is the newest reasoning-so-far handed in by the sink.
	pending string
	// source is the part of pending already translated, rendered its Korean.
	source   string
	rendered string
	inFlight bool
	stopped  bool
	// Counted for the one Debug line at stop(): whether the live path fired at
	// all, and how much of it the translator declined, is otherwise unknowable
	// from outside — the fallback is silent by design.
	passes    int
	declined  int
	sentBytes int
}

func newLiveReasoningTranslator(
	ctx context.Context,
	translate func(context.Context, string) (string, bool),
	emit func(text string),
	logger *slog.Logger,
) *liveReasoningTranslator {
	return &liveReasoningTranslator{
		ctx: ctx, translate: translate, emit: emit, logger: logger,
		finishBudget: liveReasoningFinishBudget,
	}
}

// observe records the newest reasoning-so-far and starts a translation pass when
// none is running. It never blocks on the translator: the caller is the agent's
// streaming goroutine, and holding that up for a display nicety would slow the
// answer itself.
func (t *liveReasoningTranslator) observe(full string) {
	if t == nil || full == "" {
		return
	}
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.pending = full
	if t.inFlight {
		t.mu.Unlock()
		return
	}
	t.inFlight = true
	t.mu.Unlock()

	safego.GoWithSlog(t.logger, "chat-stream-reasoning-translate", t.drain)
}

// stop ends live translation. It is a barrier, not a request: an emit in
// progress finishes first (it runs under mu) and every later pass sees stopped
// and writes nothing. A pass blocked in the translator keeps running to fill the
// cache — the done frame reuses it — but can no longer reach the writer.
func (t *liveReasoningTranslator) stop() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.stopped = true
	passes, declined, sent := t.passes, t.declined, t.sentBytes
	t.mu.Unlock()

	if passes > 0 && t.logger != nil {
		t.logger.Debug("live reasoning translated",
			"passes", passes, "declined", declined, "bytes", sent)
	}
}

// finish renders the turn's final reasoning for the terminal frame: whatever the
// live pass already translated, extended with everything it never covered.
//
// The final text is not the live stream verbatim. The executor joins each turn's
// thinking blocks with a blank line (joinAllThinkingTexts, appendRunSection) and
// the pipeline trims the result, so a byte-exact prefix test fails on every
// multi-step turn — measured: sourceLen 1150, finalLen 1406, hasPrefix false.
// Coverage is therefore matched on non-whitespace content.
//
// Nothing here re-translates the whole block. That one call is size-capped and
// deadlined by the translator, and a long agentic turn is exactly when it
// refuses — which turned a block that had been Korean all through the stream
// back into English on its last frame. Instead the uncovered part is translated
// in chunks within a budget, and whatever the budget does not reach ships as the
// model wrote it: partly translated, never wholly undone.
func (t *liveReasoningTranslator) finish(final string) string {
	if t == nil || strings.TrimSpace(final) == "" {
		return ""
	}
	t.mu.Lock()
	source, rendered := t.source, t.rendered
	t.mu.Unlock()

	base, rest := "", final
	if covered, ok := coveredPrefix(final, source); ok && rendered != "" {
		// Coverage ends at the last matched content byte, so the whitespace that
		// follows it belongs to the remainder. Drop the rendered copy of it or
		// the seam gets it twice — and the final text is the authority on what
		// that whitespace is (a live sentence cut's space is a blank line there).
		base, rest = strings.TrimRight(rendered, " \n\t\r"), final[covered:]
	}

	deadline := time.Now().Add(t.finishBudget)
	for rest != "" {
		// Whitespace carries over verbatim — the blank line between two turns is
		// structure, not text to translate, and sending it would spend a call to
		// get the same bytes back.
		if lead := len(rest) - len(strings.TrimLeft(rest, " \n\t\r")); lead > 0 {
			base += rest[:lead]
			rest = rest[lead:]
			continue
		}
		if time.Now().After(deadline) {
			return joinRenderedChunk(base, rest)
		}
		chunk := rest
		if len(chunk) > liveReasoningChunkBytes {
			chunk = rest[:boundedReasoningCut(rest)]
		}
		rest = rest[len(chunk):]
		out, ok := t.translate(t.ctx, chunk)
		if !ok {
			// Already Korean, or a refusal: show what the model wrote.
			out = chunk
		}
		base = joinRenderedChunk(base, out)
	}
	return base
}

// coveredPrefix reports how many bytes of final the already-translated source
// covers, comparing non-whitespace content so the blank lines the executor
// inserts between turns do not read as a mismatch. ok is false when source is
// not the beginning of final at all — the rolling window dropped the head, the
// final text is shorter than what streamed, or nothing was translated yet.
func coveredPrefix(final, source string) (int, bool) {
	if strings.TrimSpace(source) == "" {
		return 0, false
	}
	i, j, end := 0, 0, 0
	for {
		for j < len(source) && isSpaceByte(source[j]) {
			j++
		}
		if j == len(source) {
			return end, true
		}
		for i < len(final) && isSpaceByte(final[i]) {
			i++
		}
		// Byte comparison is rune comparison here: a whitespace byte never
		// appears inside a multi-byte UTF-8 sequence.
		if i == len(final) || final[i] != source[j] {
			return 0, false
		}
		i++
		j++
		end = i
	}
}

func isSpaceByte(b byte) bool { return b == ' ' || b == '\n' || b == '\t' || b == '\r' }

// drain translates settled text until nothing is settled or the stream stops.
func (t *liveReasoningTranslator) drain() {
	// The worker clears inFlight itself on every ordinary exit (see below), so
	// the only thing left to unwedge is a panic: without this the flag would
	// stay set and no later frame could ever start a pass again. Re-panic so
	// safego still logs it.
	defer func() {
		if r := recover(); r != nil {
			t.mu.Lock()
			t.inFlight = false
			t.mu.Unlock()
			panic(r)
		}
	}()

	for {
		t.mu.Lock()
		if t.stopped {
			t.inFlight = false
			t.mu.Unlock()
			return
		}
		source, rendered := t.source, t.rendered
		if !strings.HasPrefix(t.pending, source) {
			// The broadcaster's live buffer is a rolling window — past its cap it
			// drops from the head, so what we translated stops being a prefix of
			// what it now holds. Start the block over rather than splice two texts
			// that no longer meet.
			source, rendered = "", ""
		}
		chunk := settledReasoningChunk(t.pending[len(source):])
		if chunk == "" {
			// Nothing has settled yet. Clearing the flag while still holding the
			// lock is what makes this exit safe: a frame arriving after this point
			// sees no pass running and starts one, instead of being swallowed by a
			// worker that had already read pending and was on its way out.
			t.inFlight = false
			t.mu.Unlock()
			return
		}
		t.mu.Unlock()

		out, ok := t.translate(t.ctx, chunk)
		if !ok {
			// Already Korean, over the size cap, or a translator refusal — this
			// seam cannot tell them apart, and all three want the same thing: show
			// what the model wrote rather than a hole. The done frame retries the
			// whole block, so a refusal here is not the last word.
			out = chunk
		}
		display := joinRenderedChunk(rendered, out)

		t.mu.Lock()
		if t.stopped {
			t.inFlight = false
			t.mu.Unlock()
			return
		}
		t.source = source + chunk
		t.rendered = display
		t.passes++
		t.sentBytes += len(chunk)
		if !ok {
			t.declined++
		}
		t.emit(display)
		t.mu.Unlock()
	}
}

// settledReasoningChunk returns the leading part of the untranslated remainder
// that is worth translating now: everything through the last line break or
// sentence end. What follows is a half-written sentence, and translating that
// would pay for the same characters again once the model finishes it. Returns
// "" while nothing has settled.
func settledReasoningChunk(pending string) string {
	if pending == "" {
		return ""
	}
	cut := 0
	if i := strings.LastIndexByte(pending, '\n'); i >= 0 {
		cut = i + 1
	}
	// Sentences settle too, not just lines. Reasoning that runs on for a whole
	// paragraph — measured on a live turn: one 130-byte line, no break until the
	// end — would otherwise show nothing at all until the done frame, which is
	// exactly the delay this path exists to remove. Committed text is never sent
	// again, so a finer cut costs no extra characters.
	if j := lastSentenceEnd(pending[cut:]); j > 0 {
		cut += j
	}
	if cut == 0 {
		return ""
	}
	if cut > liveReasoningChunkBytes {
		cut = boundedReasoningCut(pending)
	}
	return pending[:cut]
}

// boundedReasoningCut picks the largest cut at or under the per-call byte budget,
// preferring a line break, then a sentence end, then any space. It always
// returns a positive rune-aligned length, so a backlog cannot stall forever on
// text that offers no boundary at all.
func boundedReasoningCut(pending string) int {
	window := pending[:liveReasoningChunkBytes]
	if i := strings.LastIndexByte(window, '\n'); i >= 0 {
		return i + 1
	}
	if j := lastSentenceEnd(window); j > 0 {
		return j
	}
	if i := strings.LastIndexByte(window, ' '); i >= 0 {
		return i + 1
	}
	cut := liveReasoningChunkBytes
	for cut > 0 && !utf8.RuneStart(pending[cut]) {
		cut--
	}
	if cut == 0 {
		return liveReasoningChunkBytes
	}
	return cut
}

// lastSentenceEnd returns the offset just past the last sentence terminator in
// s — a Latin terminator followed by whitespace, or a CJK one anywhere — and 0
// when the text holds no sentence end.
func lastSentenceEnd(s string) int {
	end := 0
	prevTerm := false
	for i, r := range s {
		switch r {
		case '。', '！', '？', '…':
			end = i + utf8.RuneLen(r)
			prevTerm = false
			continue
		}
		if prevTerm && (r == ' ' || r == '\n' || r == '\t') {
			end = i + utf8.RuneLen(r)
		}
		prevTerm = r == '.' || r == '!' || r == '?'
	}
	return end
}

// joinRenderedChunk appends a translated chunk to what is already rendered,
// restoring the separator a sentence-boundary cut leaves at the seam: the chunk
// carries its trailing space, but a translator is free to trim it.
func joinRenderedChunk(rendered, chunk string) string {
	if rendered == "" {
		return chunk
	}
	if endsWithSpace(rendered) || startsWithSpace(chunk) {
		return rendered + chunk
	}
	return rendered + " " + chunk
}

func endsWithSpace(s string) bool {
	r, size := utf8.DecodeLastRuneInString(s)
	return size > 0 && isSpaceRune(r)
}

func startsWithSpace(s string) bool {
	r, size := utf8.DecodeRuneInString(s)
	return size > 0 && isSpaceRune(r)
}

func isSpaceRune(r rune) bool { return r == ' ' || r == '\n' || r == '\t' || r == '\r' }
