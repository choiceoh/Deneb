// Package tokenest provides model-family-aware token count estimation.
//
// Unlike naive rune/N or byte/N estimators, tokenest performs Unicode script
// analysis and applies per-script calibration constants derived from BPE
// tokenizer behavior. Estimators are model-family aware: Claude, OpenAI,
// and Gemini tokenizers have different efficiencies per script.
//
// Usage:
//
//	// Quick estimate with default (Korean-weighted) calibration:
//	n := tokenest.Estimate("안녕 Hello 세계")
//
//	// Model-specific estimate:
//	est := tokenest.ForModel("claude-sonnet-4.6")
//	n := est.Count("안녕 Hello 세계")
//
//	// Raw byte estimate (for JSON payloads, no UTF-8 decode):
//	n := tokenest.EstimateBytes(jsonPayload)
package tokenest

import (
	"strings"
	"unicode"
)

// ── Model families ──────────────────────────────────────────────────────

// Family identifies a tokenizer family with distinct BPE vocabulary
// characteristics. Each family has per-script calibration constants
// derived from empirical measurements.
type Family int

const (
	FamilyClaude  Family = iota // Anthropic models (Claude tokenizer)
	FamilyOpenAI                // GPT models (cl100k_base / o200k_base)
	FamilyGemini                // Google Gemini/Gemma models (SentencePiece)
	FamilyDefault               // Conservative default (Korean-weighted)
)

// ── Script classification ───────────────────────────────────────────────

// scriptClass categorizes Unicode runes for per-script token estimation.
type scriptClass int

const (
	classLatin  scriptClass = iota // a-z, A-Z
	classHangul                    // Korean syllable blocks + Jamo
	classCJK                       // CJK ideographs, Hiragana, Katakana
	classDigit                     // 0-9
	classSpace                     // whitespace
	classPunct                     // punctuation and symbols
	classOther                     // emoji, misc Unicode
	numClasses                     // sentinel — must be last
)

// ── Per-family calibration ──────────────────────────────────────────────
//
// Each value is "runes per token" for that script class.
// Higher value = better BPE compression = fewer tokens.
//
// Calibrated against representative Korean/English/mixed samples:
//   - Korean prose: 한국어 위키백과 문서 (~3000 char sample)
//   - English prose: Wikipedia articles (~3000 char sample)
//   - Mixed: Deneb system prompt (Korean + English + JSON)
//   - Code: Go source files with English identifiers
//
// Conservative direction: slight under-estimate of runes-per-token
// (= slight over-count of tokens) so budget decisions are safe.

var familyRatios = [4][numClasses]float64{
	// FamilyClaude — Anthropic's tokenizer has strong Korean/CJK coverage.
	//  Latin 4.0: common English words are single tokens ("the"=1, "function"=1)
	//  Hangul 1.5: frequent syllable pairs merge (e.g. 하세→1 token); rare ones split
	//  CJK 1.3: common ideographs are single tokens; rare ones split into 2
	//  Digit 2.5: short number runs merge ("2024"=1-2 tokens)
	//  Space 2.0: spaces often merge with adjacent text
	//  Punct 1.0: mostly individual tokens
	//  Other 1.0: emoji/misc are individual tokens
	{4.0, 1.5, 1.3, 2.5, 2.0, 1.0, 1.0},

	// FamilyOpenAI — cl100k_base/o200k_base, weaker CJK coverage than Claude.
	{4.0, 1.2, 1.2, 2.5, 2.0, 1.0, 1.0},

	// FamilyGemini — SentencePiece-based, decent multilingual coverage.
	{4.0, 1.4, 1.3, 2.5, 2.0, 1.0, 1.0},

	// FamilyDefault — the locally served GLM-5.3 tokenizer, which is what this
	// deployment actually sends: six of seven model roles resolve to the local
	// engine (docs/agent-rules/model-roles.md), so "unknown model" here means
	// GLM far more often than it means anything else.
	//
	// Measured 2026-09-13 against the tokenizer the ST engine boots with
	// (st-glm53-meta/tokenizer.json, vocab 154,856), over Deneb's own prompt
	// material: context files, persisted prompt snapshots, the rendered tool
	// schema head and real transcripts. Re-derive by tokenizing that corpus and
	// fitting counts-per-class, then biasing so the estimate lands slightly
	// high: a budget that over-counts compacts early, a budget that
	// under-counts overflows the window.
	//
	// Hangul is the one class worth sweeping rather than fitting. The raw fit
	// against Deneb's own corpus says 0.86, but that corpus is business wiki
	// and prompt snapshots — proper nouns, figures and markdown, which the BPE
	// splits hard. Ordinary Korean prose merges far better (about 1.2), so a
	// ratio tuned to the corpus over-counts a plain sentence by 58%. 0.9 is the
	// value that still never under-counts either one: corpus 1.02, plain
	// sentences 1.05, worst single string 1.41.
	//
	// The previous row {3.5, 1.3, 1.2, 2.0, 1.5, 1.0, 1.0} was inherited from
	// Claude-family assumptions and was wrong per script in both directions at
	// once: Korean costs MORE than one token per syllable here (not 1.3 syllables
	// per token), Latin and whitespace cost far less. On real prompt material it
	// over-counted by 15% overall and by 69% at p90 — the dispersion, not the
	// mean, is what made budget decisions unpredictable.
	//
	//  cjk/other keep the old values: the corpus held under 1,000 runes of each,
	//  which is not enough to fit.
	{4.2, 0.9, 1.2, 0.95, 7.5, 1.6, 1.0},
}

// ── Estimator ───────────────────────────────────────────────────────────

// Estimator performs model-family-aware token estimation.
type Estimator struct {
	family Family
}

// defaultEst is the package-level estimator (FamilyDefault).
var defaultEst = Estimator{family: FamilyDefault}

// Estimate returns estimated token count using the default estimator
// (conservative, Korean-weighted). This is the primary entry point for
// callers that don't know or need model-specific calibration.
func Estimate(text string) int {
	return defaultEst.Count(text)
}

// EstimateUncalibrated uses the fixed built-in ratios and deliberately ignores
// process-global feedback. Deterministic evaluators use it to avoid run-order
// dependence while production continues benefiting from self-calibration.
func EstimateUncalibrated(text string) int {
	return defaultEst.rawCount(text)
}

// EstimateBytes returns estimated token count from raw bytes without
// full UTF-8 rune iteration. Uses a byte-level heuristic: samples the
// byte stream to detect ASCII vs multi-byte ratio and applies an
// appropriate divisor.
//
// Use this for JSON payloads or raw message bytes where rune-level
// analysis is unnecessary overhead.
func EstimateBytes(data []byte) int {
	return defaultEst.CountBytes(data)
}

// EstimateBytesUncalibrated is the byte-oriented fixed-ratio counterpart of
// EstimateUncalibrated.
func EstimateBytesUncalibrated(data []byte) int {
	return defaultEst.rawCountBytes(data)
}

// ForModel returns an estimator calibrated for the given model ID.
// Model ID matching is case-insensitive and substring-based.
//
// Examples:
//
//	ForModel("claude-sonnet-4.6")       → FamilyClaude
//	ForModel("gpt-5.4-mini")            → FamilyOpenAI
//	ForModel("gemini-3.1-pro-preview")  → FamilyGemini
//	ForModel("unknown-model")           → FamilyDefault
func ForModel(modelID string) *Estimator {
	return &Estimator{family: resolveFamily(modelID)}
}

// ForFamily returns an estimator for a specific tokenizer family.
func ForFamily(f Family) *Estimator {
	return &Estimator{family: f}
}

// Count returns estimated token count for text using Unicode script analysis.
// Scans runes once, classifying each into a script class, then applies
// the per-class runes-per-token ratio for this estimator's family.
//
// If self-calibration data is available (from RecordFeedback), a learned
// correction factor is applied transparently.
func (e *Estimator) Count(text string) int {
	n := e.rawCount(text)
	if factor := globalCal.factor(e.family); factor != 1.0 {
		n = int(float64(n)*factor + 0.5)
	}
	if n < 1 && len(text) > 0 {
		return 1
	}
	return n
}

// rawCount performs the pure heuristic estimate without calibration.
func (e *Estimator) rawCount(text string) int {
	if len(text) == 0 {
		return 0
	}

	var counts [numClasses]int
	for _, r := range text {
		counts[classifyRune(r)]++
	}

	ratios := &familyRatios[e.family]
	var tokens float64
	for i := 0; i < int(numClasses); i++ {
		if counts[i] > 0 {
			tokens += float64(counts[i]) / ratios[i]
		}
	}

	n := int(tokens + 0.5)
	if n < 1 {
		return 1
	}
	return n
}

// CountBytes estimates tokens from raw bytes without full UTF-8 decoding.
// Samples up to 512 bytes to detect the ASCII vs multi-byte ratio,
// then picks the appropriate bytes-per-token divisor.
//
// Self-calibration correction is applied when available.
func (e *Estimator) CountBytes(data []byte) int {
	n := e.rawCountBytes(data)
	if factor := globalCal.factor(e.family); factor != 1.0 {
		n = int(float64(n)*factor + 0.5)
	}
	if n < 1 && len(data) > 0 {
		return 1
	}
	return n
}

// rawCountBytes performs the byte heuristic without calibration.
func (e *Estimator) rawCountBytes(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	divisor := byteDivisor(data)
	n := int(float64(len(data))/divisor + 0.5)
	if n < 1 {
		return 1
	}
	return n
}

// Family returns this estimator's tokenizer family.
func (e *Estimator) Family() Family {
	return e.family
}

// ── Internals ───────────────────────────────────────────────────────────

// classifyRune maps a rune to its script class. Fast-path checks for
// ASCII ranges first (covers the majority of code/JSON content).
func classifyRune(r rune) scriptClass {
	// Fast path: ASCII range (covers ~70%+ of typical content).
	if r < 0x80 {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			return classLatin
		case r >= '0' && r <= '9':
			return classDigit
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			return classSpace
		default:
			return classPunct // ASCII punctuation and symbols
		}
	}
	// Hangul (Korean).
	if isHangul(r) {
		return classHangul
	}
	// CJK (Chinese, Japanese).
	if isCJK(r) {
		return classCJK
	}
	// Unicode whitespace beyond ASCII.
	if unicode.IsSpace(r) {
		return classSpace
	}
	// Unicode punctuation/symbols.
	if unicode.IsPunct(r) || unicode.IsSymbol(r) {
		return classPunct
	}
	return classOther
}

// isHangul returns true for Korean syllable blocks and Jamo ranges.
func isHangul(r rune) bool {
	return (r >= 0xAC00 && r <= 0xD7A3) || // Hangul Syllables
		(r >= 0x1100 && r <= 0x11FF) || // Hangul Jamo
		(r >= 0x3130 && r <= 0x318F) || // Hangul Compatibility Jamo
		(r >= 0xA960 && r <= 0xA97F) || // Hangul Jamo Extended-A
		(r >= 0xD7B0 && r <= 0xD7FF) // Hangul Jamo Extended-B
}

// isCJK returns true for CJK ideographs, Hiragana, and Katakana.
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility Ideographs
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols and Punctuation
		(r >= 0x3040 && r <= 0x309F) || // Hiragana
		(r >= 0x30A0 && r <= 0x30FF) // Katakana
}

// byteDivisor samples raw bytes to estimate the bytes-per-token ratio.
// Multi-byte (high-bit) bytes indicate CJK/Korean content, which has
// a different bytes-per-token ratio than ASCII.
func byteDivisor(data []byte) float64 {
	// Sample up to 512 bytes.
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	multiByte := 0
	for _, b := range sample {
		if b >= 0x80 {
			multiByte++
		}
	}
	ratio := float64(multiByte) / float64(len(sample))

	// Bytes per token FALLS as multi-byte content rises, it does not rise.
	// A Hangul syllable is 3 UTF-8 bytes and costs MORE than one token, so it
	// is ~2.2 bytes per token; an ASCII run is 1 byte per rune and several
	// runes per token, so it is ~3.5. This function used to interpolate
	// 4.0 → 4.5 in the wrong direction, which under-counted Korean by 47% and
	// every prompt by 29% overall (measured 2026-09-13 against the engine's own
	// tokenizer). That error fed the calibration loop below as if it were
	// tokenizer drift.
	//
	// Shape follows the cause: split the bytes into an ASCII part and a
	// multi-byte part (3 bytes per rune), price each at its own runes-per-token,
	// and return the effective divisor so rawCountBytes stays a single division.
	asciiPart := (1 - ratio) / bytesASCIIRunesPerToken
	multiPart := ratio / (utf8BytesPerCJKRune * bytesMultibyteRunesPerToken)
	if per := asciiPart + multiPart; per > 0 {
		return 1 / per
	}
	return bytesASCIIRunesPerToken
}

// Constants for byteDivisor, measured with the rest of the calibration above.
// They are deliberately separate from familyRatios: this path never decodes
// UTF-8, so it can only tell ASCII from not-ASCII and has to price each half
// as a blend of the script classes that actually appear there.
const (
	// These are tuned for ACCURACY, not for the conservative bias the rune
	// ratios carry: this path is what recordTokenFeedback measures with, and a
	// deliberate bias here would be learned by the calibrator as if it were
	// tokenizer drift. Measured over the same corpus: total 1.00, p90 1.16.
	bytesASCIIRunesPerToken     = 3.7  // latin + space + punct + digit, blended
	bytesMultibyteRunesPerToken = 0.85 // Hangul dominates; CJK is close
	utf8BytesPerCJKRune         = 3.0  // Hangul and CJK are 3 bytes in UTF-8
)

// resolveFamily maps a model ID to its tokenizer family.
func resolveFamily(modelID string) Family {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "claude"):
		return FamilyClaude
	case strings.HasPrefix(lower, "gpt-"),
		strings.Contains(lower, "codex"),
		strings.HasPrefix(lower, "o1"),
		strings.HasPrefix(lower, "o3"),
		strings.HasPrefix(lower, "o4"):
		return FamilyOpenAI
	case strings.Contains(lower, "gemini"),
		strings.Contains(lower, "gemma"):
		return FamilyGemini
	default:
		// GLM (the locally served family) lands here on purpose: FamilyDefault
		// IS the GLM calibration. Naming it in a case of its own would suggest
		// the fallthrough is unrelated, and the next model swap would silently
		// get Claude-shaped ratios again.
		return FamilyDefault
	}
}
