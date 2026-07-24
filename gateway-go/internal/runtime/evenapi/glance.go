package evenapi

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

const glanceCacheTTL = 8 * time.Second

// GlanceEvent is one upcoming (or in-progress) calendar row for HUD formatting.
type GlanceEvent struct {
	Summary string
	Start   time.Time
	End     time.Time
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

type glanceCache struct {
	mu   sync.Mutex
	text string
	at   time.Time
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

	now := h.now().In(dentime.Location())
	if force := r.URL.Query().Get("fresh"); force == "1" || force == "true" {
		text := FormatGlance(now, h.sources)
		h.storeGlanceCache(text, now)
		writeJSON(w, http.StatusOK, map[string]any{
			"text":      text,
			"generated": now.Format(time.RFC3339),
			"cached":    false,
		})
		return
	}
	if text, at, ok := h.lookupGlanceCache(now); ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"text":      text,
			"generated": at.Format(time.RFC3339),
			"cached":    true,
		})
		return
	}
	text := FormatGlance(now, h.sources)
	h.storeGlanceCache(text, now)
	writeJSON(w, http.StatusOK, map[string]any{
		"text":      text,
		"generated": now.Format(time.RFC3339),
		"cached":    false,
	})
}

// FormatGlance builds 2–4 Korean plain-text lines for the G2 HUD.
func FormatGlance(now time.Time, src GlanceSources) string {
	now = now.In(dentime.Location())
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
	var current *GlanceEvent
	var upcoming []GlanceEvent
	for i := range events {
		ev := events[i]
		summary := strings.TrimSpace(ev.Summary)
		if summary == "" || ev.Start.IsZero() {
			continue
		}
		ev.Summary = summary
		ev.Start = ev.Start.In(now.Location())
		if !ev.End.IsZero() {
			ev.End = ev.End.In(now.Location())
		}
		if isCurrentEvent(ev, now) {
			cp := ev
			if current == nil || cp.Start.Before(current.Start) {
				current = &cp
			}
			continue
		}
		if ev.Start.Before(now) || !ev.Start.Before(horizon) {
			continue
		}
		upcoming = append(upcoming, ev)
	}
	sort.Slice(upcoming, func(i, j int) bool { return upcoming[i].Start.Before(upcoming[j].Start) })

	var parts []string
	if current != nil {
		parts = append(parts, "지금 "+truncateRunes(current.Summary, 24))
	}
	if len(upcoming) > 0 {
		first := upcoming[0]
		when := formatEventWhen(first, now)
		title := truncateRunes(first.Summary, 24)
		line := "다음 " + when + " " + title
		if len(upcoming) > 1 {
			line += " 외 " + strconv.Itoa(len(upcoming)-1)
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n")
}

func isCurrentEvent(ev GlanceEvent, now time.Time) bool {
	if ev.Start.After(now) {
		return false
	}
	if ev.AllDay {
		return sameDay(ev.Start, now)
	}
	end := ev.End
	if end.IsZero() {
		end = ev.Start.Add(time.Hour)
	}
	return !end.Before(now)
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
	sorted := append([]GlanceUrgent(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority > sorted[j].Priority })
	var titles []string
	for _, it := range sorted {
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
	total := len(sorted)
	if total < len(titles) {
		total = len(titles)
	}
	return "긴급 " + strconv.Itoa(total) + "건 · " + titles[0] + ", " + titles[1]
}

func formatTodoLine(todos []GlanceTodo, now time.Time) string {
	type ranked struct {
		title string
		rank  int
		due   time.Time
	}
	endTomorrow := endOfDay(now.Add(24 * time.Hour))
	startToday := startOfDay(now)
	var rankedTodos []ranked
	for _, td := range todos {
		title := strings.TrimSpace(td.Title)
		if title == "" {
			continue
		}
		rank := 3 // undated
		var due time.Time
		if !td.Due.IsZero() {
			due = td.Due.In(now.Location())
			switch {
			case td.DueAllDay && due.Before(startToday):
				rank = 0
			case td.DueAllDay && sameDay(due, now):
				rank = 1
			case !td.DueAllDay && due.Before(now):
				rank = 0
			case !td.DueAllDay && sameDay(due, now):
				rank = 1
			case !due.After(endTomorrow):
				rank = 2
			default:
				continue
			}
		}
		rankedTodos = append(rankedTodos, ranked{title: truncateRunes(title, 22), rank: rank, due: due})
	}
	sort.SliceStable(rankedTodos, func(i, j int) bool {
		if rankedTodos[i].rank != rankedTodos[j].rank {
			return rankedTodos[i].rank < rankedTodos[j].rank
		}
		if rankedTodos[i].due.IsZero() != rankedTodos[j].due.IsZero() {
			return !rankedTodos[i].due.IsZero()
		}
		return rankedTodos[i].due.Before(rankedTodos[j].due)
	})
	if len(rankedTodos) == 0 {
		return ""
	}
	if len(rankedTodos) == 1 {
		prefix := "할 일"
		if rankedTodos[0].rank == 0 {
			prefix = "지난 할 일"
		}
		return prefix + " · " + rankedTodos[0].title
	}
	prefix := "할 일"
	if rankedTodos[0].rank == 0 {
		prefix = "지난 할 일"
	}
	return prefix + " " + strconv.Itoa(len(rankedTodos)) + " · " + rankedTodos[0].title + " 외 " + strconv.Itoa(len(rankedTodos)-1)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.In(b.Location()).Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func endOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, 0, t.Location())
}

func (h *Handler) lookupGlanceCache(now time.Time) (string, time.Time, bool) {
	if h == nil {
		return "", time.Time{}, false
	}
	h.glanceCache.mu.Lock()
	defer h.glanceCache.mu.Unlock()
	if h.glanceCache.text == "" || now.Sub(h.glanceCache.at) > glanceCacheTTL {
		return "", time.Time{}, false
	}
	return h.glanceCache.text, h.glanceCache.at, true
}

func (h *Handler) storeGlanceCache(text string, at time.Time) {
	if h == nil {
		return
	}
	h.glanceCache.mu.Lock()
	defer h.glanceCache.mu.Unlock()
	h.glanceCache.text = text
	h.glanceCache.at = at
}
