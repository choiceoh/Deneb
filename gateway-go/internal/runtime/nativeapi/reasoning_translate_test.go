package nativeapi

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSettledReasoningChunkCutsAtLastLineBreak(t *testing.T) {
	// The tail after the last newline is a half-written line: translating it now
	// would pay for the same characters again once the model finishes it.
	got := settledReasoningChunk("first line\nsecond line\nhalf writ")
	if want := "first line\nsecond line\n"; got != want {
		t.Fatalf("chunk = %q, want %q", got, want)
	}
}

func TestSettledReasoningChunkHoldsBackAnUnsettledLine(t *testing.T) {
	if got := settledReasoningChunk("the model has not finished this line yet"); got != "" {
		t.Fatalf("chunk = %q, want empty (nothing settled)", got)
	}
}

func TestSettledReasoningChunkSettlesAtSentenceEnd(t *testing.T) {
	// Reasoning that runs on without a newline must still settle sentence by
	// sentence — a live turn produced exactly one 130-byte line, and waiting for
	// a break would have left the block empty until the done frame.
	oneLine := "간단한 산수입니다. 1부터 30까지의 소수: 2, 3, 5, 7. 합 = 17. 이제 남은 것을 계"
	got := settledReasoningChunk(oneLine)
	if !strings.HasSuffix(got, "합 = 17. ") {
		t.Fatalf("chunk = %q, want a cut at the last sentence end", got)
	}
	if strings.Contains(got, "이제 남은") {
		t.Fatal("chunk carried the half-written tail")
	}

	// The same cut applies past a line break: the settled part is the newline
	// prefix plus whatever sentences finished after it.
	para := "first line\n" + strings.Repeat("This is a settled sentence. ", 40) + "and this one is still being writ"
	got = settledReasoningChunk(para)
	if !strings.HasPrefix(got, "first line\n") || !strings.HasSuffix(got, "sentence. ") {
		t.Fatalf("chunk = %q…, want the line break plus finished sentences", got[:min(40, len(got))])
	}
	if strings.Contains(got, "still being writ") {
		t.Fatal("chunk carried the half-written tail")
	}
}

func TestSettledReasoningChunkBoundsOneCall(t *testing.T) {
	// A backlog far past the per-call budget still makes progress, in one bite
	// small enough that the translator's own size cap cannot refuse it.
	huge := strings.Repeat("한 줄의 추론 텍스트입니다.\n", 2000)
	got := settledReasoningChunk(huge)
	if got == "" {
		t.Fatal("chunk = empty, want progress on a large backlog")
	}
	if len(got) > liveReasoningChunkBytes {
		t.Fatalf("chunk = %d bytes, want <= %d", len(got), liveReasoningChunkBytes)
	}
	if !strings.HasPrefix(huge, got) {
		t.Fatal("chunk is not a prefix of the pending text")
	}
}

func TestSettledReasoningChunkAlwaysAdvancesWithoutBoundaries(t *testing.T) {
	// Neither a line break nor a sentence end nor a space inside the budget: the
	// cut must still be positive and rune-aligned, or the block stalls forever.
	blob := strings.Repeat("한", 4000) + "\n"
	got := settledReasoningChunk(blob)
	if got == "" {
		t.Fatal("chunk = empty, want a forced cut")
	}
	if len(got) > liveReasoningChunkBytes {
		t.Fatalf("chunk = %d bytes, want <= %d", len(got), liveReasoningChunkBytes)
	}
	if strings.ContainsRune(got, '�') {
		t.Fatal("chunk cut a rune in half")
	}
}

// liveHarness drives a liveReasoningTranslator with a scripted translator and
// collects the frames it emits.
type liveHarness struct {
	mu     sync.Mutex
	calls  []string
	frames chan string
	live   *liveReasoningTranslator
}

func newLiveHarness(t *testing.T, translate func(string) (string, bool)) *liveHarness {
	t.Helper()
	h := &liveHarness{frames: make(chan string, 16)}
	h.live = newLiveReasoningTranslator(
		context.Background(),
		func(_ context.Context, text string) (string, bool) {
			h.mu.Lock()
			h.calls = append(h.calls, text)
			h.mu.Unlock()
			return translate(text)
		},
		func(text string) { h.frames <- text },
		nil,
	)
	return h
}

func (h *liveHarness) nextFrame(t *testing.T) string {
	t.Helper()
	select {
	case f := <-h.frames:
		return f
	case <-time.After(2 * time.Second):
		t.Fatal("no reasoning frame emitted")
		return ""
	}
}

func (h *liveHarness) translated() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

func markKorean(text string) (string, bool) { return "[ko]" + text, true }

func TestLiveReasoningTranslatorEmitsTranslationNotSource(t *testing.T) {
	h := newLiveHarness(t, markKorean)
	h.live.observe("The user asks about the stream.\n")

	frame := h.nextFrame(t)
	if !strings.HasPrefix(frame, "[ko]") {
		t.Fatalf("frame = %q, want the translated text", frame)
	}
}

func TestLiveReasoningTranslatorTranslatesEachChunkOnce(t *testing.T) {
	h := newLiveHarness(t, markKorean)

	h.live.observe("one.\n")
	if got := h.nextFrame(t); got != "[ko]one.\n" {
		t.Fatalf("first frame = %q", got)
	}
	// The sink hands in the whole reasoning-so-far every window; only the part
	// that is new since the last pass may reach the translator.
	h.live.observe("one.\ntwo.\n")
	if got := h.nextFrame(t); got != "[ko]one.\n[ko]two.\n" {
		t.Fatalf("second frame = %q", got)
	}

	calls := h.translated()
	if len(calls) != 2 {
		t.Fatalf("translator calls = %d (%q), want 2", len(calls), calls)
	}
	if calls[0] != "one.\n" || calls[1] != "two.\n" {
		t.Fatalf("translator saw %q, want the two chunks separately", calls)
	}
}

func TestLiveReasoningTranslatorKeepsSourceWhenTranslatorDeclines(t *testing.T) {
	// A refusal is indistinguishable from "already Korean" here, and both want
	// the model's own text shown rather than a hole in the block.
	h := newLiveHarness(t, func(string) (string, bool) { return "", false })
	h.live.observe("이미 한국어인 추론입니다.\n")

	if got := h.nextFrame(t); got != "이미 한국어인 추론입니다.\n" {
		t.Fatalf("frame = %q, want the original text", got)
	}
}

func TestLiveReasoningTranslatorRestartsWhenBufferRolls(t *testing.T) {
	h := newLiveHarness(t, markKorean)

	h.live.observe("head.\n")
	if got := h.nextFrame(t); got != "[ko]head.\n" {
		t.Fatalf("first frame = %q", got)
	}
	// Past its cap the broadcaster drops from the head, so what we translated is
	// no longer a prefix: the block must restart rather than splice.
	h.live.observe("tail only.\n")
	if got := h.nextFrame(t); got != "[ko]tail only.\n" {
		t.Fatalf("frame after roll = %q, want a restarted block", got)
	}
}

func TestLiveReasoningTranslatorSilentAfterStop(t *testing.T) {
	h := newLiveHarness(t, markKorean)
	h.live.stop()
	h.live.observe("anything at all.\n")

	select {
	case f := <-h.frames:
		t.Fatalf("emitted %q after stop — that is a write past the terminal frame", f)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestLiveReasoningTranslatorPicksUpTextArrivingAsAPassEnds(t *testing.T) {
	// The missed-wakeup case: a frame that lands between a worker's last read
	// and its exit must still start a pass, or the block stops growing for the
	// rest of the turn.
	h := newLiveHarness(t, markKorean)
	h.live.observe("first.\n")
	if got := h.nextFrame(t); got != "[ko]first.\n" {
		t.Fatalf("first frame = %q", got)
	}
	for i := 0; i < 20; i++ {
		h.live.observe("first.\nsecond.\n")
	}
	if got := h.nextFrame(t); got != "[ko]first.\n[ko]second.\n" {
		t.Fatalf("frame = %q, want the later text picked up", got)
	}
}

func TestLiveReasoningTranslatorReleasesWorkerWhilePanicUnwinds(t *testing.T) {
	tests := []struct {
		name      string
		translate func(context.Context, string) (string, bool)
		emit      func(string)
	}{
		{
			name:      "translator panic while unlocked",
			translate: func(context.Context, string) (string, bool) { panic("translator") },
			emit:      func(string) {},
		},
		{
			name:      "emitter panic while locked",
			translate: func(_ context.Context, text string) (string, bool) { return text, true },
			emit:      func(string) { panic("emitter") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			live := newLiveReasoningTranslator(context.Background(), tt.translate, tt.emit, nil)
			live.pending = "settled. "
			live.inFlight = true

			panicValue := make(chan any, 1)
			go func() {
				defer func() { panicValue <- recover() }()
				live.drain()
			}()

			select {
			case recovered := <-panicValue:
				if recovered == nil {
					t.Fatal("panic did not reach the goroutine boundary")
				}
			case <-time.After(time.Second):
				t.Fatal("panic cleanup deadlocked")
			}

			live.mu.Lock()
			inFlight := live.inFlight
			live.mu.Unlock()
			if inFlight {
				t.Fatal("worker remained in flight after panic unwind")
			}
		})
	}
}

func TestLiveReasoningFinishExtendsAcrossTheExecutorSeparator(t *testing.T) {
	// The bug this shape exists for: the executor joins each turn's thinking
	// blocks with a blank line, so the final text is never a byte-exact
	// extension of what streamed. Coverage must survive that, or the whole block
	// gets re-translated and reverts to English when that call is refused.
	h := newLiveHarness(t, markKorean)
	h.live.observe("step one thought. ")
	if got := h.nextFrame(t); got != "[ko]step one thought. " {
		t.Fatalf("live frame = %q", got)
	}
	h.live.stop()

	// The separator comes from the final text, not from the live cut's space.
	got := h.live.finish("step one thought.\n\nstep two thought.")
	if got != "[ko]step one thought.\n\n[ko]step two thought." {
		t.Fatalf("finish = %q, want the live block plus the translated tail", got)
	}
	calls := h.translated()
	if len(calls) != 2 || strings.Contains(calls[1], "step one") {
		t.Fatalf("translator saw %q, want only the uncovered tail on the second call", calls)
	}
}

func TestLiveReasoningFinishTranslatesWhatTheLiveBlockCannotCover(t *testing.T) {
	// No live block at all (a model that streams no reasoning deltas), and a
	// rolling window that dropped the head: both must still come back Korean —
	// the old whole-block retry is what shipped English here.
	fresh := newLiveHarness(t, markKorean)
	if got := fresh.live.finish("a block that never streamed."); got != "[ko]a block that never streamed." {
		t.Fatalf("finish with no live block = %q", got)
	}

	rolled := newLiveHarness(t, markKorean)
	rolled.live.observe("dropped head.\n")
	if got := rolled.nextFrame(t); got != "[ko]dropped head.\n" {
		t.Fatalf("live frame = %q", got)
	}
	rolled.live.stop()
	if got := rolled.live.finish("an entirely different block."); got != "[ko]an entirely different block." {
		t.Fatalf("finish after a roll = %q, want the whole text translated", got)
	}
}

func TestLiveReasoningFinishChunksPastTheWholeBlockCap(t *testing.T) {
	// A block far past one call's budget must not be all-or-nothing: it arrives
	// in chunks, each small enough that the translator's size cap cannot refuse
	// it, and every byte of the source is accounted for.
	h := newLiveHarness(t, markKorean)
	block := strings.Repeat("A settled sentence about the code. ", 500) // ~17KB
	got := h.live.finish(block)

	calls := h.translated()
	if len(calls) < 4 {
		t.Fatalf("translator calls = %d, want the block split into several", len(calls))
	}
	for i, c := range calls {
		if len(c) > liveReasoningChunkBytes {
			t.Fatalf("chunk %d = %d bytes, want <= %d", i, len(c), liveReasoningChunkBytes)
		}
	}
	if joined := strings.ReplaceAll(got, "[ko]", ""); joined != block {
		t.Fatalf("finish lost or reordered source text (%d bytes vs %d)", len(joined), len(block))
	}
}

func TestLiveReasoningFinishShipsTheRemainderWhenTheBudgetRunsOut(t *testing.T) {
	// The terminal frame must not hang on a slow translator: what the budget
	// does not reach ships as the model wrote it — partly translated, never
	// held open.
	h := newLiveHarness(t, func(text string) (string, bool) {
		time.Sleep(30 * time.Millisecond)
		return "[ko]" + text, true
	})
	h.live.finishBudget = 10 * time.Millisecond

	block := strings.Repeat("A settled sentence about the code. ", 500)
	start := time.Now()
	got := h.live.finish(block)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("finish took %s, want it bounded by the budget", elapsed)
	}
	if !strings.HasPrefix(got, "[ko]") {
		t.Fatalf("finish = %q…, want the first chunk translated", got[:40])
	}
	if joined := strings.ReplaceAll(got, "[ko]", ""); joined != block {
		t.Fatal("finish lost source text when the budget ran out")
	}
}

func TestLiveReasoningFinishKeepsSourceOnRefusal(t *testing.T) {
	h := newLiveHarness(t, func(string) (string, bool) { return "", false })
	if got := h.live.finish("이미 한국어인 최종 추론입니다."); got != "이미 한국어인 최종 추론입니다." {
		t.Fatalf("finish = %q, want the original text", got)
	}
}

func TestCoveredPrefixStopsAtTheFirstRealMismatch(t *testing.T) {
	if _, ok := coveredPrefix("a totally different block", "streamed text"); ok {
		t.Fatal("coveredPrefix accepted a text that does not start with the source")
	}
	if _, ok := coveredPrefix("short", "a much longer streamed source"); ok {
		t.Fatal("coveredPrefix accepted a final text shorter than the source")
	}
	if _, ok := coveredPrefix("anything", "   \n  "); ok {
		t.Fatal("coveredPrefix accepted a blank source")
	}
	// Whitespace differences are not mismatches, and the cut lands right after
	// the last matched byte so the separator rides along with the tail.
	n, ok := coveredPrefix("one two.\n\nthree", "one\ntwo.")
	if !ok || n != len("one two.") {
		t.Fatalf("coveredPrefix = %d, %v; want %d, true", n, ok, len("one two."))
	}
}

func TestJoinRenderedChunkRestoresTheSeam(t *testing.T) {
	// A sentence-boundary cut carries its trailing space, but a translator may
	// trim it; without the guard two sentences would run together.
	if got := joinRenderedChunk("첫 문장입니다.", "다음 문장입니다."); got != "첫 문장입니다. 다음 문장입니다." {
		t.Fatalf("join = %q", got)
	}
	if got := joinRenderedChunk("첫 줄\n", "다음 줄"); got != "첫 줄\n다음 줄" {
		t.Fatalf("join across a line break = %q", got)
	}
}
