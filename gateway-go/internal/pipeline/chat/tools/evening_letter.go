package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// EveningLetterOpts holds optional configuration for the evening letter tool.
type EveningLetterOpts struct {
	DiaryDir string // wiki diary directory; empty = no diary logging
	WikiDir  string // wiki root directory; empty = no deadline scan
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
func ToolEveningLetter(_ toolctx.ToolExecutor, opts ...EveningLetterOpts) ToolFunc {
	var diaryDir, wikiDir string
	if len(opts) > 0 {
		diaryDir = opts[0].DiaryDir
		wikiDir = opts[0].WikiDir
	}

	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		now := time.Now().In(kstLocation)

		var (
			mu      sync.Mutex
			results = make([]any, 3)
		)

		type collector struct {
			idx int
			fn  func(ctx context.Context) any
		}
		collectors := []collector{
			{0, func(ctx context.Context) any { return fetchCalendar(ctx) }},
			{1, func(ctx context.Context) any { return fetchEmail(ctx) }},
			{2, func(_ context.Context) any { return fetchDeadlines(wikiDir, now) }},
		}

		var wg sync.WaitGroup
		for _, c := range collectors {
			wg.Add(1)
			go func(idx int, fn func(context.Context) any) {
				defer wg.Done()
				sectionCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				data := fn(sectionCtx)
				mu.Lock()
				results[idx] = data
				mu.Unlock()
			}(c.idx, c.fn)
		}
		wg.Wait()

		weekday := [...]string{"일", "월", "화", "수", "목", "금", "토"}[now.Weekday()]
		dateStr := fmt.Sprintf("%d년 %d월 %d일 %s요일", now.Year(), int(now.Month()), now.Day(), weekday)
		envelope := map[string]any{
			"date":      dateStr,
			"timestamp": now.Format(time.RFC3339),
			"sections": map[string]any{
				"calendar":  results[0],
				"email":     results[1],
				"deadlines": results[2],
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

	return sb.String()
}
