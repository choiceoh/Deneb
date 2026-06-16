package hanja

import "strings"

// Streamer transliterates Han→Hangul across a token stream, carrying the small
// amount of state that a chunk boundary can split: whether we're inside a code
// fence / inline code span, whether the previous source rune was a Hanja (for
// 두음법칙 word-run detection), and a run of trailing backticks held back until we
// can tell a fence (```), an inline delimiter (`), or a literal apart.
//
// Usage: feed each delta through [Streamer.Write] (returns the transformed text to
// forward), then call [Streamer.Flush] once at end of stream to release any held
// backticks. A run is single-threaded (one turn emits deltas sequentially), so a
// Streamer needs no locking. [Transliterate] wraps this for whole strings.
type Streamer struct {
	inFence  bool
	inInline bool
	prevHan  bool
	ticks    int // consecutive trailing backticks awaiting resolution
}

// NewStreamer returns a fresh transliterating stream transformer.
func NewStreamer() *Streamer { return &Streamer{} }

// Write transforms one delta and returns the text to forward. Trailing backticks
// are held back (a fence marker may continue into the next delta) and released on
// the following Write or on Flush.
func (s *Streamer) Write(delta string) string {
	if delta == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(delta))
	for _, r := range delta {
		if r == '`' {
			s.ticks++
			s.prevHan = false // a backtick breaks a Hanja run
			continue
		}
		if s.ticks > 0 {
			s.resolveTicks(&b)
		}
		s.emit(&b, r)
	}
	return b.String()
}

// Flush releases any backticks held at end of stream. Call once when the turn's
// text is complete; after Flush the Streamer should not be reused.
func (s *Streamer) Flush() string {
	if s.ticks == 0 {
		return ""
	}
	var b strings.Builder
	s.resolveTicks(&b)
	return b.String()
}

// emit writes one non-backtick rune, transliterating a Hanja to its reading when
// outside code, with 두음법칙 on the first Hanja of a run.
func (s *Streamer) emit(b *strings.Builder, r rune) {
	if !isHanIdeograph(r) {
		b.WriteRune(r)
		s.prevHan = false
		return
	}
	// Inside code, Han characters are code/data — pass through verbatim.
	if s.inFence || s.inInline {
		b.WriteRune(r)
		s.prevHan = true
		return
	}
	if reading, ok := readings[r]; ok {
		if !s.prevHan { // word-initial → apply 두음법칙
			reading = applyDueum(reading)
		}
		b.WriteRune(reading)
	} else {
		b.WriteRune(r) // no known reading (rare ext/archaic) → leave as-is
	}
	s.prevHan = true
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
