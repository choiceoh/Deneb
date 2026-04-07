// Package prompt provides system prompt assembly and token budget optimization.
package prompt

import (
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
)

// DefaultSystemPromptBudget is the default token budget for system prompt
// fragments (65536 tokens). The base system prompt (identity, tools, skills,
// context files) is considered fixed; this budget governs variable additions
// like knowledge, recall, and proactive hints.
const DefaultSystemPromptBudget uint64 = 65_536

// PromptFragment represents a named segment of the system prompt with
// priority metadata for budget-aware optimization.
type PromptFragment struct {
	Name       string // e.g., "soul", "tool_schemas", "memory", "proactive_hints"
	Content    string
	Priority   int  // 0=highest (never remove), 1=high, 2=medium, 3=low (first to remove)
	Shrinkable bool // true if content can be truncated when over budget
}

// PromptBudget controls system prompt token allocation.
type PromptBudget struct {
	Total uint64 // max tokens for the optimizable portion of the system prompt
}

// DefaultPriorities maps well-known fragment names to their default priority.
// Priority 0: never removed or shrunk.
// Priority 1: never removed (tool schemas need to be complete).
// Priority 2: can be shrunk or removed under pressure.
// Priority 3: first to be removed entirely.
var DefaultPriorities = map[string]int{
	"soul":          0,
	"identity":      0,
	"user_profile":  0,
	"tool_schemas":  1,
	"skills":        1,
	"memory":        2,
	"session_state": 2,
}

// defaultShrinkable defines which priority levels support content shrinking.
var defaultShrinkable = map[int]bool{
	2: true,
	3: true,
}

// NewFragment creates a PromptFragment with default priority and shrinkable
// settings looked up from DefaultPriorities.
func NewFragment(name, content string) PromptFragment {
	priority := 2 // default to medium if name is unknown
	if p, ok := DefaultPriorities[name]; ok {
		priority = p
	}
	return PromptFragment{
		Name:       name,
		Content:    content,
		Priority:   priority,
		Shrinkable: defaultShrinkable[priority],
	}
}

// Optimize returns a copy of fragments that fits within the token budget.
// Optimization proceeds in priority order:
//  1. If total tokens <= budget, return all fragments unchanged.
//  2. Remove all priority 3 fragments.
//  3. Shrink priority 2 fragments to half (largest first).
//  4. Remove priority 2 fragments (smallest first).
//  5. Priority 0 and 1 are never modified.
func (b *PromptBudget) Optimize(fragments []PromptFragment) []PromptFragment {
	if b.Total == 0 || len(fragments) == 0 {
		return fragments
	}

	// Work on a copy so callers' slices are not mutated.
	result := make([]PromptFragment, len(fragments))
	copy(result, fragments)

	if sumTokens(result) <= b.Total {
		return result
	}

	// Step 1: remove priority 3 fragments.
	result = filterByPriority(result, 3)
	if sumTokens(result) <= b.Total {
		return result
	}

	// Step 2: shrink priority 2 fragments (largest first) to half.
	result = shrinkByPriority(result, 2, 0.5)
	if sumTokens(result) <= b.Total {
		return result
	}

	// Step 3: remove priority 2 fragments (smallest first).
	result = removeShrinkableSmallestFirst(result, 2, b.Total)

	return result
}

// Assemble runs Optimize and concatenates the surviving fragment contents.
func (b *PromptBudget) Assemble(fragments []PromptFragment) string {
	optimized := b.Optimize(fragments)
	var sb strings.Builder
	for _, f := range optimized {
		if f.Content != "" {
			sb.WriteString(f.Content)
		}
	}
	return sb.String()
}

// sumTokens returns the total estimated tokens across all fragments.
func sumTokens(fragments []PromptFragment) uint64 {
	var total uint64
	for _, f := range fragments {
		total += uint64(tokenest.Estimate(f.Content))
	}
	return total
}

// filterByPriority removes all fragments with the given priority.
func filterByPriority(fragments []PromptFragment, priority int) []PromptFragment {
	result := make([]PromptFragment, 0, len(fragments))
	for _, f := range fragments {
		if f.Priority != priority {
			result = append(result, f)
		}
	}
	return result
}

// shrinkByPriority truncates shrinkable fragments at the given priority level
// to the specified fraction of their original rune count (largest first).
func shrinkByPriority(fragments []PromptFragment, priority int, fraction float64) []PromptFragment {
	// Sort indices by token count descending so we shrink the biggest first.
	type idxTokens struct {
		idx    int
		tokens uint64
	}
	var targets []idxTokens
	for i, f := range fragments {
		if f.Priority == priority && f.Shrinkable {
			targets = append(targets, idxTokens{i, uint64(tokenest.Estimate(f.Content))})
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].tokens > targets[j].tokens
	})

	result := make([]PromptFragment, len(fragments))
	copy(result, fragments)
	for _, t := range targets {
		result[t.idx].Content = shrinkContent(result[t.idx].Content, fraction)
	}
	return result
}

// removeShrinkableSmallestFirst removes priority-level fragments one by one
// (smallest token count first) until the total fits within the budget.
func removeShrinkableSmallestFirst(fragments []PromptFragment, priority int, budget uint64) []PromptFragment {
	// Collect indices of removable fragments, sorted by token count ascending.
	type idxTokens struct {
		idx    int
		tokens uint64
	}
	var removable []idxTokens
	for i, f := range fragments {
		if f.Priority == priority {
			removable = append(removable, idxTokens{i, uint64(tokenest.Estimate(f.Content))})
		}
	}
	sort.Slice(removable, func(i, j int) bool {
		return removable[i].tokens < removable[j].tokens
	})

	removeSet := make(map[int]bool)
	total := sumTokens(fragments)
	for _, r := range removable {
		if total <= budget {
			break
		}
		removeSet[r.idx] = true
		total -= r.tokens
	}

	result := make([]PromptFragment, 0, len(fragments)-len(removeSet))
	for i, f := range fragments {
		if !removeSet[i] {
			result = append(result, f)
		}
	}
	return result
}

// shrinkContent truncates text to the given fraction of its rune count,
// preserving valid UTF-8 boundaries.
func shrinkContent(text string, fraction float64) string {
	runes := []rune(text)
	targetLen := int(float64(len(runes)) * fraction)
	if targetLen <= 0 {
		return ""
	}
	if targetLen >= len(runes) {
		return text
	}
	return string(runes[:targetLen])
}
