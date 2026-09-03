package textutil

import "unicode"

// HangulRatio reports the share of letters that are Hangul, or 0 when there are
// no letters at all. Code, punctuation and digits are ignored so a block that is
// Korean prose around English identifiers still reads as Korean.
//
// Callers use it to decide "is this already Korean, leave it alone". The bar
// they compare against is deliberately low (0.30): identifiers count as English
// letters, so a Korean sentence naming one symbol — "이 함수는 `GetAttachment`
// 를 호출한다" — measures only ~0.41, and a bar near 0.5 would keep
// re-translating ordinary Korean prose about code.
func HangulRatio(s string) float64 {
	var letters, hangul int
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.Is(unicode.Hangul, r) {
			hangul++
		}
	}
	if letters == 0 {
		return 0
	}
	return float64(hangul) / float64(letters)
}
