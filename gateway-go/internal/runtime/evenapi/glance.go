package evenapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	ID        string
	Title     string
	Preview   string // optional one-line summary snippet
	Body      string // detail body for tap-through
	Priority  int
	CreatedAt time.Time // relative age on HUD
}

// GlanceItem is the wire shape for selectable HUD alerts (list → detail).
type GlanceItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Preview  string `json:"preview,omitempty"`
	Body     string `json:"body,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Age      string `json:"age,omitempty"`
}

// GlanceSources are the live providers behind the HUD. Each takes the REQUEST
// context: they reach out to network-backed stores (Google Calendar), and this
// runs inside an HTTP handler, so a hung upstream must be cancellable by the
// caller going away rather than pinning a request for the client's full timeout.
type GlanceSources struct {
	Events func(ctx context.Context, now time.Time) []GlanceEvent
	Todos  func(ctx context.Context, now time.Time) []GlanceTodo
	Urgent func(ctx context.Context, now time.Time) []GlanceUrgent
}

type GlancePage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
	Empty bool   `json:"empty,omitempty"`
}

type GlanceBundle struct {
	Text  string
	Pages []GlancePage
	Items []GlanceItem
	// Now is what a face-worn display is actually for: "지금 금호타이어 · 종료
	// 20분" or "15:00 기아 광주". It is the one thing the wearer cannot get by
	// reading the alert list underneath it.
	Now string
	// Counts is the one-line "일정 2 · 할 일 3" summary of what is behind the
	// alert page. The plugin draws the alert list itself from Items, so it never
	// renders the home page's Text — this line is how that briefing still
	// reaches the glass, on the page the wearer actually lands on.
	Counts string
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

	ctx := r.Context()
	now := h.now().In(dentime.Location())
	if force := r.URL.Query().Get("fresh"); force == "1" || force == "true" {
		bundle := BuildGlance(ctx, now, h.sources)
		h.storeGlanceCache(bundle, now)
		writeGlanceJSON(w, bundle, now, false)
		return
	}
	if bundle, at, ok := h.lookupGlanceCache(now); ok {
		writeGlanceJSON(w, bundle, at, true)
		return
	}
	bundle := BuildGlance(ctx, now, h.sources)
	h.storeGlanceCache(bundle, now)
	writeGlanceJSON(w, bundle, now, false)
}

func writeGlanceJSON(w http.ResponseWriter, bundle GlanceBundle, at time.Time, cached bool) {
	writeJSON(w, http.StatusOK, map[string]any{
		"text":      bundle.Text,
		"pages":     bundle.Pages,
		"items":     bundle.Items,
		"now":       bundle.Now,
		"counts":    bundle.Counts,
		"generated": at.Format(time.RFC3339),
		"cached":    cached,
	})
}

func FormatGlance(ctx context.Context, now time.Time, src GlanceSources) string {
	return BuildGlance(ctx, now, src).Text
}

func BuildGlance(ctx context.Context, now time.Time, src GlanceSources) GlanceBundle {
	now = now.In(dentime.Location())
	var events []GlanceEvent
	var todos []GlanceTodo
	var urgent []GlanceUrgent
	if src.Events != nil {
		events = src.Events(ctx, now)
	}
	if src.Todos != nil {
		todos = src.Todos(ctx, now)
	}
	if src.Urgent != nil {
		urgent = src.Urgent(ctx, now)
	}

	homeText, homeEmpty := formatHomePage(now, events, urgent, todos)
	alertText, alertEmpty := formatAlertsPage(urgent, now)
	calText, calEmpty := formatCalPage(events, now)
	todoText, todoEmpty := formatTodoPage(todos, now)

	pages := []GlancePage{
		{ID: "home", Title: "알림", Text: homeText, Empty: homeEmpty},
		{ID: "alerts", Title: "알림 전체", Text: alertText, Empty: alertEmpty},
		{ID: "cal", Title: "일정", Text: calText, Empty: calEmpty},
		{ID: "todo", Title: "할 일", Text: todoText, Empty: todoEmpty},
	}
	// Alerts are excluded: the alert page shows its own count, and repeating it
	// would spend one of about ten usable lines saying the same thing twice.
	calN := len(events)
	todoN := len(rankTodos(todos, now))
	return GlanceBundle{
		Now:    formatNowLine(events, now),
		Text:   homeText,
		Pages:  pages,
		Items:  buildGlanceItems(urgent, now),
		Counts: formatCountsLine(calN, 0, todoN),
	}
}

func buildGlanceItems(items []GlanceUrgent, now time.Time) []GlanceItem {
	sorted := sortUrgent(items)
	out := make([]GlanceItem, 0, len(sorted))
	for i, it := range sorted {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		id := strings.TrimSpace(it.ID)
		if id == "" {
			id = "alert-" + strconv.Itoa(i+1)
		}
		preview := strings.TrimSpace(it.Preview)
		body := strings.TrimSpace(it.Body)
		if body == "" {
			body = preview
		}
		if body == "" {
			body = title
		}
		// Keep detail readable on G2 (~400 char practical HUD budget).
		body = CleanForG2(body)
		body = truncateRunes(body, 280)
		preview = truncateRunes(CleanForG2(preview), 80)
		out = append(out, GlanceItem{
			ID:       id,
			Title:    truncateRunes(title, 48),
			Preview:  preview,
			Body:     body,
			Priority: it.Priority,
			Age:      formatAge(it.CreatedAt, now),
		})
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func cleanPage(s string) string {
	s = CleanForG2(s)
	return truncateRunes(s, pageMaxRunes)
}

func formatHomePage(now time.Time, events []GlanceEvent, urgent []GlanceUrgent, todos []GlanceTodo) (string, bool) {
	sorted := sortUrgent(urgent)
	n := countNonEmptyUrgent(sorted)
	var lines []string
	lines = append(lines, formatClockHeader(now))
	if n == 0 {
		lines = append(lines, "새 알림 없음")
		current, upcoming := splitEvents(events, now)
		ranked := rankTodos(todos, now)
		calN := len(upcoming)
		if current != nil {
			calN++
		}
		hint := formatCountsLine(calN, 0, len(ranked))
		if hint != "" {
			lines = append(lines, hint+" · ↓스와이프")
		} else {
			lines = append(lines, "탭=새로고침")
		}
		return cleanPage(strings.Join(lines, "\n")), true
	}
	lines = append(lines, "알림 "+strconv.Itoa(n)+"건")
	shown := 0
	for _, it := range sorted {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		mark := "· "
		if it.Priority >= 4 {
			mark = "! "
		}
		line := mark + truncateRunes(title, 26)
		if age := formatAge(it.CreatedAt, now); age != "" {
			line += " · " + age
		}
		lines = append(lines, line)
		shown++
		if shown >= 5 {
			break
		}
	}
	if n > shown {
		lines = append(lines, "↓전체 "+strconv.Itoa(n-shown)+"건 더 · 탭상세")
	} else {
		lines = append(lines, "탭=상세 · ↓전체")
	}
	return cleanPage(strings.Join(lines, "\n")), false
}

func formatClockHeader(now time.Time) string {
	weekdays := []string{"일", "월", "화", "수", "목", "금", "토"}
	wd := weekdays[int(now.Weekday())]
	return now.Format("1/2") + " " + wd + " " + now.Format("15:04")
}

func formatCountsLine(calN, urgentN, todoN int) string {
	parts := make([]string, 0, 3)
	if calN > 0 {
		parts = append(parts, "일정 "+strconv.Itoa(calN))
	}
	if urgentN > 0 {
		parts = append(parts, "알림 "+strconv.Itoa(urgentN))
	}
	if todoN > 0 {
		parts = append(parts, "할 일 "+strconv.Itoa(todoN))
	}
	return strings.Join(parts, " · ")
}

func formatCalPage(events []GlanceEvent, now time.Time) (string, bool) {
	current, upcoming := splitEvents(events, now)
	total := len(upcoming)
	if current != nil {
		total++
	}
	var lines []string
	if current != nil {
		line := "지금 " + truncateRunes(current.Summary, 26)
		if rem := formatRemaining(current, now); rem != "" {
			line += " · " + rem
		}
		lines = append(lines, line)
	}
	for _, ev := range upcoming {
		if len(lines) >= 5 {
			break
		}
		when := formatNextRelative(ev, now)
		if !strings.Contains(when, "분") && !strings.Contains(when, "곧") {
			when = formatEventWhen(ev, now)
		}
		lines = append(lines, when+" "+truncateRunes(ev.Summary, 24))
	}
	if len(lines) == 0 {
		return cleanPage("예정된 일정이 없어요."), true
	}
	header := "일정 " + strconv.Itoa(total) + "건"
	return cleanPage(header + "\n" + strings.Join(lines, "\n")), false
}

func formatAlertsPage(items []GlanceUrgent, now time.Time) (string, bool) {
	sorted := sortUrgent(items)
	var lines []string
	for _, it := range sorted {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		mark := "· "
		if it.Priority >= 4 {
			mark = "! "
		}
		line := mark + truncateRunes(title, 28)
		if age := formatAge(it.CreatedAt, now); age != "" {
			line += " · " + age
		}
		if prev := strings.TrimSpace(it.Preview); prev != "" && prev != title {
			line += "\n  " + truncateRunes(prev, 30)
		}
		lines = append(lines, line)
		if len(lines) >= 8 {
			break
		}
	}
	if len(lines) == 0 {
		return cleanPage("새 알림이 없어요."), true
	}
	n := countNonEmptyUrgent(sorted)
	header := "알림 " + strconv.Itoa(n) + "건"
	return cleanPage(header + "\n" + strings.Join(lines, "\n")), false
}

func formatAge(created, now time.Time) string {
	if created.IsZero() || now.Before(created) {
		return ""
	}
	mins := int(now.Sub(created).Minutes())
	if mins < 1 {
		return "방금"
	}
	if mins < 60 {
		return strconv.Itoa(mins) + "분 전"
	}
	hours := mins / 60
	if hours < 24 {
		return strconv.Itoa(hours) + "시간 전"
	}
	days := hours / 24
	if days < 7 {
		return strconv.Itoa(days) + "일 전"
	}
	return created.Format("1/2")
}

func formatTodoPage(todos []GlanceTodo, now time.Time) (string, bool) {
	ranked := rankTodos(todos, now)
	if len(ranked) == 0 {
		return cleanPage("오늘 볼 할 일이 없어요."), true
	}
	var lines []string
	for _, td := range ranked {
		if len(lines) >= 6 {
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
	return cleanPage(header + "\n" + strings.Join(lines, "\n")), false
}

func formatRemaining(ev *GlanceEvent, now time.Time) string {
	if ev == nil || ev.AllDay {
		return ""
	}
	end := ev.End
	if end.IsZero() {
		end = ev.Start.Add(time.Hour)
	}
	if !end.After(now) {
		return ""
	}
	mins := int(end.Sub(now).Minutes())
	if mins < 1 {
		return "곧 끝"
	}
	if mins < 180 {
		return "종료 " + strconv.Itoa(mins) + "분"
	}
	return "종료 " + end.Format("15:04")
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

// ErrAckUnavailable means the workfeed store is not up yet, which is a 503 and
// not a missing alert — the difference matters to the wearer, who should retry
// rather than assume the card is gone.
var ErrAckUnavailable = errors.New("workfeed unavailable")

// Ack marks one glance alert handled, from the glasses.
//
// The HUD was read-only: an alert could be read on the glass but only cleared
// from the phone or the desktop, so the same three items kept arriving in the
// wearer's field of view all morning. This is the smallest write that changes
// that — it moves a workfeed card to StatusAcked, which evenGlanceUrgent
// already filters out, so the next poll simply stops showing it.
func (h *Handler) Ack(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "even g2 bridge unavailable", http.StatusServiceUnavailable)
		return
	}
	if !h.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "even g2 bridge disabled (set "+EnvBridgeToken+")")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	presented := ParseBearer(r.Header.Get("Authorization"))
	if !tokenMatch(h.token, presented) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.ackAlert == nil {
		writeErr(w, http.StatusServiceUnavailable, "workfeed unavailable")
		return
	}

	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json")
		return
	}
	id := strings.TrimSpace(body.ID)
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id required")
		return
	}
	// Synthetic ids are assigned by buildGlanceItems when the source card had
	// none; they address nothing, so acking one would silently do nothing.
	if strings.HasPrefix(id, "alert-") {
		writeErr(w, http.StatusBadRequest, "unaddressable alert id")
		return
	}
	if err := h.ackAlert(id); err != nil {
		if errors.Is(err, ErrAckUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	// The wearer just changed what the glance should say; drop the 8s cache so
	// the confirming refresh does not hand back the pre-ack list.
	h.invalidateGlanceCache()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// invalidateGlanceCache forces the next Glance call to rebuild.
func (h *Handler) invalidateGlanceCache() {
	h.glanceCache.mu.Lock()
	h.glanceCache.at = time.Time{}
	h.glanceCache.mu.Unlock()
}

// formatNowLine condenses the calendar to the single line worth putting at the
// top of a HUD: what is happening now, or what is next.
//
// The same facts already exist inside the home page's text, but the plugin
// draws the alert page from Items and never renders that text — so on the glass
// this had simply gone missing.
func formatNowLine(events []GlanceEvent, now time.Time) string {
	current, upcoming := splitEvents(events, now)
	if current != nil {
		line := "지금 " + truncateRunes(current.Summary, 20)
		if rem := formatRemaining(current, now); rem != "" {
			line += " · " + rem
		}
		return line
	}
	if len(upcoming) == 0 {
		return ""
	}
	next := upcoming[0]
	when := formatNextRelative(next, now)
	if !strings.Contains(when, "분") && !strings.Contains(when, "곧") {
		when = formatEventWhen(next, now)
	}
	return when + " " + truncateRunes(next.Summary, 20)
}
