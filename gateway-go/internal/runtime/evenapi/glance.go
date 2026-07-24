package evenapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GlanceEvent is one upcoming calendar row for HUD formatting.
type GlanceEvent struct {
	Summary string
	Start   time.Time
	AllDay  bool
}

// GlanceTodo is one open to-do row for HUD formatting.
type GlanceTodo struct {
	Title     string
	Due       time.Time
	DueAllDay bool
}

// GlanceUrgent is one high-priority work-feed / mail brief row.
type GlanceUrgent struct {
	Title    string
	Priority int
}

// GlanceSources supplies structured data for GET /api/even/glance.
// Any func may be nil; missing sources are skipped.
type GlanceSources struct {
	Events func(now time.Time) []GlanceEvent
	Todos  func(now time.Time) []GlanceTodo
	Urgent func(now time.Time) []GlanceUrgent
}

// Glance handles GET /api/even/glance — structured HUD text, no agent turn.
func (h *Handler) Glance(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "even g2 bridge unavailable", http.StatusServiceUnavailable)
		return
	}
	if !h.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "even g2 bridge disabled (set "+EnvBridgeToken+")")
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	presented := ParseBearer(r.Header.Get("Authorization"))
	if !tokenMatch(h.token, presented) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	now := h.now()
	text := FormatGlance(now, h.sources)
	writeJSON(w, http.StatusOK, map[string]any{
		"text":      text,
		"generated": now.UTC().Format(time.RFC3339),
	})
}

// FormatGlance builds 2–4 Korean plain-text lines for the G2 HUD.
func FormatGlance(now time.Time, src GlanceSources) string {
	var lines []string
	if src.Events != nil {
		if line := formatEventLine(src.Events(now), now); line != "" {
			lines = append(lines, line)
		}
	}
	if src.Urgent != nil {
		if line := formatUrgentLine(src.Urgent(now)); line != "" {
			lines = append(lines, line)
		}
	}
	if src.Todos != nil {
		if line := formatTodoLine(src.Todos(now), now); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "지금 볼 일정·긴급·할 일은 없어요."
	}
	return CleanForG2(strings.Join(lines, "\n"))
}

func formatEventLine(events []GlanceEvent, now time.Time) string {
	horizon := now.Add(48 * time.Hour)
	var upcoming []GlanceEvent
	for _, ev := range events {
		summary := strings.TrimSpace(ev.Summary)
		if summary == "" || ev.Start.IsZero() {
			continue
		}
		if ev.Start.Before(now) || !ev.Start.Before(horizon) {
			continue
		}
		upcoming = append(upcoming, GlanceEvent{Summary: summary, Start: ev.Start, AllDay: ev.AllDay})
	}
	if len(upcoming) == 0 {
		return ""
	}
	first := upcoming[0]
	when := formatEventWhen(first, now)
	title := truncateRunes(first.Summary, 28)
	if len(upcoming) == 1 {
		return "다음 일정 " + when + " " + title
	}
	return "다음 일정 " + when + " " + title + " 외 " + strconv.Itoa(len(upcoming)-1)
}

func formatEventWhen(ev GlanceEvent, now time.Time) string {
	if ev.AllDay {
		if sameDay(ev.Start, now) {
			return "오늘"
		}
		if sameDay(ev.Start, now.Add(24*time.Hour)) {
			return "내일"
		}
		return ev.Start.Format("1/2")
	}
	if sameDay(ev.Start, now) {
		return ev.Start.Format("15:04")
	}
	if sameDay(ev.Start, now.Add(24*time.Hour)) {
		return "내일 " + ev.Start.Format("15:04")
	}
	return ev.Start.Format("1/2 15:04")
}

func formatUrgentLine(items []GlanceUrgent) string {
	var titles []string
	for _, it := range items {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		titles = append(titles, truncateRunes(title, 22))
		if len(titles) >= 2 {
			break
		}
	}
	if len(titles) == 0 {
		return ""
	}
	if len(titles) == 1 {
		return "긴급 · " + titles[0]
	}
	return "긴급 " + strconv.Itoa(len(items)) + "건 · " + titles[0] + ", " + titles[1]
}

func formatTodoLine(todos []GlanceTodo, now time.Time) string {
	endOfTomorrow := endOfDay(now.Add(24 * time.Hour))
	var titles []string
	for _, td := range todos {
		title := strings.TrimSpace(td.Title)
		if title == "" {
			continue
		}
		if !td.Due.IsZero() && td.Due.After(endOfTomorrow) {
			continue
		}
		// Undated open todos still count — they are "today's pile".
		titles = append(titles, truncateRunes(title, 22))
		if len(titles) >= 3 {
			break
		}
	}
	if len(titles) == 0 {
		return ""
	}
	if len(titles) == 1 {
		return "할 일 · " + titles[0]
	}
	return "할 일 " + strconv.Itoa(len(titles)) + " · " + titles[0] + " 외 " + strconv.Itoa(len(titles)-1)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.In(b.Location()).Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func endOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, 0, t.Location())
}
