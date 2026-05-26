// calendar_briefing.go — sends a Telegram "meeting in 15 min" push when
// any calendar event's start falls inside the lead-time window.
//
// Lifecycle:
//   - newCalendarBriefingService() returns nil unless both Telegram plugin
//     and Calendar client factory are present, so wiring callers always
//     nil-check before invoking start().
//   - start(ctx) launches a single polling goroutine bound to the server's
//     ShutdownCtx; the goroutine exits cleanly on ctx.Done().
//   - The goroutine has a defer-recover via pkg/safego so a panic in any
//     downstream call (Calendar refresh, Telegram send) cannot crash the
//     gateway. Failures are logged at Error level when user-impacting
//     (push dropped) and Warn for transient calendar fetch errors.
//
// Dedup:
//   - sentMu-guarded map keyed by event ID, value = wall-clock send time.
//   - On each tick, entries whose event-start is more than 30 min in the
//     past are pruned so the map cannot grow unbounded.
//
// Window math:
//   - Default lead time is 15 min, window width 2 min on each side, so an
//     event with start in [now+13min, now+17min] triggers exactly once.
//   - The +/-2 min slack absorbs poll jitter so a 60s poll interval never
//     misses an event.

package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	calendarPollInterval = 1 * time.Minute
	calendarLeadTime     = 15 * time.Minute
	calendarWindowSlack  = 2 * time.Minute
	calendarLookahead    = 30 * time.Minute // how far ahead we ask Calendar for
	calendarMaxResults   = 50
)

// briefingCalendarClient is the slice of *calendar.Client we depend on.
// Function-typed factory matches the lazy resolver in method_registry.go
// so the service handles "OAuth not yet configured" gracefully.
type briefingCalendarClient interface {
	ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error)
}

// resolveBriefingCalendarClient is the production factory: it wraps
// calendar.DefaultClient() in the interface form so callers can swap
// fakes in tests without touching the global singleton.
func resolveBriefingCalendarClient() (briefingCalendarClient, error) {
	return calendar.DefaultClient()
}

type calendarBriefingService struct {
	plugin    *telegram.Plugin
	resolve   func() (briefingCalendarClient, error)
	chatID    string
	logger    *slog.Logger
	leadTime  time.Duration
	pollEvery time.Duration

	sentMu sync.Mutex
	sent   map[string]time.Time // eventID → wall-clock time we sent the push
}

// newCalendarBriefingService returns nil when either dependency is
// missing — that keeps the lifecycle wiring (start/stop) a no-op rather
// than an error, matching notifyService's pattern.
func newCalendarBriefingService(
	plug *telegram.Plugin,
	resolve func() (briefingCalendarClient, error),
	logger *slog.Logger,
) *calendarBriefingService {
	if plug == nil || resolve == nil {
		return nil
	}
	chatID := plug.PrimaryChatID()
	if chatID == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &calendarBriefingService{
		plugin:    plug,
		resolve:   resolve,
		chatID:    chatID,
		logger:    logger,
		leadTime:  calendarLeadTime,
		pollEvery: calendarPollInterval,
		sent:      make(map[string]time.Time),
	}
}

// start launches the polling goroutine. Returns immediately. The
// goroutine exits when ctx is canceled (typically server shutdown).
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
		// Expected when OAuth tokens are missing — Warn (not Error) so
		// operator can configure later without log noise on every tick.
		// Single sample line per tick is fine; this is a 1-minute cadence.
		s.logger.Warn("calendar briefing: client unavailable", "error", err)
		return
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	events, err := client.ListUpcoming(fetchCtx, time.Now(), time.Now().Add(calendarLookahead), calendarMaxResults)
	if err != nil {
		s.logger.Warn("calendar briefing: fetch failed", "error", err)
		return
	}

	now := time.Now()
	windowMin := now.Add(s.leadTime - calendarWindowSlack)
	windowMax := now.Add(s.leadTime + calendarWindowSlack)

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
		if s.alreadySent(ev.ID) {
			continue
		}
		if err := s.sendBriefing(ctx, ev); err != nil {
			// User-observable failure — Error per logging.md.
			s.logger.Error("calendar briefing: push failed",
				"eventId", ev.ID, "summary", ev.Summary, "error", err)
			continue
		}
		s.markSent(ev.ID, ev.Start)
	}

	s.prune(now)
}

// sendBriefing assembles and posts the briefing message. Plain text by
// default — inline keyboards / mini-app buttons can be added once the
// detail view is shipped.
func (s *calendarBriefingService) sendBriefing(ctx context.Context, ev calendar.Event) error {
	sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.plugin.SendMessage(sendCtx, telegram.OutboundMessage{
		To:   s.chatID,
		Text: formatBriefing(ev, s.leadTime),
	})
}

// formatBriefing builds the Telegram message body. Korean-first per
// project convention; time formatted in Asia/Seoul (single-user
// deployment, no timezone configuration).
func formatBriefing(ev calendar.Event, lead time.Duration) string {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		loc = time.Local
	}
	start := ev.Start.In(loc).Format("15:04")

	title := strings.TrimSpace(ev.Summary)
	if title == "" {
		title = "(제목 없음)"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🕒 D-%d분  %s\n", int(lead.Minutes()), title)
	fmt.Fprintf(&b, "시작: %s", start)
	if loc := strings.TrimSpace(ev.Location); loc != "" {
		fmt.Fprintf(&b, "\n📍 %s", loc)
	}
	if ev.Conference != nil && ev.Conference.URI != "" {
		fmt.Fprintf(&b, "\n🔗 %s", ev.Conference.URI)
	}
	if len(ev.Attendees) > 0 {
		names := attendeeNames(ev.Attendees, 4)
		if names != "" {
			fmt.Fprintf(&b, "\n👤 %s", names)
		}
	}
	return b.String()
}

// attendeeNames returns up to `limit` non-self attendee labels, joined
// with " · ". Self is filtered because "you are invited to your own
// meeting" is noise.
func attendeeNames(attendees []calendar.Attendee, limit int) string {
	var picks []string
	for _, a := range attendees {
		if a.Self {
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

func (s *calendarBriefingService) alreadySent(eventID string) bool {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	_, ok := s.sent[eventID]
	return ok
}

func (s *calendarBriefingService) markSent(eventID string, eventStart time.Time) {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	// Store eventStart (not wall-clock send time) so prune can decide
	// purely from the calendar event timeline.
	s.sent[eventID] = eventStart
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
