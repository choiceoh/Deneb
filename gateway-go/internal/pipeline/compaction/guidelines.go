package compaction

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// MaxLearnedGuidelines caps how many learned preservation rules ride in the
// summarizer prompt. Each adds a line; too many dilute the signal (same
// reasoning as the anchor cap). When over the cap the first entries are kept —
// the refinement task orders newest-first.
const MaxLearnedGuidelines = 5

// maxGuidelineRunes bounds a single guideline so a runaway LLM proposal can't
// bloat the prompt.
const maxGuidelineRunes = 160

// GuidelineFileName is the learned-guidelines file basename. Callers join it
// onto the resolved state dir (DENEB_STATE_DIR-aware) so the reader (chat) and
// writer (tuner) agree on one path and dev/prod stay isolated.
const GuidelineFileName = "compaction-guidelines.json"

// GuidelineStore persists the learned compaction guidelines as a JSON string
// array. Read per-run (a tiny file) and rewritten by the refinement task.
type GuidelineStore struct {
	path string
	mu   sync.Mutex
}

// NewGuidelineStore returns a store backed by the given JSON file path.
func NewGuidelineStore(path string) *GuidelineStore { return &GuidelineStore{path: path} }

// Load returns the stored guidelines, sanitized and capped. A missing or
// invalid file yields nil — the feature is simply inactive, never an error.
func (s *GuidelineStore) Load() []string {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil
	}
	var raw []string
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	return sanitizeGuidelines(raw)
}

// Save sanitizes, caps, and writes the guidelines atomically.
func (s *GuidelineStore) Save(guidelines []string) error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(sanitizeGuidelines(guidelines), "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(s.path, data, &atomicfile.Options{Perm: 0o644})
}

// placeholderGuideline matches schema-echo junk the tuner LLM occasionally
// returns verbatim ("guideline1", "예시 2", …). Live 2026-07-06: two such
// entries persisted and rode every summarizer prompt until noticed.
var placeholderGuideline = regexp.MustCompile(`(?i)^(?:guideline|rule|example|지침|규칙|예시)\s*[\d.:_-]*$`)

// junkGuideline reports whether g is a placeholder or otherwise cannot be a
// real preservation directive. The audit prompt mandates Korean "~보존하라"
// one-liners, so an entry with no Hangul at all is schema noise, not guidance.
func junkGuideline(g string) bool {
	if placeholderGuideline.MatchString(g) {
		return true
	}
	for _, r := range g {
		if unicode.Is(unicode.Hangul, r) {
			return false
		}
	}
	return true
}

// sanitizeGuidelines trims, drops empties and placeholder junk, truncates
// over-long entries, dedups (exact AND near-duplicate), and caps to
// MaxLearnedGuidelines keeping the first. Runs on Load AND Save, so junk
// already persisted is filtered out the next time anything reads the store.
//
// Near-dup dedup (2026-07-19): exact-string dedup let reworded siblings of the
// same rule ("금액은 …통화로 보존" vs "단가·예산 등 금액은 …단위를 보존") each
// take a slot, so the 5-slot cap churned between phrasings of 2 concepts
// instead of holding 5 distinct ones. Now a candidate is dropped when its
// content words overlap an already-kept guideline past a threshold — the
// tuner orders newest-first and we keep the first, so the freshest phrasing
// of a concept wins and the freed slots go to genuinely new categories.
func sanitizeGuidelines(in []string) []string {
	out := make([]string, 0, len(in))
	keptTokens := make([][]string, 0, len(in))
	for _, g := range in {
		g = strings.TrimSpace(g)
		if g == "" || junkGuideline(g) {
			continue
		}
		if r := []rune(g); len(r) > maxGuidelineRunes {
			g = strings.TrimSpace(string(r[:maxGuidelineRunes]))
		}
		toks := guidelineContentTokens(g)
		dup := false
		for _, kt := range keptTokens {
			if guidelineOverlap(toks, kt) >= guidelineDupThreshold {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		out = append(out, g)
		keptTokens = append(keptTokens, toks)
		if len(out) >= MaxLearnedGuidelines {
			break
		}
	}
	return out
}

// guidelineDupThreshold is the content-word overlap coefficient above which two
// guidelines are treated as the same rule. 0.5 merges "금액…통화" with
// "단가·예산…금액…단위" (share 금액·숫자) while keeping "금액 보존" and
// "날짜 보존" (share nothing) distinct.
const guidelineDupThreshold = 0.5

// guidelineStopwords are the boilerplate that appears across nearly every
// "~를 정확히 보존하라" guideline; counting them would make unrelated rules look
// similar, so they are dropped before overlap is measured.
var guidelineStopwords = map[string]struct{}{
	"정확한": {}, "정확히": {}, "구체적인": {}, "구체적으로": {}, "보존하라": {}, "보존": {},
	"하라": {}, "남기지": {}, "말고": {}, "등": {}, "만": {}, "직책이나": {}, "호칭만": {},
	"값을": {}, "값": {}, "그대로": {},
}

// guidelineContentTokens splits a guideline into meaningful content words:
// lowercased, punctuation-stripped, boilerplate removed, >=2 runes. Korean
// particles are handled by the prefix match in guidelineOverlap, not here.
func guidelineContentTokens(g string) []string {
	fields := strings.FieldsFunc(strings.ToLower(g), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	toks := make([]string, 0, len(fields))
	for _, f := range fields {
		if len([]rune(f)) < 2 {
			continue
		}
		if _, stop := guidelineStopwords[f]; stop {
			continue
		}
		toks = append(toks, f)
	}
	return toks
}

// guidelineOverlap is the overlap coefficient |a∩b| / min(|a|,|b|). Two tokens
// match when one is a prefix of the other (>=2 runes) — this absorbs Korean
// particles ("금액" <-> "금액은") without a morphological dictionary. Empty on
// either side yields 0 (a value-free guideline never suppresses a real one).
func guidelineOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	matches := 0
	for _, ta := range a {
		for _, tb := range b {
			if tokenAkin(ta, tb) {
				matches++
				break
			}
		}
	}
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	return float64(matches) / float64(minLen)
}

// tokenAkin reports whether two content tokens denote the same word up to a
// Korean particle: equal, or one a prefix of the other with the shorter >=2 runes.
func tokenAkin(a, b string) bool {
	if a == b {
		return true
	}
	ra, rb := []rune(a), []rune(b)
	short, long := ra, rb
	if len(rb) < len(ra) {
		short, long = rb, ra
	}
	if len(short) < 2 {
		return false
	}
	return strings.HasPrefix(string(long), string(short))
}
