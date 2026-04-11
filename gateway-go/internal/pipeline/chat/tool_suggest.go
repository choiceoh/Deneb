package chat

import (
	"fmt"
	"sort"
	"strings"
)

// suggestToolNames returns up to maxResults registered tool names that are
// closest to `name` by Levenshtein edit distance (rune-level, case-insensitive).
// Candidates with distance > maxDistance are excluded. Results are sorted by
// ascending distance, then alphabetically.
func (r *ToolRegistry) suggestToolNames(name string, maxResults, maxDistance int) []string {
	r.mu.RLock()
	candidates := make([]string, 0, len(r.tools))
	for n := range r.tools {
		candidates = append(candidates, n)
	}
	r.mu.RUnlock()
	if len(candidates) == 0 || name == "" {
		return nil
	}

	lowerName := strings.ToLower(name)
	type scored struct {
		name string
		dist int
	}
	scoredList := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		d := toolNameEditDistance(lowerName, strings.ToLower(c))
		if d <= maxDistance {
			scoredList = append(scoredList, scored{name: c, dist: d})
		}
	}
	if len(scoredList) == 0 {
		return nil
	}
	sort.SliceStable(scoredList, func(i, j int) bool {
		if scoredList[i].dist != scoredList[j].dist {
			return scoredList[i].dist < scoredList[j].dist
		}
		return scoredList[i].name < scoredList[j].name
	})

	if maxResults > len(scoredList) {
		maxResults = len(scoredList)
	}
	out := make([]string, maxResults)
	for i := 0; i < maxResults; i++ {
		out[i] = scoredList[i].name
	}
	return out
}

// unknownToolError builds an error for an unknown tool call, optionally
// including similar registered tool names ("Did you mean: grep, find?").
// This helps the LLM recover from tool name typos in one turn instead of
// guessing blindly from "unknown tool".
func (r *ToolRegistry) unknownToolError(name string) error {
	maxDist := dynamicMaxDistance(name)
	suggestions := r.suggestToolNames(name, 3, maxDist)
	if len(suggestions) == 0 {
		return fmt.Errorf("unknown tool: %q", name)
	}
	return fmt.Errorf("unknown tool: %q. Did you mean: %s?", name, strings.Join(suggestions, ", "))
}

// dynamicMaxDistance scales the allowed edit distance to the query length.
// Very short names (e.g. "kv") must match almost exactly; longer names
// tolerate more typos.
func dynamicMaxDistance(name string) int {
	n := len([]rune(name))
	switch {
	case n <= 3:
		return 1
	case n <= 6:
		return 2
	default:
		return 3
	}
}

// toolNameEditDistance computes the Levenshtein edit distance between
// two strings (rune-level). Used to suggest similar tool names on
// unknown-tool errors.
func toolNameEditDistance(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	la, lb := len(ra), len(rb)

	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr := make([]int, lb+1)
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev = curr
	}
	return prev[lb]
}
