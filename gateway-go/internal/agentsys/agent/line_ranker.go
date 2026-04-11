package agent

import (
	"fmt"
	"sort"
	"strings"
)

const (
	anchorThreshold    = 8 // lines scoring >= this get ±context expansion
	contextRadius      = 2 // default context lines around anchors
	panicContextRadius = 5 // wider context for panic/fatal to capture stack traces
	overheadReserve    = 256
)

// RankLines selects lines from content by importance score, fitting within
// budget characters. Error, warning, and panic lines are prioritized, and
// context lines around error-level anchors are boosted to preserve stack
// traces (±5 for panic/fatal, ±2 for others). Gaps between selected lines
// are marked with "[N lines omitted]".
//
// A small portion of the budget is reserved for the header and omission
// markers so the selected content doesn't overshoot.
//
// For content that is not line-structured (≤3 lines) or when no individual
// line fits the budget, falls back to head/tail truncation.
func RankLines(content string, budget int) string {
	if len(content) <= budget {
		return content
	}

	lines := strings.Split(content, "\n")
	n := len(lines)

	// Line-level ranking needs enough lines to be useful.
	if n <= 3 {
		return TruncateHeadTail(content, budget, "")
	}

	// Pre-compute lowercased lines once for scoring and context expansion.
	lowers := make([]string, n)
	for i, line := range lines {
		lowers[i] = strings.ToLower(line)
	}

	// 1. Score each line.
	scores := make([]int, n)
	for i, line := range lines {
		scores[i] = scoreLine(line, lowers[i], i, n)
	}

	// 2. Mark context around anchor lines. Panic/fatal/goroutine anchors
	//    get wider radius (±5) to capture Go stack traces; other error
	//    anchors get ±2.
	priority := make([]bool, n)
	for i, s := range scores {
		if s < anchorThreshold {
			continue
		}
		radius := contextRadius
		if isPanicAnchor(lowers[i]) {
			radius = panicContextRadius
		}
		for j := max(0, i-radius); j <= min(n-1, i+radius); j++ {
			priority[j] = true
		}
	}

	// 3. Sort line indices: priority first, then by score descending.
	// Stable sort preserves original order among equal-scored lines.
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}
	sort.SliceStable(indices, func(a, b int) bool {
		ia, ib := indices[a], indices[b]
		if priority[ia] != priority[ib] {
			return priority[ia]
		}
		return scores[ia] > scores[ib]
	})

	// 4. Greedily select lines. Reserve overhead for the header and markers.
	contentBudget := budget - overheadReserve
	if contentBudget < budget/2 {
		contentBudget = budget / 2 // never reserve more than half
	}
	selected := make([]bool, n)
	used, count := 0, 0
	for _, idx := range indices {
		cost := len(lines[idx]) + 1 // +1 for newline
		if used+cost > contentBudget {
			if contentBudget-used < 2 {
				break
			}
			continue
		}
		selected[idx] = true
		used += cost
		count++
	}

	if count == 0 {
		return TruncateHeadTail(content, budget, "")
	}

	// 5. Reassemble in original order with omission markers.
	var b strings.Builder
	b.Grow(used + overheadReserve)
	totalOmitted := 0
	i := 0
	for i < n {
		if selected[i] {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(lines[i])
			i++
			continue
		}
		omitStart := i
		for i < n && !selected[i] {
			i++
		}
		omitted := i - omitStart
		totalOmitted += omitted
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "... [%d lines omitted] ...", omitted)
	}

	if totalOmitted > 0 {
		return fmt.Sprintf("[ranked: %d/%d lines, %d omitted]\n%s",
			count, n, totalOmitted, b.String())
	}
	return b.String()
}

// scoreLine assigns an importance score to a line based on content patterns
// and position. Scores are cumulative — a line matching multiple patterns
// gets the sum of all matching bonuses plus a base score of 1.
//
//	panic/fatal            +15
//	goroutine (header)     +12
//	error/에러/오류          +10
//	failed/exception       +8
//	exit code/status       +6
//	warning/warn/경고       +5
//	test FAIL marker       +4
//	HTTP 4xx/5xx status    +4
//	JSON error fields      +4
//	section separator      +3
//	last 20% position      +3
//	first 10% position     +2
func scoreLine(line, lower string, idx, total int) int {
	score := 1

	// Severity keywords.
	if strings.Contains(lower, "panic") || strings.Contains(lower, "fatal") {
		score += 15
	}
	if strings.HasPrefix(lower, "goroutine ") {
		score += 12
	}
	if containsAny(lower, "error", "에러", "오류") {
		score += 10
	}
	if containsAny(lower, "failed", "exception") {
		score += 8
	}
	if containsAny(lower, "exit code", "exit status") {
		score += 6
	}
	if containsAny(lower, "warning", "warn", "경고") {
		score += 5
	}

	// Go test output markers (case-sensitive).
	if strings.HasPrefix(line, "--- FAIL") || strings.HasPrefix(line, "FAIL\t") || line == "FAIL" {
		score += 4
	}
	// HTTP error status codes in tool output (API/health results).
	if containsAny(lower, " 400", " 401", " 403", " 404", " 500", " 502", " 503") {
		score += 4
	}
	// JSON-style error fields common in structured tool output.
	if containsAny(lower, `"error"`, `"status": "fail`, `"status":"fail`) {
		score += 4
	}
	// Section separators help preserve output structure.
	trimmed := strings.TrimSpace(line)
	if len(trimmed) >= 3 && (strings.HasPrefix(trimmed, "===") || strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "***")) {
		score += 3
	}

	// Positional bonuses.
	if total > 0 {
		pos := float64(idx) / float64(total)
		if pos >= 0.8 {
			score += 3
		}
		if pos < 0.1 {
			score += 2
		}
	}

	return score
}

// isPanicAnchor reports whether a lowercased line indicates a panic, fatal
// error, or goroutine header — used to apply wider context radius.
func isPanicAnchor(lower string) bool {
	return strings.Contains(lower, "panic") || strings.Contains(lower, "fatal") || strings.HasPrefix(lower, "goroutine ")
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
