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

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
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

// resolveBriefingCalendarClient is the production factory: explicitly
// converts (nil, err) into a typed nil so a nil-interface check
// downstream never sees a non-nil interface wrapping a nil concrete.
func resolveBriefingCalendarClient() (briefingCalendarClient, error) {
	c, err := calendar.DefaultClient()
	if err != nil {
		return nil, err
	}
	return c, nil
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
		if err := s.sendBriefing(ev); err != nil {
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
func (s *calendarBriefingService) sendBriefing(ev calendar.Event) error {
	delivered, err := s.deliver(s.formatBriefing(ev))
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
