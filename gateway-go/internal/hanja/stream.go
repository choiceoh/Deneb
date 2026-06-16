package hanja

import "strings"

// maxHanRun bounds the buffered Han run. Sino-Korean words are short (株式會社 is
// 4, 稅金計算書 5); a longer unbroken Han run is Chinese prose, so capping just
// keeps the buffer (and stream hold-back) tiny — the all-or-nothing decision in
// flushRun is what actually matters.
const maxHanRun = 16

// Streamer transliterates Han→Hangul across a token stream, carrying the state a
// chunk boundary can split: whether we're inside a code fence / inline code, a
// run of trailing backticks held until we can tell a fence (```) from an inline
// delimiter (`), and the current run of consecutive Han ideographs.
//
// A run is decided all-or-nothing: a Sino-Korean word reads only when EVERY
// character has a Korean reading, so a run that contains an unknown char
// (Simplified-Chinese 时/发, archaic) is treated as real Chinese and left whole
// rather than half-converted into "즉时发생" mush.
//
// Usage: feed each delta through [Streamer.Write] (returns the text to forward),
// then call [Streamer.Flush] once at end of stream to release any buffered run /
// backticks. A run is single-threaded (one turn emits deltas sequentially), so a
// Streamer needs no locking. [Transliterate] wraps this for whole strings.
type Streamer struct {
	inFence  bool
	inInline bool
	ticks    int    // consecutive trailing backticks awaiting resolution
	run      []rune // buffered consecutive Han ideographs awaiting an all-or-nothing decision
}

// NewStreamer returns a fresh transliterating stream transformer.
func NewStreamer() *Streamer { return &Streamer{} }

// Write transforms one delta and returns the text to forward. Trailing backticks
// and an in-progress Han run are held back (both can continue into the next
// delta) and released on a following Write or on Flush.
func (s *Streamer) Write(delta string) string {
	if delta == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(delta))
	for _, r := range delta {
		if r == '`' {
			s.flushRun(&b) // the Han run ends at the backtick
			s.ticks++
			continue
		}
		if s.ticks > 0 {
			s.resolveTicks(&b)
		}
		s.emit(&b, r)
	}
	return b.String()
}

// Flush releases any buffered Han run and held backticks at end of stream. Call
// once when the turn's text is complete; after Flush the Streamer is not reused.
func (s *Streamer) Flush() string {
	var b strings.Builder
	s.flushRun(&b)
	if s.ticks > 0 {
		s.resolveTicks(&b)
	}
	return b.String()
}

// emit buffers a Han ideograph (outside code) into the current run, or ends the
// run and writes any other rune. Han inside a code fence / inline span passes
// through verbatim (code/data, not Korean prose).
func (s *Streamer) emit(b *strings.Builder, r rune) {
	if isHanIdeograph(r) && !s.inFence && !s.inInline {
		s.run = append(s.run, r)
		if len(s.run) >= maxHanRun {
			s.flushRun(b)
		}
		return
	}
	s.flushRun(b)
	b.WriteRune(r)
}

// flushRun emits the buffered Han run: every character's reading when they all
// have one (with 두음법칙 on the run's first character), else the raw run.
func (s *Streamer) flushRun(b *strings.Builder) {
	if len(s.run) == 0 {
		return
	}
	allHave := true
	for _, r := range s.run {
		if _, ok := readings[r]; !ok {
			allHave = false
			break
		}
	}
	if allHave {
		for i, r := range s.run {
			reading := readings[r]
			if i == 0 { // word-initial → apply 두음법칙
				reading = applyDueum(reading)
			}
			b.WriteRune(reading)
		}
	} else {
		for _, r := range s.run {
			b.WriteRune(r) // real Chinese / archaic — leave the run whole
		}
	}
	s.run = s.run[:0]
}

// resolveTicks emits a completed run of backticks verbatim and toggles code state:
// ≥3 backticks toggle a fenced block, a lone backtick toggles an inline span (only
// when not already inside a fence). A run of 2 is left as a literal (rare “…“).
func (s *Streamer) resolveTicks(b *strings.Builder) {
	n := s.ticks
	s.ticks = 0
	for range n {
		b.WriteByte('`')
	}
	switch {
	case n >= 3:
		s.inFence = !s.inFence
		s.inInline = false // a fence boundary resets any inline state
	case n == 1 && !s.inFence:
		s.inInline = !s.inInline
	}
}
