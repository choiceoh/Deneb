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

const (
	glanceCacheTTL = 8 * time.Second
	pageMaxRunes   = 350
)

type GlanceEvent struct {
	Summary string
	Start   time.Time
	End     time.Time
	AllDay  bool
}

type GlanceTodo struct {
	Title     string
	Due       time.Time
	DueAllDay bool
}

type GlanceUrgent struct {
	Title    string
	Priority int
}

type GlanceSources struct {
	Events func(now time.Time) []GlanceEvent
	Todos  func(now time.Time) []GlanceTodo
	Urgent func(now time.Time) []GlanceUrgent
}

type GlancePage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type GlanceBundle struct {
	Text  string
	Pages []GlancePage
}

type glanceCache struct {
	mu     sync.Mutex
	bundle GlanceBundle
	at     time.Time
}

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
		bundle := BuildGlance(now, h.sources)
		h.storeGlanceCache(bundle, now)
		writeGlanceJSON(w, bundle, now, false)
		return
	}
	if bundle, at, ok := h.lookupGlanceCache(now); ok {
		writeGlanceJSON(w, bundle, at, true)
		return
	}
	bundle := BuildGlance(now, h.sources)
	h.storeGlanceCache(bundle, now)
	writeGlanceJSON(w, bundle, now, false)
}

func writeGlanceJSON(w http.ResponseWriter, bundle GlanceBundle, at time.Time, cached bool) {
	writeJSON(w, http.StatusOK, map[string]any{
		"text":      bundle.Text,
		"pages":     bundle.Pages,
		"generated": at.Format(time.RFC3339),
		"cached":    cached,
	})
}

func FormatGlance(now time.Time, src GlanceSources) string {
	return BuildGlance(now, src).Text
}

func BuildGlance(now time.Time, src GlanceSources) GlanceBundle {
	now = now.In(dentime.Location())
	var events []GlanceEvent
	var todos []GlanceTodo
	var urgent []GlanceUrgent
	if src.Events != nil {
		events = src.Events(now)
	}
	if src.Todos != nil {
		todos = src.Todos(now)
	}
	if src.Urgent != nil {
		urgent = src.Urgent(now)
	}

	home := formatHomePage(now, events, urgent, todos)
	pages := []GlancePage{
		{ID: "home", Title: "오늘", Text: home},
		{ID: "cal", Title: "일정", Text: formatCalPage(events, now)},
		{ID: "urgent", Title: "긴급", Text: formatUrgentPage(urgent)},
		{ID: "todo", Title: "할 일", Text: formatTodoPage(todos, now)},
	}
	return GlanceBundle{Text: home, Pages: pages}
}

func cleanPage(s string) string {
	s = CleanForG2(s)
	return truncateRunes(s, pageMaxRunes)
}

func formatHomePage(now time.Time, events []GlanceEvent, urgent []GlanceUrgent, todos []GlanceTodo) string {
	var lines []string
	lines = append(lines, now.Format("15:04"))
	if line := formatHomeEventLine(events, now); line != "" {
		lines = append(lines, line)
	}
	if line := formatHomeUrgentLine(urgent); line != "" {
		lines = append(lines, line)
	}
	if line := formatHomeTodoLine(todos, now); line != "" {
		lines = append(lines, line)
	}
	if len(lines) == 1 {
		lines = append(lines, "지금 볼 일정·긴급·할 일은 없어요.")
	}
	return cleanPage(strings.Join(lines, "\n"))
}

func formatHomeEventLine(events []GlanceEvent, now time.Time) string {
	current, upcoming := splitEvents(events, now)
	if current != nil {
		return "지금 " + truncateRunes(current.Summary, 24)
	}
	if len(upcoming) == 0 {
		return ""
	}
	first := upcoming[0]
	when := formatNextRelative(first, now)
	return "다음 " + when + " " + truncateRunes(first.Summary, 22)
}

func formatHomeUrgentLine(items []GlanceUrgent) string {
	sorted := sortUrgent(items)
	for _, it := range sorted {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		return "긴급 · " + truncateRunes(title, 24)
	}
	return ""
}

func formatHomeTodoLine(todos []GlanceTodo, now time.Time) string {
	ranked := rankTodos(todos, now)
	if len(ranked) == 0 {
		return ""
	}
	prefix := "할 일"
	if ranked[0].rank == 0 {
		prefix = "지난 할 일"
	}
	return prefix + " · " + ranked[0].title
}

func formatCalPage(events []GlanceEvent, now time.Time) string {
	current, upcoming := splitEvents(events, now)
	var lines []string
	if current != nil {
		lines = append(lines, "지금 "+truncateRunes(current.Summary, 28))
	}
	for _, ev := range upcoming {
		if len(lines) >= 4 {
			break
		}
		when := formatEventWhen(ev, now)
		lines = append(lines, when+" "+truncateRunes(ev.Summary, 26))
	}
	if len(lines) == 0 {
		return cleanPage("예정된 일정이 없어요.")
	}
	return cleanPage(strings.Join(lines, "\n"))
}

func formatUrgentPage(items []GlanceUrgent) string {
	sorted := sortUrgent(items)
	var lines []string
	for _, it := range sorted {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		lines = append(lines, "· "+truncateRunes(title, 32))
		if len(lines) >= 5 {
			break
		}
	}
	if len(lines) == 0 {
		return cleanPage("긴급 항목이 없어요.")
	}
	n := countNonEmptyUrgent(sorted)
	header := "긴급 " + strconv.Itoa(n) + "건"
	return cleanPage(header + "\n" + strings.Join(lines, "\n"))
}

func formatTodoPage(todos []GlanceTodo, now time.Time) string {
	ranked := rankTodos(todos, now)
	if len(ranked) == 0 {
		return cleanPage("오늘 볼 할 일이 없어요.")
	}
	var lines []string
	for _, td := range ranked {
		if len(lines) >= 5 {
			break
		}
		prefix := "· "
		switch td.rank {
		case 0:
			prefix = "! "
		case 1:
			prefix = "오늘 "
		case 2:
			prefix = "내일 "
		}
		lines = append(lines, prefix+td.title)
	}
	header := "할 일 " + strconv.Itoa(len(ranked)) + "건"
	return cleanPage(header + "\n" + strings.Join(lines, "\n"))
}

func splitEvents(events []GlanceEvent, now time.Time) (*GlanceEvent, []GlanceEvent) {
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
	return current, upcoming
}

func formatNextRelative(ev GlanceEvent, now time.Time) string {
	if ev.AllDay {
		return formatEventWhen(ev, now)
	}
	if sameDay(ev.Start, now) {
		mins := int(ev.Start.Sub(now).Minutes())
		if mins < 1 {
			return "곧"
		}
		if mins < 120 {
			return strconv.Itoa(mins) + "분 후"
		}
		return ev.Start.Format("15:04")
	}
	return formatEventWhen(ev, now)
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

func sortUrgent(items []GlanceUrgent) []GlanceUrgent {
	sorted := append([]GlanceUrgent(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority > sorted[j].Priority })
	return sorted
}

func countNonEmptyUrgent(items []GlanceUrgent) int {
	n := 0
	for _, it := range items {
		if strings.TrimSpace(it.Title) != "" {
			n++
		}
	}
	return n
}

type rankedTodo struct {
	title string
	rank  int
	due   time.Time
}

func rankTodos(todos []GlanceTodo, now time.Time) []rankedTodo {
	endTomorrow := endOfDay(now.Add(24 * time.Hour))
	startToday := startOfDay(now)
	var rankedTodos []rankedTodo
	for _, td := range todos {
		title := strings.TrimSpace(td.Title)
		if title == "" {
			continue
		}
		rank := 3
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
		rankedTodos = append(rankedTodos, rankedTodo{title: truncateRunes(title, 22), rank: rank, due: due})
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
	return rankedTodos
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

func (h *Handler) lookupGlanceCache(now time.Time) (GlanceBundle, time.Time, bool) {
	if h == nil {
		return GlanceBundle{}, time.Time{}, false
	}
	h.glanceCache.mu.Lock()
	defer h.glanceCache.mu.Unlock()
	if h.glanceCache.bundle.Text == "" || now.Sub(h.glanceCache.at) > glanceCacheTTL {
		return GlanceBundle{}, time.Time{}, false
	}
	return h.glanceCache.bundle, h.glanceCache.at, true
}

func (h *Handler) storeGlanceCache(bundle GlanceBundle, at time.Time) {
	if h == nil {
		return
	}
	h.glanceCache.mu.Lock()
	defer h.glanceCache.mu.Unlock()
	h.glanceCache.bundle = bundle
	h.glanceCache.at = at
}