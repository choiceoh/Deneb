// knowledge_goals.go — Auto-set autonomous goals from memory facts recalled
// during knowledge prefetch. Selectively identifies high-importance actionable
// facts (decisions, context) and creates autonomous goals for them.
//
// Runs as fire-and-forget after memory search completes, never blocking
// the knowledge injection pipeline.
package chat

import (
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
)

// Auto-goal selection criteria.
const (
	// Only facts with importance >= this threshold are considered.
	autoGoalMinImportance = 0.8
	// Facts older than this are skipped (stale decisions are not actionable).
	autoGoalMaxAgeDays = 14
	// Minimum content length (runes) for a fact to become a goal.
	autoGoalMinContentRunes = 10
	// Maximum goals to create per prefetch cycle (avoid spam).
	autoGoalMaxPerCycle = 2
	// Similarity threshold: skip if existing goal shares this prefix length match.
	autoGoalDedupPrefixRunes = 30
)

// autoGoalCategories defines which fact categories are goal-worthy.
// Decisions need execution; context items may need follow-up.
var autoGoalCategories = map[string]bool{
	"decision": true,
	"context":  true,
}

// autoSetGoalsFromFacts checks recalled memory facts for high-importance
// actionable items and creates autonomous goals for qualifying ones.
// Called as fire-and-forget (goroutine) from PrefetchKnowledge.
func autoSetGoalsFromFacts(goalStore *autonomous.GoalStore, facts []memory.SearchResult) {
	if goalStore == nil || len(facts) == 0 {
		return
	}

	now := time.Now()
	cutoff := now.Add(-autoGoalMaxAgeDays * 24 * time.Hour)

	// Load existing active goals for dedup.
	activeGoals, err := goalStore.ActiveGoals()
	if err != nil {
		slog.Warn("auto-goal: failed to load active goals", "error", err)
		return
	}

	created := 0
	for _, sr := range facts {
		if created >= autoGoalMaxPerCycle {
			break
		}

		f := sr.Fact
		if !isGoalWorthy(f, cutoff) {
			continue
		}

		// Dedup: skip if an existing goal covers similar content.
		if isDuplicateGoal(f.Content, activeGoals) {
			continue
		}

		// Map fact importance to goal priority.
		priority := factToPriority(f.Importance)

		desc := truncateRunes(f.Content, autonomous.MaxDescriptionLen)
		goal, err := goalStore.Add(desc, priority)
		if err != nil {
			slog.Warn("auto-goal: failed to add goal", "error", err, "factId", f.ID)
			continue
		}

		slog.Info("auto-goal: created from memory fact",
			"goalId", goal.ID,
			"factId", f.ID,
			"category", f.Category,
			"importance", f.Importance,
			"priority", priority,
		)
		created++

		// Add to activeGoals for subsequent dedup within this cycle.
		activeGoals = append(activeGoals, goal)
	}
}

// isGoalWorthy checks whether a fact qualifies for auto-goal creation.
func isGoalWorthy(f memory.Fact, cutoff time.Time) bool {
	// Category filter.
	if !autoGoalCategories[f.Category] {
		return false
	}
	// Importance threshold.
	if f.Importance < autoGoalMinImportance {
		return false
	}
	// Content length check.
	if utf8.RuneCountInString(f.Content) < autoGoalMinContentRunes {
		return false
	}
	// Freshness: use UpdatedAt if available, else CreatedAt.
	refTime := f.UpdatedAt
	if refTime.IsZero() {
		refTime = f.CreatedAt
	}
	if !refTime.IsZero() && refTime.Before(cutoff) {
		return false
	}
	return true
}

// isDuplicateGoal checks if the fact content overlaps with any existing active goal.
// Uses a prefix-based match to catch near-duplicates.
func isDuplicateGoal(content string, goals []autonomous.Goal) bool {
	contentPrefix := runePrefix(strings.TrimSpace(content), autoGoalDedupPrefixRunes)
	for _, g := range goals {
		goalPrefix := runePrefix(strings.TrimSpace(g.Description), autoGoalDedupPrefixRunes)
		if contentPrefix == goalPrefix {
			return true
		}
		// Also check if the fact content is a substring of an existing goal or vice versa.
		if strings.Contains(g.Description, content) || strings.Contains(content, g.Description) {
			return true
		}
	}
	return false
}

// factToPriority maps fact importance to goal priority.
func factToPriority(importance float64) string {
	switch {
	case importance >= 0.9:
		return autonomous.PriorityHigh
	case importance >= 0.8:
		return autonomous.PriorityMedium
	default:
		return autonomous.PriorityLow
	}
}

// runePrefix returns the first n runes of s, lowercased for comparison.
func runePrefix(s string, n int) string {
	r := []rune(strings.ToLower(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
