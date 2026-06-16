package hanja

// Transliterate replaces Han characters in a complete string with their
// Sino-Korean Hangul readings (報告書 → 보고서), applying 두음법칙 at the start of each
// Han run and leaving code fences / inline code untouched. It shares the exact
// logic of the streaming path ([Streamer]), so the final persisted/synchronous
// text matches what was streamed token by token. Returns the input unchanged when
// it contains no Han characters (the common all-Korean case — no allocation).
func Transliterate(s string) string {
	if !ContainsHan(s) {
		return s
	}
	st := NewStreamer()
	return st.Write(s) + st.Flush()
}

// ContainsHan reports whether s has any CJK ideograph — the cheap guard the
// output hooks use to skip the transform on the all-Korean common case.
func ContainsHan(s string) bool {
	for _, r := range s {
		if isHanIdeograph(r) {
			return true
		}
	}
	return false
}
