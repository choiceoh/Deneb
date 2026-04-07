package skills

import (
	"sort"
	"strings"
)

// NormalizeSkillFilter normalizes a raw filter list to clean, sorted, unique strings.
// Returns nil if input is nil (meaning unrestricted).
// Returns empty slice if input is empty (meaning no skills allowed).
func NormalizeSkillFilter(filter []string) []string {
	if filter == nil {
		return nil
	}
	result := make([]string, 0)
	for _, s := range filter {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return dedupeAndSort(result)
}

// NormalizeSkillFilterForComparison prepares filters for structural comparison.
// Returns nil if input is nil. Returns non-nil empty slice for empty input.
func NormalizeSkillFilterForComparison(filter []string) []string {
	if filter == nil {
		return nil
	}
	normalized := NormalizeSkillFilter(filter)
	if normalized == nil {
		return make([]string, 0)
	}
	return dedupeAndSort(normalized)
}

// MatchesSkillFilter checks if two filter lists are structurally equivalent.
func MatchesSkillFilter(cached, next []string) bool {
	a := NormalizeSkillFilterForComparison(cached)
	b := NormalizeSkillFilterForComparison(next)

	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// dedupeAndSort returns a sorted, deduplicated copy of the input.
func dedupeAndSort(input []string) []string {
	if len(input) == 0 {
		return input
	}
	seen := make(map[string]struct{}, len(input))
	var unique []string
	for _, s := range input {
		if _, ok := seen[s]; !ok {
			unique = append(unique, s)
			seen[s] = struct{}{}
		}
	}
	sort.Strings(unique)
	return unique
}
