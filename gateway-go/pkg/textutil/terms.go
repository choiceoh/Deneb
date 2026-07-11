package textutil

import "strings"

// LimitedTerms builds a case-insensitively unique, comma-separated term list
// under both count and byte limits. Add returns false once adding a new term
// would exceed either limit.
type LimitedTerms struct {
	maxTerms int
	maxChars int
	chars    int
	seen     map[string]struct{}
	terms    []string
}

// NewLimitedTerms initializes a bounded term collector.
func NewLimitedTerms(maxTerms, maxChars int) *LimitedTerms {
	return &LimitedTerms{
		maxTerms: maxTerms,
		maxChars: maxChars,
		seen:     make(map[string]struct{}),
		terms:    make([]string, 0, maxTerms),
	}
}

// Add records a trimmed term. Blank and duplicate terms are accepted without
// consuming capacity.
func (t *LimitedTerms) Add(raw string) bool {
	term := strings.TrimSpace(raw)
	if term == "" {
		return true
	}
	key := strings.ToLower(term)
	if _, exists := t.seen[key]; exists {
		return true
	}
	if len(t.terms) >= t.maxTerms || t.chars+len(term) > t.maxChars {
		return false
	}
	t.seen[key] = struct{}{}
	t.terms = append(t.terms, term)
	t.chars += len(term) + 2
	return true
}

// String returns the collected terms in insertion order.
func (t *LimitedTerms) String() string { return strings.Join(t.terms, ", ") }
