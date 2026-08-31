package routine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// EveningLetterOpts holds optional configuration for the evening letter tool.
type EveningLetterOpts struct {
	DiaryDir           string                    // wiki diary directory; empty = no diary logging
	WikiDir            string                    // wiki root directory; empty = no deadline scan
	GroupwareCollector func(context.Context) any // optional test/alternate collector
}

// ToolEveningLetter returns the evening_letter tool — the end-of-day counterpart
// to morning_letter. It collects the forward-looking sections that matter for a
// wrap-up and tomorrow prep — calendar (today + tomorrow), unhandled email, and
// approaching wiki deadlines — in parallel and returns raw JSON for the LLM to
// compose the final letter (reflection, tomorrow prep, priorities).
//
// The morning-only market sections (weather, FX, copper) are intentionally
// dropped: those belong to a morning brief, not an evening review. The shared
// section collectors and data types live in morning_letter.go.
func ToolEveningLetter(opts ...EveningLetterOpts) toolport.ToolFunc {
	var diaryDir, wikiDir string
	groupwareCollector := fetchGroupwarePending
	if len(opts) > 0 {
		diaryDir = opts[0].DiaryDir
		wikiDir = opts[0].WikiDir
		if opts[0].GroupwareCollector != nil {
			groupwareCollector = opts[0].GroupwareCollector
		}
	}

	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		now := time.Now().In(kstLocation)

		collectors := []letterCollector{
			{0, func(ctx context.Context) any { return fetchCalendar(ctx) }},
			{1, func(ctx context.Context) any { return fetchEmail(ctx) }},
			{2, func(_ context.Context) any { return fetchDeadlines(wikiDir, now) }},
			{3, groupwareCollector},
		}

		results := collectLetterSections(ctx, 4, collectors)
		dateStr := koreanDate(now)
		envelope := map[string]any{
			"date":      dateStr,
			"timestamp": now.Format(time.RFC3339),
			"sections": map[string]any{
				"calendar":          results[0],
				"email":             results[1],
				"deadlines":         results[2],
				"groupware_pending": results[3],
			},
		}

		out, err := json.MarshalIndent(envelope, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal evening letter data: %w", err)
		}

		// Log collected data to diary for wiki knowledge synthesis.
		if diaryDir != "" {
			summary := formatEveningDiarySummary(dateStr, results)
			_ = wiki.AppendDiaryTo(diaryDir, summary) // best-effort: diary append is non-critical
		}

		return string(out), nil
	}
}

// formatEveningDiarySummary builds a concise diary entry from evening letter data.
func formatEveningDiarySummary(dateStr string, results []any) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "이브닝레터 수집 (%s)\n\n", dateStr)

	if cal, ok := results[0].(calendarData); ok && cal.OK && len(cal.Events) > 0 {
		fmt.Fprintf(&sb, "- 일정: %d건\n", len(cal.Events))
	}

	if em, ok := results[1].(emailData); ok && em.OK && len(em.Messages) > 0 {
		fmt.Fprintf(&sb, "- 메일: %d건\n", len(em.Messages))
	}

	if dl, ok := results[2].(deadlineData); ok && dl.OK && len(dl.Items) > 0 {
		fmt.Fprintf(&sb, "- 임박 마감: %d건\n", len(dl.Items))
	}
	if len(results) > 3 {
		if gw, ok := results[3].(groupwarePendingData); ok && gw.OK && gw.Count > 0 {
			if gw.StaleCount > 0 {
				fmt.Fprintf(&sb, "- 미결 전자결재: %d건 (방치 %d건)\n", gw.Count, gw.StaleCount)
			} else {
				fmt.Fprintf(&sb, "- 미결 전자결재: %d건\n", gw.Count)
			}
		}
	}

	return sb.String()
}
