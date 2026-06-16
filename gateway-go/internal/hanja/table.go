// Package hanja transliterates Han characters (한자) embedded in otherwise-Korean
// model output into their Sino-Korean Hangul readings (報告書 → 보고서), applying
// 두음법칙 at word-initial position (旅行 → 여행, 女子 → 여자).
//
// Why this exists: Deneb routes several roles to Chinese-lineage models (GLM,
// MiMo, DeepSeek). They occasionally write Sino-Korean vocabulary in Hanja
// instead of Hangul, which reads as Chinese to a Korean user. Hanja→Hangul is a
// deterministic per-character reading lookup (NOT translation), so it needs no
// model and no sentence context — a Hanja's Korean reading is a fixed single
// syllable. That makes it safe to apply even mid-stream, token by token
// ([Streamer]). It is applied only to user-facing prose (chat assistant text,
// analysis reports), never to internal JSON the gateway parses.
//
// Scope/limits: this reads Hanja as Korean; it does NOT translate actual Chinese
// sentences (그건 NMT 영역). Code fences / inline code / URLs are left untouched
// (Han there is code/data, not prose). 두음법칙 uses a word-initial heuristic
// (the first Hanja of a consecutive run), which is correct for the common
// compounds but can miss morpheme-internal cases (新女性 → 신녀성, not 신여성).
package hanja

import (
	_ "embed"
	"strconv"
	"strings"
)

//go:embed readings.tsv
var readingsTSV string

// readings maps a Han codepoint to its dominant Sino-Korean reading (exactly one
// Hangul syllable). Built once at package init from the embedded Unihan-derived
// table; see readings.tsv for provenance (Unicode Unihan kHangul, v17.0.0).
var readings = parseReadings(readingsTSV)

// parseReadings turns the embedded table into the lookup map. The body is
// whitespace-separated "<hex codepoint>:<Hangul syllable>" pairs packed many per
// line (with leading '#' comment lines), e.g. "5831:보 543F:고 …".
func parseReadings(tsv string) map[rune]rune {
	m := make(map[rune]rune, 10000)
	for _, line := range strings.Split(tsv, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || line[0] == '#' {
			continue
		}
		for _, tok := range strings.Fields(line) {
			cpStr, han, ok := strings.Cut(tok, ":")
			if !ok {
				continue
			}
			cp, err := strconv.ParseInt(cpStr, 16, 32)
			if err != nil {
				continue
			}
			reading := []rune(han)
			if len(reading) != 1 {
				continue // every Unihan kHangul reading is a single syllable; skip anomalies
			}
			m[rune(cp)] = reading[0]
		}
	}
	return m
}

// isHanIdeograph reports whether r is a CJK ideograph (used for word-run
// detection independent of whether the char has a known reading). Covers the
// Unified blocks + Extension A–F + compatibility ideographs.
func isHanIdeograph(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
		return true
	case r >= 0x3400 && r <= 0x4DBF: // Extension A
		return true
	case r >= 0xF900 && r <= 0xFAFF: // Compatibility Ideographs
		return true
	case r >= 0x20000 && r <= 0x2A6DF: // Extension B
		return true
	case r >= 0x2A700 && r <= 0x2EBEF: // Extension C–F
		return true
	case r >= 0x2F800 && r <= 0x2FA1F: // Compatibility Ideographs Supplement
		return true
	case r >= 0x30000 && r <= 0x323AF: // Extension G–H
		return true
	}
	return false
}
