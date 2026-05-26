// calendar.go — miniapp.calendar.* RPC handlers.
//
//   miniapp.calendar.list_upcoming  — events in [now, now+hoursAhead)
//   miniapp.calendar.get            — single event detail by ID
//
// Same pattern as gmail.go: lazy CalendarClient factory wired in
// method_registry.go so the gateway boots even when calendar OAuth is
// not yet configured (per-call UNAVAILABLE instead of startup failure).

package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// CalendarClient is the subset of *calendar.Client the handlers use.
// Interface-based so tests can substitute fakes.
type CalendarClient interface {
	ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error)
	Get(ctx context.Context, eventID string) (*calendar.Event, error)
}

// CalendarDeps wraps the lazy client factory.
type CalendarDeps struct {
	Client func() (CalendarClient, error)
}

const (
	defaultUpcomingHours = 48
	maxUpcomingHours     = 24 * 14 // 2 weeks
	defaultUpcomingLimit = 50
	maxUpcomingLimit     = 250
)

// Wire shapes (package-scoped so both handlers can return them through
// the helper without anonymous-struct type-identity issues).

type calendarAttendeeOut struct {
	Email          string `json:"email,omitempty"`
	DisplayName    string `json:"displayName,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	Self           bool   `json:"self,omitempty"`
	Organizer      bool   `json:"organizer,omitempty"`
}

type calendarConferenceOut struct {
	Solution string `json:"solution,omitempty"`
	URI      string `json:"uri,omitempty"`
}

type calendarEventOut struct {
	ID          string                 `json:"id"`
	Summary     string                 `json:"summary"`
	Description string                 `json:"description,omitempty"`
	Location    string                 `json:"location,omitempty"`
	Start       string                 `json:"start"`
	End         string                 `json:"end"`
	AllDay      bool                   `json:"allDay,omitempty"`
	Status      string                 `json:"status,omitempty"`
	HTMLLink    string                 `json:"htmlLink,omitempty"`
	Organizer   *calendarAttendeeOut   `json:"organizer,omitempty"`
	Attendees   []calendarAttendeeOut  `json:"attendees,omitempty"`
	Conference  *calendarConferenceOut `json:"conference,omitempty"`
	HasMeet     bool                   `json:"hasMeet,omitempty"`
}

// CalendarMethods returns the miniapp.calendar.* handler map. Nil deps
// (no client factory) → nil map, and method_registry.go skips wiring.
func CalendarMethods(deps CalendarDeps) map[string]rpcutil.HandlerFunc {
	if deps.Client == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.calendar.list_upcoming": calendarListUpcoming(deps),
		"miniapp.calendar.get":           calendarGet(deps),
	}
}

func calendarClientOrErr(deps CalendarDeps, reqID string) (CalendarClient, *protocol.ResponseFrame) {
	client, err := deps.Client()
	if err != nil {
		return nil, rpcerr.WrapUnavailable("calendar client unavailable", err).Response(reqID)
	}
	return client, nil
}

// --- list_upcoming -------------------------------------------------------

func calendarListUpcoming(deps CalendarDeps) rpcutil.HandlerFunc {
	type params struct {
		HoursAhead int `json:"hoursAhead,omitempty"`
		Limit      int `json:"limit,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		hours := p.HoursAhead
		if hours <= 0 {
			hours = defaultUpcomingHours
		}
		if hours > maxUpcomingHours {
			hours = maxUpcomingHours
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultUpcomingLimit
		}
		if limit > maxUpcomingLimit {
			limit = maxUpcomingLimit
		}

		client, errResp := calendarClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		now := time.Now()
		events, err := client.ListUpcoming(ctx, now, now.Add(time.Duration(hours)*time.Hour), limit)
		if err != nil {
			return mapCalendarError(req.ID, "calendar list failed", err)
		}

		out := make([]calendarEventOut, 0, len(events))
		for _, e := range events {
			out = append(out, projectEventOut(e, false))
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"events": out})
	}
}

// --- get -----------------------------------------------------------------

func calendarGet(deps CalendarDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		client, errResp := calendarClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		ev, err := client.Get(ctx, p.ID)
		if err != nil {
			return mapCalendarError(req.ID, "calendar get failed", err)
		}
		if ev == nil {
			return rpcerr.NotFound("event " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, projectEventOut(*ev, true))
	}
}

// --- helpers --------------------------------------------------------------

// projectEventOut maps a domain Event to the wire shape. List view
// gets HasMeet (a boolean badge) but no full conference object; detail
// gets the conference object instead. Detail intentionally omits
// HasMeet so clients have exactly one signal — the conference field —
// and cannot drift between two booleans that should always agree.
func projectEventOut(e calendar.Event, includeDetail bool) calendarEventOut {
	row := calendarEventOut{
		ID:       e.ID,
		Summary:  e.Summary,
		Location: e.Location,
		Start:    formatTime(e.Start),
		End:      formatTime(e.End),
		AllDay:   e.AllDay,
		Status:   e.Status,
		HTMLLink: e.HTMLLink,
	}
	if includeDetail {
		row.Description = e.Description
		if e.Conference != nil {
			row.Conference = &calendarConferenceOut{
				Solution: e.Conference.Solution,
				URI:      e.Conference.URI,
			}
		}
	} else {
		row.HasMeet = e.Conference != nil && e.Conference.URI != ""
	}
	if e.Organizer.Email != "" || e.Organizer.DisplayName != "" {
		a := projectAttendeeOut(e.Organizer)
		row.Organizer = &a
	}
	for _, att := range e.Attendees {
		row.Attendees = append(row.Attendees, projectAttendeeOut(att))
	}
	return row
}

func projectAttendeeOut(a calendar.Attendee) calendarAttendeeOut {
	return calendarAttendeeOut{
		Email:          a.Email,
		DisplayName:    a.DisplayName,
		ResponseStatus: a.ResponseStatus,
		Self:           a.Self,
		Organizer:      a.Organizer,
	}
}

// formatTime returns RFC3339 or empty string for zero time. We surface
// zero times as "" to keep the JSON shape predictable for the client.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// mapCalendarError classifies a Calendar client error via the typed
// *calendar.APIError so a 500 response whose body happens to contain
// "not found" never collapses into NOT_FOUND.
func mapCalendarError(reqID, msg string, err error) *protocol.ResponseFrame {
	if err == nil {
		return rpcerr.Unavailable(msg).Response(reqID)
	}
	if errors.Is(err, errCalendarNotFound) {
		return rpcerr.NotFound(msg).Response(reqID)
	}
	var apiErr *calendar.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusNotFound:
			return rpcerr.NotFound(msg).Response(reqID)
		case http.StatusForbidden:
			return rpcerr.New(protocol.ErrForbidden, msg+": "+apiErr.Error()).Response(reqID)
		}
	}
	return rpcerr.WrapUnavailable(msg, err).Response(reqID)
}

// errCalendarNotFound is a sentinel callers can wrap; primarily kept
// for tests that don't want to construct a full *calendar.APIError.
var errCalendarNotFound = errors.New("calendar: event not found")
