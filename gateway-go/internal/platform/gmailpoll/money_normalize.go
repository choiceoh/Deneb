// money_normalize.go — deterministic, LLM-free Korean money normalization for
// the deal-amount verification gate (FinAcumen ③ "source-traceable amounts").
//
// The deal extractor runs on RoleTiny (the smallest model), so its `Amount`
// field is hallucination-prone. Before a deal amount is frozen onto a wiki page
// and pinned as citable notebook evidence (wiki_mail_analysis.go), we check that
// the extracted figure actually appears in the source document — by INTEGER
// EQUIVALENCE, not substring, so "5,000,000원" / "500만원" / "5백만" / "₩5,000,000"
// all reduce to the same 5000000 and match regardless of how each was written.
//
// Over-block avoidance is the #1 priority: if a figure can't be parsed
// unambiguously, the caller passes it through with a flag rather than blanking a
// possibly-correct amount. This file only ever *recognizes* values; the decision
// to blank/flag lives in dealInfoFromExtract.
package gmailpoll

import (
	"regexp"
	"strings"
)

// Korean myriad/decimal unit multipliers. Korean groups by 10^4 (만), so a
// composite like "1억2천만" is 1*1e8 + 2*1e3*1e4 = 120,000,000.
const (
	unitEok   = 100_000_000 // 억  10^8
	unitMan   = 10_000      // 만  10^4
	unitCheon = 1_000       // 천  10^3
	unitBaek  = 100         // 백  10^2
)

// moneyTokenRe finds candidate money tokens in free text: a digit run (with
// optional thousands separators / decimal) optionally followed by Korean unit
// words, OR a pure Korean-unit amount. Currency adornments (₩ ￦ KRW 원 달러) are
// matched loosely around the number so they don't break the digit capture; the
// parser re-reads the matched slice for units. We deliberately keep this greedy
// on units (억천만 etc.) so "1억2천만" is one token, not three.
//
// Two alternations:
//  1. digits + optional fractional + optional Korean unit suffix (5,000,000 /
//     5,000,000원 / 5백만 / 5천만 / 1억2천만 / 5억)
//  2. (covered by 1 since the leading number is required) — a bare unit word
//     with no number is not a money figure, so we require at least one digit.
var moneyTokenRe = regexp.MustCompile(`[0-9][0-9,]*(?:\.[0-9]+)?\s*(?:억\s*)?(?:[0-9,]*\s*천\s*)?(?:[0-9,]*\s*만\s*)?(?:[0-9,]*\s*백\s*)?`)

// unitChunkRe pulls "<number><unit>" pairs out of a normalized (comma-free,
// space-free) money token so we can expand Korean units compositionally. A
// number may be absent before a unit (e.g. "억" alone means 1억), which we treat
// as an implicit 1.
var unitChunkRe = regexp.MustCompile(`([0-9]*)(억|천만|백만|천|만|백)`)

// normalizeMoneyToInt converts a single money string to its integer KRW value.
// Returns (value, true) only when the parse is UNAMBIGUOUS; (0, false) when the
// token is empty, malformed, or otherwise can't be reduced to one number — the
// caller must treat false as "don't block" (over-block guard), never as zero.
//
// Handles: plain "5,000,000" / "5000000", currency-adorned "₩5,000,000" /
// "KRW 5,000,000" / "5,000,000원", and Korean-unit "500만원" / "5백만" / "5천만" /
// "5억" / "1억2천만" / "1억 2천만".
func normalizeMoneyToInt(s string) (int64, bool) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return 0, false
	}
	// Strip currency symbols/words and whitespace, then require that EVERY
	// remaining rune is a digit, separator, dot, or one of the four unit chars.
	// If anything else survives (hangul number words like "오백", stray letters,
	// "약"/"미정"), the token is ambiguous → return false so the caller's
	// over-block guard keeps the original rather than us guessing a wrong value.
	// (Silently dropping "오" from "오백만" would mis-read it as 1,000,000.)
	raw = stripCurrencyAdornments(raw)
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9', r == ',', r == '.':
			b.WriteRune(r)
		case r == '억' || r == '천' || r == '만' || r == '백':
			b.WriteRune(r)
		case r == ' ' || r == '\t':
			// allowed separator between number and unit ("1억 2천만"); skip it
		default:
			return 0, false // unrecognized residue → ambiguous, do not block
		}
	}
	cleaned := strings.TrimSpace(b.String())
	if cleaned == "" {
		return 0, false
	}

	hasUnit := strings.ContainsAny(cleaned, "억천만백")
	if !hasUnit {
		// Pure number path: "5,000,000" / "5000000". A decimal point on a pure
		// number is ambiguous as KRW (5,000,000.50?) so reject it — over-block
		// guard prefers pass+flag over a wrong block, and the caller does exactly
		// that on (0,false).
		digits := strings.ReplaceAll(cleaned, ",", "")
		if strings.Contains(digits, ".") {
			return 0, false
		}
		return parsePlainDigits(digits)
	}
	return expandKoreanUnits(cleaned)
}

// expandKoreanUnits reduces a comma/space-free Korean-unit money string to an
// integer by summing each "<number><unit>" chunk. A trailing plain-number tail
// (rare, e.g. "5만3000") is added as ones. Returns false if any residue can't be
// accounted for, so a malformed mix never silently produces a confident value.
func expandKoreanUnits(cleaned string) (int64, bool) {
	work := strings.ReplaceAll(cleaned, ",", "")
	var total int64
	matched := false
	// Walk left to right, consuming "<num?><unit>" chunks. idx tracks how far we
	// got; any leftover after the loop means an unexpected shape (→ ambiguous).
	idx := 0
	for idx < len(work) {
		loc := unitChunkRe.FindStringSubmatchIndex(work[idx:])
		if loc == nil || loc[0] != 0 {
			break
		}
		numStr := work[idx+loc[2] : idx+loc[3]]
		unit := work[idx+loc[4] : idx+loc[5]]
		var n int64 = 1
		if numStr != "" {
			v, ok := parsePlainDigits(numStr)
			if !ok {
				return 0, false
			}
			n = v
		}
		total += n * unitMultiplier(unit)
		matched = true
		idx += loc[1]
	}
	// Any trailing pure-digit tail ("5만3000" → +3000).
	if idx < len(work) {
		tail := work[idx:]
		if !isAllDigits(tail) {
			return 0, false // leftover non-digit residue → ambiguous
		}
		v, ok := parsePlainDigits(tail)
		if !ok {
			return 0, false
		}
		total += v
		matched = true
	}
	if !matched || total <= 0 {
		return 0, false
	}
	return total, true
}

// unitMultiplier maps a unit word (already chunked) to its KRW multiplier.
// "천만" and "백만" are common compound units that the chunker keeps whole.
func unitMultiplier(unit string) int64 {
	switch unit {
	case "억":
		return unitEok
	case "천만":
		return unitCheon * unitMan // 10^7
	case "백만":
		return unitBaek * unitMan // 10^6
	case "천":
		return unitCheon
	case "만":
		return unitMan
	case "백":
		return unitBaek
	default:
		return 1
	}
}

// currencyAdornments are the currency markers stripped before reading the
// number underneath. Latin codes cover the common casings the model emits.
var currencyAdornments = []string{"₩", "￦", "원", "달러", "KRW", "krw", "Krw", "USD", "usd", "Usd"}

// stripCurrencyAdornments removes currency markers (₩ ￦ KRW 원 달러) so the
// number underneath can be read.
func stripCurrencyAdornments(s string) string {
	out := s
	for _, tok := range currencyAdornments {
		out = strings.ReplaceAll(out, tok, "")
	}
	return out
}

// parsePlainDigits parses a comma-free, sign-free digit string into an int64.
// Returns false on empty, non-digit, or overflow.
func parsePlainDigits(s string) (int64, bool) {
	s = strings.ReplaceAll(s, ",", "")
	if s == "" || !isAllDigits(s) {
		return 0, false
	}
	var v int64
	for _, r := range s {
		d := int64(r - '0')
		// Overflow guard: KRW deal amounts never approach int64 max, but a
		// runaway digit run should fail closed (→ false → pass+flag) not wrap.
		if v > (1<<62)/10 {
			return 0, false
		}
		v = v*10 + d
	}
	return v, true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sourceMoneyValues scans free text (the attachment + analysis source) and
// returns the set of integer KRW values it can confidently read. Used as the
// membership set the extracted amount must belong to. Best-effort: tokens that
// don't parse cleanly are skipped (they can't anchor a match anyway).
//
// This set is intentionally PERMISSIVE: bare digit runs from dates/phones/
// percentages (2026, 110, 10, …) also land here. That only ever makes the gate
// more lenient — a noise value can cause a rare false *pass*, never a false
// *block* — which is the correct bias given over-block avoidance is the #1
// goal. Real deal amounts dwarf such noise, so a hallucinated figure colliding
// with, say, a year is a negligible residual we accept rather than risk
// blanking a correct amount by filtering the source.
func sourceMoneyValues(text string) map[int64]struct{} {
	out := make(map[int64]struct{})
	if strings.TrimSpace(text) == "" {
		return out
	}
	for _, tok := range moneyTokenRe.FindAllString(text, -1) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if v, ok := normalizeMoneyToInt(tok); ok {
			out[v] = struct{}{}
		}
	}
	return out
}

// amountFoundInSource reports whether the extracted amount's integer value
// appears in the source text's money value set. The bool `parsed` is false when
// the EXTRACTED amount itself couldn't be normalized — the caller then applies
// the over-block guard (pass + flag) instead of blanking.
//
// Contract:
//   - parsed=false           → extracted amount ambiguous; caller passes it through.
//   - parsed=true, found=true → amount corroborated by source; keep as-is.
//   - parsed=true, found=false→ amount not in source (hallucination); blank + flag.
func amountFoundInSource(amount, source string) (found, parsed bool) {
	want, ok := normalizeMoneyToInt(amount)
	if !ok {
		return false, false // can't parse extracted amount → don't block
	}
	values := sourceMoneyValues(source)
	if _, hit := values[want]; hit {
		return true, true
	}
	return false, true
}
