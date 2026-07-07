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
// over-long entries, dedups (case-insensitive), and caps to
// MaxLearnedGuidelines keeping the first. Runs on Load AND Save, so junk
// already persisted is filtered out the next time anything reads the store.
func sanitizeGuidelines(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, g := range in {
		g = strings.TrimSpace(g)
		if g == "" || junkGuideline(g) {
			continue
		}
		if r := []rune(g); len(r) > maxGuidelineRunes {
			g = strings.TrimSpace(string(r[:maxGuidelineRunes]))
		}
		key := strings.ToLower(g)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, g)
		if len(out) >= MaxLearnedGuidelines {
			break
		}
	}
	return out
}
