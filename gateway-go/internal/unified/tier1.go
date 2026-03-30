// Tier 1 always-inject: high-importance facts that are injected into the
// system prompt every turn, without requiring a search query.
//
// This provides immediate context about critical user decisions, preferences,
// and identity regardless of what the current conversation is about.
//
// Criteria: importance >= 0.85, category in (decision, preference, user_model),
// active = 1, not expired.
package unified

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	// Tier1Threshold is the minimum importance for always-on injection.
	Tier1Threshold = 0.85
	// Tier1MaxTokens is the soft token budget for tier-1 facts.
	Tier1MaxTokens = 3000
)

// Tier1Section builds the "## 핵심 기억" system prompt section from
// high-importance facts. Returns empty string if no qualifying facts exist.
func (s *Store) Tier1Section(ctx context.Context) string {
	facts, err := s.Tier1Facts(ctx, Tier1Threshold, []string{"decision", "preference", "user_model"})
	if err != nil || len(facts) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## 핵심 기억\n\n")
	b.WriteString("다음은 항상 기억해야 할 중요한 사실입니다:\n\n")

	tokenCount := 0
	for _, f := range facts {
		line := fmt.Sprintf("- [%.0f%%] %s\n", f.Importance*100, f.Content)
		lineTokens := len(line) / 4 // rough estimate
		if tokenCount+lineTokens > Tier1MaxTokens {
			break
		}
		b.WriteString(line)
		tokenCount += lineTokens
	}

	return b.String()
}

// AssignTier determines the tier for a new item based on type and importance.
func AssignTier(itemType string, importance float64) string {
	switch itemType {
	case "fact":
		return "long"
	case "summary":
		return "medium"
	case "message":
		return "short"
	default:
		// Unknown type: use importance to decide.
		if importance >= 0.7 {
			return "long"
		}
		if importance >= 0.3 {
			return "medium"
		}
		return "short"
	}
}

// ShouldPromote checks if a summary is important enough to be promoted
// to a long-term fact during compaction.
func ShouldPromote(importance float64, depth uint32) bool {
	// Promote condensed summaries (depth >= 1) with high importance.
	return depth >= 1 && importance >= 0.7
}

// FormatTier1ForPrompt formats tier-1 facts for system prompt injection.
// This is a standalone function that works with any fact source.
func FormatTier1ForPrompt(facts []SearchResult) string {
	if len(facts) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## 핵심 기억\n\n")

	now := time.Now()
	for _, f := range facts {
		age := ""
		if f.CreatedAt > 0 {
			created := time.Unix(0, f.CreatedAt*int64(time.Millisecond))
			days := int(now.Sub(created).Hours() / 24)
			if days == 0 {
				age = "오늘"
			} else if days < 7 {
				age = fmt.Sprintf("%d일 전", days)
			} else if days < 30 {
				age = fmt.Sprintf("%d주 전", days/7)
			} else {
				age = fmt.Sprintf("%d개월 전", days/30)
			}
		}

		if age != "" {
			fmt.Fprintf(&b, "- [%.0f%%] %s (%s)\n", f.Importance*100, f.Content, age)
		} else {
			fmt.Fprintf(&b, "- [%.0f%%] %s\n", f.Importance*100, f.Content)
		}
	}

	return b.String()
}
