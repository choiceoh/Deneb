// calendar_briefing.go — sends a "meeting in 15 min" push to the native
// client when any calendar event's start falls inside the lead-time window.
//
// Delivery is native-only: the briefing body is appended to the client:main
// (업무) transcript and live-pushed to connected native clients via the same
// proactive-relay path that gmail polling and wiki dreaming use. There is no
// Telegram send — the bot has been retired.
//
// Lifecycle:
//   - newCalendarBriefingService() returns nil only when the native deliver
//     callback or Calendar resolver are wholly missing.
//   - start(ctx) launches a single polling goroutine bound to the server's
//     ShutdownCtx; the goroutine exits cleanly on ctx.Done().
//   - The goroutine has a defer-recover via pkg/safego so a panic in any
//     downstream call (Calendar refresh, native delivery) cannot crash the
//     gateway.
//
// Dedup:
//   - sentMu-guarded map keyed by eventID+UnixStart so a rescheduled
//     event (same Google ID, different start) gets its own briefing.
//   - On each tick, entries whose event-start is more than 30 min in the
//     past are pruned so the map cannot grow unbounded.
//
// Window math:
//   - Default lead time is 15 min, window width 2 min on each side, so an
//     event with start in [now+13min, now+17min] triggers exactly once.
//   - The +/-2 min slack absorbs poll jitter so a 60s poll interval never
//     misses an event.
//
// Failure handling:
//   - briefingDecision is computed in a pure function (decidePushes) so
//     test and production share the exact same logic — no test "mirror"
//     to drift.
//   - Repeated client-unavailable failures (no OAuth tokens) are Warn the
//     first time, then suppressed until tokens land — no per-minute log
//     spam over a day.

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	calendarPollInterval = 1 * time.Minute
	calendarLeadTime     = 15 * time.Minute
	calendarWindowSlack  = 2 * time.Minute
	calendarLookahead    = 30 * time.Minute // how far ahead we ask Calendar for
	calendarMaxResults   = 50

	// kstFallbackOffset is used when LoadLocation("Asia/Seoul") fails
	// (containers without tzdata). Single-user KST deployment is the
	// product contract, so a fixed offset is correct here rather than
	// time.Local which silently equals UTC on stripped images.
	kstFallbackOffset = 9 * 60 * 60
)

const (
	// briefEnrichTimeout bounds the whole context-enrichment pass (all
	// attendee + topic lookups for one event). The briefing fires once per
	// meeting at D-15, so this is not user-facing latency — it only caps how
	// long a slow Gmail/wiki backend can hold the briefing goroutine before
	// we ship the base briefing anyway.
	briefEnrichTimeout = 8 * time.Second

	// briefAttendeeCap limits how many external attendees get a context line
	// so a 30-person invite doesn't produce a 30-line push.
	briefAttendeeCap = 3

	// briefMailCountDays / briefMailCountCap: the per-attendee freshness
	// signal counts messages from that sender within the window. We cap the
	// page because only the count matters, not the messages themselves.
	briefMailCountDays = 30
	briefMailCountCap  = 25

	// briefTopicMailCap limits topic-related recent mail subjects shown.
	briefTopicMailCap = 3
)

// errBriefingUndelivered is returned by sendBriefing when the native relay
// reports the body was not delivered (no transcript store wired). Surfaced so
// tick() skips markSent and retries on the next poll.
var errBriefingUndelivered = errors.New("calendar briefing: native delivery not wired")

// briefingCalendarClient is the slice of *calendar.Client we depend on.
// Function-typed factory matches the lazy resolver in method_registry.go
// so the service handles "OAuth not yet configured" gracefully.
type briefingCalendarClient interface {
	ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error)
}

// mergedBriefingSource unions the read-only Google client with the local store
// so hand-added events also trigger the D-15min reminder. Either field may be
// nil; a Google list error propagates (so a configured-but-broken Google calendar
// is noticed), while local events are always included.
type mergedBriefingSource struct {
	google briefingCalendarClient // nil when Google OAuth isn't configured
	local  *localcal.Store        // nil when the local file can't be read
}

func (m mergedBriefingSource) ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error) {
	var out []calendar.Event
	if m.google != nil {
		ev, err := m.google.ListUpcoming(ctx, from, to, maxResults)
		if err != nil {
			return nil, err
		}
		out = append(out, ev...)
	}
	if m.local != nil {
		out = append(out, m.local.ListRange(from, to)...)
	}
	return out, nil
}

// resolveBriefingCalendarClient is the production factory: it merges the Google
// client (when configured) with the local store so briefings cover both. When
// neither is available it returns the Google error so the operator is told.
func resolveBriefingCalendarClient() (briefingCalendarClient, error) {
	c, gerr := calendar.DefaultClient()
	local, _ := localcal.Default()
	var google briefingCalendarClient
	if gerr == nil {
		google = c
	}
	if google == nil && local == nil {
		return nil, gerr
	}
	return mergedBriefingSource{google: google, local: local}, nil
}

type calendarBriefingService struct {
	// deliver sends a pre-formatted briefing body to the native client (업무
	// transcript + live push). Returns (delivered, err); (false, nil) means
	// the relay had nothing to write to (no transcript store), treated as a
	// soft "not ready" so the next tick retries.
	deliver   func(text string) (bool, error)
	resolve   func() (briefingCalendarClient, error)
	logger    *slog.Logger
	leadTime  time.Duration
	pollEvery time.Duration

	// enricher adds best-effort context (attendee mail freshness + wiki
	// notes, related recent mail, past wiki record) to the base briefing.
	// Optional and set after construction: nil → base briefing only, so the
	// nil-receiver guards and existing tests stay unaffected.
	enricher *briefingEnricher

	// displayLoc is loaded once at service construction. Cached because
	// LoadLocation is a non-trivial lookup on each formatBriefing call.
	// When zoneinfo is unavailable we use a fixed-offset KST fallback;
	// see kstFallbackOffset for the contract.
	displayLoc *time.Location

	sentMu sync.Mutex
	// sent is the dedup map. Key is eventID|UnixStart so a rescheduled
	// event with the same Google ID but a different start time gets its
	// own briefing — prior key cannot suppress the new push.
	sent map[string]time.Time

	// resolveFailureMu guards the "client unavailable" log throttle. We
	// log the first failure at Warn, then suppress identical follow-ups
	// until the next success — prevents minute-cadence spam for an
	// operator who hasn't configured OAuth yet.
	resolveFailureMu sync.Mutex
	resolveFailing   bool
}

// newCalendarBriefingService returns nil only when the native deliver
// callback or resolver are absent — both are structural prerequisites.
func newCalendarBriefingService(
	deliver func(text string) (bool, error),
	resolve func() (briefingCalendarClient, error),
	logger *slog.Logger,
) *calendarBriefingService {
	if deliver == nil || resolve == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		// Asia/Seoul zoneinfo is normally bundled with Linux distros; a
		// missing entry means we're on a stripped container image. Use
		// a fixed +09:00 offset so the briefing wall-clock time stays
		// correct rather than silently flipping to UTC via time.Local.
		logger.Warn("calendar briefing: Asia/Seoul tzdata missing, using fixed +09:00 fallback",
			"error", err)
		loc = time.FixedZone("KST", kstFallbackOffset)
	}
	return &calendarBriefingService{
		deliver:    deliver,
		resolve:    resolve,
		logger:     logger,
		leadTime:   calendarLeadTime,
		pollEvery:  calendarPollInterval,
		displayLoc: loc,
		sent:       make(map[string]time.Time),
	}
}

// start launches the polling goroutine. Returns immediately. The
// goroutine exits when ctx is canceled (typically server shutdown).
//
// The nil-receiver guard MUST stay the first statement: production
// wiring calls s.calendarBriefing.start(...) unconditionally so that
// a nil service (no deliver callback) is a safe no-op rather than a
// nil-deref crash inside safego's panic-recovery.
func (s *calendarBriefingService) start(ctx context.Context) {
	if s == nil {
		return
	}
	safego.GoWithSlog(s.logger, "calendar-briefing", func() {
		s.run(ctx)
	})
}

func (s *calendarBriefingService) run(ctx context.Context) {
	ticker := time.NewTicker(s.pollEvery)
	defer ticker.Stop()

	// Probe once immediately so a freshly-restarted gateway doesn't
	// miss a meeting starting in the next minute. Subsequent ticks fall
	// on the regular cadence.
	s.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("calendar briefing service stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *calendarBriefingService) tick(ctx context.Context) {
	client, err := s.resolve()
	if err != nil {
		s.logResolveFailure(err)
		return
	}
	// Successful resolve clears the failure-throttle so the next outage
	// is logged again (instead of staying suppressed forever).
	s.clearResolveFailure()

	now := time.Now()
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	events, err := client.ListUpcoming(fetchCtx, now, now.Add(calendarLookahead), calendarMaxResults)
	if err != nil {
		s.logger.Warn("calendar briefing: fetch failed", "error", err)
		return
	}

	pushes := decidePushes(now, s.leadTime, events, s.alreadySent)
	for _, ev := range pushes {
		if err := s.sendBriefing(ctx, ev); err != nil {
			s.logger.Error("calendar briefing: push failed",
				"eventId", ev.ID, "summary", ev.Summary, "error", err)
			continue
		}
		s.markSent(dedupKey(ev), ev.Start)
	}

	s.prune(now)
}

// decidePushes is a pure function from (now, lead, events, alreadySent
// predicate) to the list of events that should be pushed on this tick.
// Production tick() and tests both call this so behavior cannot diverge
// silently — there is no separate "test mirror" to drift.
func decidePushes(
	now time.Time,
	lead time.Duration,
	events []calendar.Event,
	alreadySent func(string) bool,
) []calendar.Event {
	windowMin := now.Add(lead - calendarWindowSlack)
	windowMax := now.Add(lead + calendarWindowSlack)
	var out []calendar.Event
	for _, ev := range events {
		if ev.AllDay {
			// All-day events shouldn't trigger 15-min briefings —
			// users don't need a "wake up, today is your birthday"
			// at 23:45 the night before.
			continue
		}
		if ev.Start.IsZero() || ev.Start.Before(windowMin) || ev.Start.After(windowMax) {
			continue
		}
		if alreadySent(dedupKey(ev)) {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// dedupKey is eventID|UnixStart so a rescheduled event with the same
// Google ID but a different start time generates a new key. UnixStart
// uses the absolute time, immune to user's display timezone.
func dedupKey(ev calendar.Event) string {
	return ev.ID + "|" + fmt.Sprintf("%d", ev.Start.Unix())
}

// sendBriefing assembles and delivers the briefing to the native client.
// Plain text (no HTML escaping) because the native client renders the
// transcript body directly — unlike the retired Telegram HTML parse mode.
//
// The base briefing (formatBriefing) always ships; context enrichment is
// strictly additive and best-effort, so a slow or absent Gmail/wiki backend
// degrades to the bare reminder rather than blocking or dropping it.
func (s *calendarBriefingService) sendBriefing(ctx context.Context, ev calendar.Event) error {
	body := s.formatBriefing(ev)
	if s.enricher != nil {
		if extra := s.enricher.extra(ctx, ev); extra != "" {
			body += "\n" + extra
		}
	}
	delivered, err := s.deliver(body)
	if err != nil {
		return err
	}
	if !delivered {
		return errBriefingUndelivered
	}
	return nil
}

// formatBriefing builds the briefing body. Korean-first per project
// convention; time rendered in the cached displayLoc. Plain text — the
// native client renders it directly, so no markup escaping is needed.
func (s *calendarBriefingService) formatBriefing(ev calendar.Event) string {
	start := ev.Start.In(s.displayLoc).Format("15:04")

	title := strings.TrimSpace(ev.Summary)
	if title == "" {
		title = "(제목 없음)"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🕒 D-%d분  %s\n", int(s.leadTime.Minutes()), title)
	fmt.Fprintf(&b, "시작: %s", start)
	if location := strings.TrimSpace(ev.Location); location != "" {
		fmt.Fprintf(&b, "\n📍 %s", location)
	}
	if names := attendeeNames(ev.Attendees, 4); names != "" {
		fmt.Fprintf(&b, "\n👤 %s", names)
	}
	return b.String()
}

// attendeeNames returns up to `limit` non-self, non-declined attendee
// labels, joined with " · ". Declined are filtered because surfacing
// "X declined" people as expected counterparts misleads the operator.
func attendeeNames(attendees []calendar.Attendee, limit int) string {
	var picks []string
	for _, a := range attendees {
		if a.Self {
			continue
		}
		if a.ResponseStatus == "declined" {
			continue
		}
		label := strings.TrimSpace(a.DisplayName)
		if label == "" {
			label = strings.TrimSpace(a.Email)
		}
		if label == "" {
			continue
		}
		picks = append(picks, label)
		if len(picks) >= limit {
			break
		}
	}
	return strings.Join(picks, " · ")
}

// externalAttendees returns up to `limit` attendees who are neither the
// authenticated user (Self) nor declined — the counterparts the operator is
// actually meeting. limit <= 0 means no cap. This is the enrichment-side
// filter: it decides who gets a *context line*, not whether the briefing
// fires (decidePushes is unchanged, so solo events still get their D-15
// reminder).
func externalAttendees(attendees []calendar.Attendee, limit int) []calendar.Attendee {
	var out []calendar.Attendee
	for _, a := range attendees {
		if a.Self || a.ResponseStatus == "declined" {
			continue
		}
		out = append(out, a)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// briefingEnricher gathers optional context for a meeting briefing:
//   - per external attendee: recent-mail frequency + a one-line wiki note
//   - topic-related recent mail (search by meeting title keywords)
//   - a past wiki record matching the meeting title
//
// Every lookup is best-effort. A nil func, an error, or a timeout simply
// omits that line, so the base briefing always ships. Sequential by design
// (project rule: prefer simple sequential over concurrency); the whole pass
// is bounded by `timeout`. The function-typed deps keep this unit-testable
// without constructing a real Gmail/wiki backend.
type briefingEnricher struct {
	// recentMailCount returns how many messages from `email` arrived within
	// the last `days`. A negative count or an error omits the freshness line.
	recentMailCount func(ctx context.Context, email string, days int) (int, error)
	// topicMail returns up to `limit` recent mail subjects matching a free-text
	// topic query (the meeting title).
	topicMail func(ctx context.Context, query string, limit int) ([]string, error)
	// wikiNote returns a one-line note ("Title — Summary") for a person or
	// topic query, or "" when nothing is in memory.
	wikiNote func(ctx context.Context, query string) string

	timeout time.Duration
	logger  *slog.Logger
}

// newBriefingEnricher wires the enricher to the production Gmail client and
// the (late-bound) wiki store. wikiGetter is read at call time so the briefing
// service, constructed before the wiki store exists, never captures a nil
// store. All backends degrade gracefully: no Gmail OAuth → no mail lines, no
// wiki store → no notes.
func newBriefingEnricher(wikiGetter func() *wiki.Store, logger *slog.Logger) *briefingEnricher {
	if logger == nil {
		logger = slog.Default()
	}
	return &briefingEnricher{
		recentMailCount: func(ctx context.Context, email string, days int) (int, error) {
			c, err := gmail.DefaultClient()
			if err != nil {
				return -1, err
			}
			// Quote the address so operator characters in the local part are
			// treated as part of the address, not Gmail search syntax.
			res, err := c.Search(ctx, fmt.Sprintf("from:%q newer_than:%dd", email, days), briefMailCountCap)
			if err != nil {
				return -1, err
			}
			return len(res), nil
		},
		topicMail: func(ctx context.Context, query string, limit int) ([]string, error) {
			c, err := gmail.DefaultClient()
			if err != nil {
				return nil, err
			}
			res, err := c.Search(ctx, query, limit)
			if err != nil {
				return nil, err
			}
			subjects := make([]string, 0, len(res))
			for _, r := range res {
				if sub := strings.TrimSpace(r.Subject); sub != "" {
					subjects = append(subjects, sub)
				}
			}
			return subjects, nil
		},
		wikiNote: func(ctx context.Context, query string) string {
			return wikiTopNote(ctx, wikiGetter, query)
		},
		timeout: briefEnrichTimeout,
		logger:  logger,
	}
}

// extra returns the additional briefing lines for an event (without a leading
// newline). Wrapped in a bounded timeout and panic recovery so enrichment can
// never block or crash the briefing goroutine — on any failure it returns "".
func (e *briefingEnricher) extra(ctx context.Context, ev calendar.Event) (out string) {
	if e == nil {
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("calendar briefing: enrich panic recovered", "panic", r)
			out = ""
		}
	}()

	timeout := e.timeout
	if timeout <= 0 {
		timeout = briefEnrichTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var b strings.Builder

	// 참석자 컨텍스트: 외부 참석자별 최근 메일 빈도 + 위키 메모.
	for _, a := range externalAttendees(ev.Attendees, briefAttendeeCap) {
		if line := e.attendeeLine(ctx, a); line != "" {
			b.WriteString("\n")
			b.WriteString(line)
		}
	}

	// 관련 최근 메일: 회의 제목 키워드로 검색.
	if e.topicMail != nil {
		if q := briefTopicQuery(ev.Summary); q != "" {
			if subs, err := e.topicMail(ctx, q, briefTopicMailCap); err == nil && len(subs) > 0 {
				b.WriteString("\n📧 관련 메일: ")
				b.WriteString(strings.Join(subs, " / "))
			}
		}
	}

	// 지난 기록: 회의 제목으로 위키 검색 (프로젝트/거래처 페이지).
	if e.wikiNote != nil {
		if note := e.wikiNote(ctx, strings.TrimSpace(ev.Summary)); note != "" {
			b.WriteString("\n📌 지난 기록: ")
			b.WriteString(note)
		}
	}

	return strings.TrimSpace(b.String())
}

// attendeeLine builds one "· name — 최근 30일 N건, wiki note" line for an
// attendee, or "" when no signal is available for them.
func (e *briefingEnricher) attendeeLine(ctx context.Context, a calendar.Attendee) string {
	name := strings.TrimSpace(a.DisplayName)
	email := strings.TrimSpace(a.Email)
	if name == "" {
		name = email
	}
	if name == "" {
		return ""
	}

	var parts []string
	// Attendee.Email is normalized to lowercase by the calendar client; a
	// plain "@" check is enough to avoid quoting a bare name into the query.
	if e.recentMailCount != nil && strings.Contains(email, "@") {
		if n, err := e.recentMailCount(ctx, email, briefMailCountDays); err == nil && n >= 0 {
			parts = append(parts, fmt.Sprintf("최근 %d일 %d건", briefMailCountDays, n))
		}
	}
	if e.wikiNote != nil {
		if note := e.wikiNote(ctx, name); note != "" {
			parts = append(parts, note)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("· %s — %s", name, strings.Join(parts, ", "))
}

// briefTopicQuery turns a meeting title into a plain Gmail keyword query by
// dropping characters Gmail treats as operators/grouping. Returns "" for
// titles too short to search usefully.
func briefTopicQuery(summary string) string {
	s := strings.Map(func(r rune) rune {
		switch r {
		case '[', ']', '(', ')', '{', '}', '"', ':':
			return ' '
		}
		return r
	}, summary)
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) < 2 {
		return ""
	}
	return s
}

// wikiTopNote returns a one-line "Title — Summary" for the best wiki hit on
// query, or "" when the store is unavailable / nothing matches. The store is
// resolved lazily so a nil (not-yet-initialized) store is a clean skip.
func wikiTopNote(ctx context.Context, getStore func() *wiki.Store, query string) string {
	query = strings.TrimSpace(query)
	if getStore == nil || query == "" {
		return ""
	}
	st := getStore()
	if st == nil {
		return ""
	}
	hits, err := st.Search(ctx, query, 1)
	if err != nil || len(hits) == 0 {
		return ""
	}
	page, err := st.ReadPage(hits[0].Path)
	if err != nil || page == nil {
		return ""
	}
	title := strings.TrimSpace(page.Meta.Title)
	summary := strings.TrimSpace(page.Meta.Summary)
	switch {
	case title != "" && summary != "":
		return title + " — " + summary
	case title != "":
		return title
	default:
		return summary
	}
}

// alreadySent / markSent / prune guard the dedup map.

func (s *calendarBriefingService) alreadySent(key string) bool {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	_, ok := s.sent[key]
	return ok
}

func (s *calendarBriefingService) markSent(key string, eventStart time.Time) {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	s.sent[key] = eventStart
}

// prune removes dedup entries whose event start is more than 30 minutes
// in the past. Bounds the map size in steady state.
func (s *calendarBriefingService) prune(now time.Time) {
	cutoff := now.Add(-30 * time.Minute)
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	for id, start := range s.sent {
		if start.Before(cutoff) {
			delete(s.sent, id)
		}
	}
}

// --- log throttles --------------------------------------------------------

func (s *calendarBriefingService) logResolveFailure(err error) {
	s.resolveFailureMu.Lock()
	first := !s.resolveFailing
	s.resolveFailing = true
	s.resolveFailureMu.Unlock()
	if first {
		s.logger.Warn("calendar briefing: client unavailable (suppressing repeats until recovery)",
			"error", err)
	}
}

func (s *calendarBriefingService) clearResolveFailure() {
	s.resolveFailureMu.Lock()
	defer s.resolveFailureMu.Unlock()
	s.resolveFailing = false
}
