package routine

import (
	"fmt"
	"strings"
	"time"
)

const morningTodayFocusMax = 5

func writeMorningTodayFocus(b *strings.Builder, calendar calendarData, deadlines deadlineData, pending groupwarePendingData, now time.Time) {
	items := collectMorningTodayFocus(calendar, deadlines, pending, now)
	if len(items) == 0 {
		return
	}
	b.WriteString("  <card>\n    <row><icon name=\"assignment\" size=\"16\"/><text style=\"caption\">오늘 전무 할 일</text></row>\n")
	writeMorningList(b, items, morningTodayFocusMax)
	b.WriteString("  </card>\n")
}

// collectMorningTodayFocus picks what 선택님 should look at today: overdue/D-1
// deadlines, today's calendar rows, then stale approvals. Capped so the card
// stays a glance, not a dump of every open item.
func collectMorningTodayFocus(calendar calendarData, deadlines deadlineData, pending groupwarePendingData, now time.Time) []string {
	items := make([]string, 0, morningTodayFocusMax)
	today := now.In(kstLocation).Format("01/02")

	for _, item := range deadlines.Items {
		if item.DaysLeft > 1 {
			continue
		}
		label, _, marker := deadlinePresentation(item.DaysLeft)
		items = append(items, fmt.Sprintf("%s %s — %s", marker, morningPlain(item.Title, 72), label))
		if len(items) == morningTodayFocusMax {
			return items
		}
	}

	if calendar.OK {
		for _, ev := range calendar.Events {
			if !strings.HasPrefix(ev, today) {
				continue
			}
			items = append(items, "🔵 "+morningPlain(ev, 90))
			if len(items) == morningTodayFocusMax {
				return items
			}
		}
	}

	for _, item := range pending.Items {
		if item.EscalationLevel <= 0 && strings.TrimSpace(item.StaleLabel) == "" {
			continue
		}
		stale := strings.TrimSpace(item.StaleLabel)
		if stale == "" {
			stale = "방치"
		}
		items = append(items, fmt.Sprintf("🟠 %s — %s", morningPlain(item.Title, 72), morningPlain(stale, 24)))
		if len(items) == morningTodayFocusMax {
			return items
		}
	}
	return items
}
