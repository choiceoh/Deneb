// heartbeat_signals.go — wires the proactive signal engine
// (internal/agentsys/autonomous/signal.go) to a live data source for the
// heartbeat task.
//
// Research basis: docs/research/claw-anything-always-on-assistant.md, finding B
// (proactive ~4x harder than reactive; Deneb's heartbeat fired on a pure timer
// with no notion of "is anything noteworthy?"). The signal engine is the pure,
// tested scorer; this file is the thin, best-effort adapter that fetches the
// snapshot it scores.
//
// Current source: Google Calendar (conflicts + imminent events + RSVP-pending
// meetings). It reuses the same lazy client resolver as the calendar briefing
// service, so it degrades gracefully when OAuth is not configured (returns an
// empty snapshot → no signals → heartbeat behaves exactly as before). Mail/VIP
// and deadline collectors are deliberately deferred (they need wiki-VIP lookups
// and a due-item store) and can be appended to the same SignalInputs later.

package server

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// signalCalendarLookahead bounds how far ahead the collector asks Calendar for
// events. Conflicts and imminent-start detection only need the near horizon; a
// day comfortably covers the default imminent (30m) and gives conflict pairs
// room without pulling the whole calendar.
const (
	signalCalendarLookahead = 24 * time.Hour
	signalCalendarMaxEvents = 50
)

// newCalendarSignalCollector returns a collector that fetches upcoming events
// via the lazy calendar resolver and maps them into autonomous.SignalInputs.
//
// It is best-effort by contract: any resolver/API error yields an empty
// snapshot (Now set, no events) so DetectSignals finds nothing and the
// heartbeat turn is unaffected. Returns nil when resolve is nil so callers can
// wire it unconditionally.
func newCalendarSignalCollector(resolve func() (briefingCalendarClient, error)) func(ctx context.Context) autonomous.SignalInputs {
	if resolve == nil {
		return nil
	}
	return func(ctx context.Context) autonomous.SignalInputs {
		now := time.Now()
		in := autonomous.SignalInputs{Now: now}

		client, err := resolve()
		if err != nil || client == nil {
			return in // OAuth not configured / transient — no signals.
		}
		events, err := client.ListUpcoming(ctx, now, now.Add(signalCalendarLookahead), signalCalendarMaxEvents)
		if err != nil {
			return in
		}
		in.Events = make([]autonomous.EventSignalInput, 0, len(events))
		for _, e := range events {
			in.Events = append(in.Events, toEventSignalInput(e))
		}
		return in
	}
}

// toEventSignalInput projects a calendar.Event onto the engine's plain input.
func toEventSignalInput(e calendar.Event) autonomous.EventSignalInput {
	return autonomous.EventSignalInput{
		ID:            e.ID,
		Summary:       e.Summary,
		Start:         e.Start,
		End:           e.End,
		AllDay:        e.AllDay,
		Canceled:      strings.EqualFold(e.Status, "cancelled"),
		NeedsResponse: selfNeedsResponse(e),
	}
}

// selfNeedsResponse reports whether the authenticated user is an attendee who
// has not yet RSVP'd (responseStatus == "needsAction").
func selfNeedsResponse(e calendar.Event) bool {
	for _, a := range e.Attendees {
		if a.Self && strings.EqualFold(a.ResponseStatus, "needsAction") {
			return true
		}
	}
	return false
}
