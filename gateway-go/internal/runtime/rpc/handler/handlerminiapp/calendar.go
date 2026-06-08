// calendar.go — miniapp.calendar.* RPC handlers.
//
//   miniapp.calendar.list_upcoming  — events in [now, now+hoursAhead)
//   miniapp.calendar.list_range     — events in an explicit [from, to) window
//   miniapp.calendar.get            — single event detail by ID
//   miniapp.calendar.create         — add a local event
//   miniapp.calendar.update         — edit a local event
//   miniapp.calendar.delete         — remove a local event
//
// The calendar is hybrid: the read-only Google client (lazy factory, like
// gmail.go) provides external events, and a local store (localcal) holds events
// the user adds by hand. Reads merge both; writes always land in the local store,
// so adding/editing/deleting works without a Google OAuth write scope. Local
// events carry a "local:" ID prefix so get/update/delete route to the store.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// CalendarClient is the subset of *calendar.Client the handlers use (read-only;
// Google writes need a scope we don't require). Interface-based for test fakes.
type CalendarClient interface {
	ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error)
	Get(ctx context.Context, eventID string) (*calendar.Event, error)
}

// LocalCalendar is the subset of *localcal.Store the handlers use — the
// read/write side of the hybrid calendar. Interface-based for test fakes.
type LocalCalendar interface {
	ListRange(from, to time.Time) []calendar.Event
	Get(id string) *calendar.Event
	Create(in localcal.CreateInput) (calendar.Event, error)
	Update(id string, in localcal.CreateInput) (*calendar.Event, error)
	Delete(id string) error
}

// CalendarDeps wraps the lazy Google client factory and the local store.
// Either may be nil; handlers degrade (Google-only, local-only, or UNAVAILABLE).
type CalendarDeps struct {
	Client func() (CalendarClient, error)
	Local  LocalCalendar
}

const (
	defaultUpcomingHours = 48
	maxUpcomingHours     = 24 * 14 // 2 weeks
	defaultUpcomingLimit = 50
	maxUpcomingLimit     = 250
	maxRangeDays         = 45 // list_range span cap (one month grid + slack)
)

// Wire shapes (package-scoped so handlers can return them through the helper
// without anonymous-struct type-identity issues).

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

// calendarEventOut is the wire shape for a calendar event, returned by the list
// and get handlers. Marked for Kotlin codegen so the native client shares this
// exact shape instead of hand-maintaining a partial mirror that can drift.
//
// Local field is true for events stored in the local store (vs read-only Google),
// so the native client can show edit/delete only where they actually work.
//
//deneb:wire
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
	Local       bool                   `json:"local,omitempty"`
	Organizer   *calendarAttendeeOut   `json:"organizer,omitempty"`
	Attendees   []calendarAttendeeOut  `json:"attendees,omitempty"`
	Conference  *calendarConferenceOut `json:"conference,omitempty"`
	HasMeet     bool                   `json:"hasMeet,omitempty"`
}

// CalendarMethods returns the miniapp.calendar.* handler map. With neither a
// Google client nor a local store, returns nil and method_registry.go skips it.
func CalendarMethods(deps CalendarDeps) map[string]rpcutil.HandlerFunc {
	if deps.Client == nil && deps.Local == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.calendar.list_upcoming": calendarListUpcoming(deps),
		"miniapp.calendar.list_range":    calendarListRange(deps),
		"miniapp.calendar.get":           calendarGet(deps),
		"miniapp.calendar.create":        calendarCreate(deps),
		"miniapp.calendar.update":        calendarUpdate(deps),
		"miniapp.calendar.delete":        calendarDelete(deps),
	}
}

// listMerged returns Google events in [from, to] (when the Google client is
// configured and healthy) merged with local events, sorted by start. A Google
// factory/list error is returned ONLY when there's no local store to fall back
// on; otherwise local events still answer the call so the calendar keeps working
// even if Google is unconfigured. A live Google error (with a client present)
// propagates, preserving the read contract the list tests encode.
func listMerged(ctx context.Context, deps CalendarDeps, reqID string, from, to time.Time, limit int) ([]calendar.Event, *protocol.ResponseFrame) {
	var merged []calendar.Event
	if deps.Client != nil {
		client, err := deps.Client()
		if err != nil {
			if deps.Local == nil {
				return nil, rpcerr.WrapUnavailable("calendar client unavailable", err).Response(reqID)
			}
			// Google not configured but local works — degrade to local-only.
		} else {
			events, err := client.ListUpcoming(ctx, from, to, limit)
			if err != nil {
				return nil, mapCalendarError(reqID, "calendar list failed", err)
			}
			merged = append(merged, events...)
		}
	}
	if deps.Local != nil {
		merged = append(merged, deps.Local.ListRange(from, to)...)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Start.Before(merged[j].Start) })
	return merged, nil
}

func respondEvents(reqID string, events []calendar.Event) *protocol.ResponseFrame {
	out := make([]calendarEventOut, 0, len(events))
	for _, e := range events {
		out = append(out, projectEventOut(e, false))
	}
	return rpcutil.RespondOK(reqID, map[string]any{"events": out})
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

		now := time.Now()
		events, errResp := listMerged(ctx, deps, req.ID, now, now.Add(time.Duration(hours)*time.Hour), limit)
		if errResp != nil {
			return errResp
		}
		return respondEvents(req.ID, events)
	}
}

// --- list_range ----------------------------------------------------------

// calendarListRange returns events in an explicit [from, to) window — used by
// the native month grid, which needs a whole month (often spanning the past)
// rather than list_upcoming's now-anchored look-ahead. Same envelope and
// element shape as list_upcoming so the client reuses one decoder.
func calendarListRange(deps CalendarDeps) rpcutil.HandlerFunc {
	type params struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		from, err := time.Parse(time.RFC3339, strings.TrimSpace(p.From))
		if err != nil {
			return rpcerr.InvalidParams(fmt.Errorf("from must be RFC3339: %w", err)).Response(req.ID)
		}
		to, err := time.Parse(time.RFC3339, strings.TrimSpace(p.To))
		if err != nil {
			return rpcerr.InvalidParams(fmt.Errorf("to must be RFC3339: %w", err)).Response(req.ID)
		}
		if !to.After(from) {
			return rpcerr.InvalidParams(fmt.Errorf("to must be after from")).Response(req.ID)
		}
		// Clamp an over-wide window so one screen can't pull a year of events.
		if to.Sub(from) > maxRangeDays*24*time.Hour {
			to = from.Add(maxRangeDays * 24 * time.Hour)
		}

		events, errResp := listMerged(ctx, deps, req.ID, from, to, maxUpcomingLimit)
		if errResp != nil {
			return errResp
		}
		return respondEvents(req.ID, events)
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

		// Local events route to the store; everything else is a Google ID.
		if localcal.IsLocalID(p.ID) {
			if deps.Local == nil {
				return rpcerr.NotFound("event " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
			}
			ev := deps.Local.Get(p.ID)
			if ev == nil {
				return rpcerr.NotFound("event " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
			}
			return rpcutil.RespondOK(req.ID, projectEventOut(*ev, true))
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

// --- create / update -----------------------------------------------------

// eventInput is the shared params shape for create/update. start/end are RFC3339
// (end optional — the store applies a default duration). timeZone is accepted for
// forward-compat but the local store derives the day/instant from start's offset.
type eventInput struct {
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	Start       string `json:"start"`
	End         string `json:"end,omitempty"`
	AllDay      bool   `json:"allDay,omitempty"`
	TimeZone    string `json:"timeZone,omitempty"`
}

// parseEventInput validates required fields and parses start/end into a
// localcal.CreateInput, returning an error response frame on bad input.
func parseEventInput(reqID string, p eventInput) (localcal.CreateInput, *protocol.ResponseFrame) {
	if strings.TrimSpace(p.Summary) == "" {
		return localcal.CreateInput{}, rpcerr.MissingParam("summary").Response(reqID)
	}
	if strings.TrimSpace(p.Start) == "" {
		return localcal.CreateInput{}, rpcerr.MissingParam("start").Response(reqID)
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(p.Start))
	if err != nil {
		return localcal.CreateInput{}, rpcerr.InvalidParams(fmt.Errorf("start must be RFC3339: %w", err)).Response(reqID)
	}
	var end time.Time
	if s := strings.TrimSpace(p.End); s != "" {
		end, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return localcal.CreateInput{}, rpcerr.InvalidParams(fmt.Errorf("end must be RFC3339: %w", err)).Response(reqID)
		}
	}
	return localcal.CreateInput{
		Summary:     p.Summary,
		Description: p.Description,
		Location:    p.Location,
		Start:       start,
		End:         end,
		AllDay:      p.AllDay,
	}, nil
}

func calendarCreate(deps CalendarDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[eventInput](req)
		if errResp != nil {
			return errResp
		}
		in, errResp := parseEventInput(req.ID, p)
		if errResp != nil {
			return errResp
		}
		if deps.Local == nil {
			return rpcerr.Unavailable("local calendar unavailable").Response(req.ID)
		}
		ev, err := deps.Local.Create(in)
		if err != nil {
			return rpcerr.WrapUnavailable("calendar create failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, projectEventOut(ev, true))
	}
}

func calendarUpdate(deps CalendarDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
		eventInput
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
		if !localcal.IsLocalID(p.ID) {
			return rpcerr.New(protocol.ErrForbidden, "외부 캘린더(구글) 일정은 이 앱에서 수정할 수 없습니다.").Response(req.ID)
		}
		in, errResp := parseEventInput(req.ID, p.eventInput)
		if errResp != nil {
			return errResp
		}
		if deps.Local == nil {
			return rpcerr.Unavailable("local calendar unavailable").Response(req.ID)
		}
		ev, err := deps.Local.Update(p.ID, in)
		if err != nil {
			if errors.Is(err, localcal.ErrNotFound) {
				return rpcerr.NotFound("event " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("calendar update failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, projectEventOut(*ev, true))
	}
}

// --- delete --------------------------------------------------------------

func calendarDelete(deps CalendarDeps) rpcutil.HandlerFunc {
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
		if !localcal.IsLocalID(p.ID) {
			return rpcerr.New(protocol.ErrForbidden, "외부 캘린더(구글) 일정은 이 앱에서 삭제할 수 없습니다.").Response(req.ID)
		}
		if deps.Local == nil {
			return rpcerr.Unavailable("local calendar unavailable").Response(req.ID)
		}
		if err := deps.Local.Delete(p.ID); err != nil {
			if errors.Is(err, localcal.ErrNotFound) {
				return rpcerr.NotFound("event " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("calendar delete failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true})
	}
}

// --- helpers --------------------------------------------------------------

func calendarClientOrErr(deps CalendarDeps, reqID string) (CalendarClient, *protocol.ResponseFrame) {
	if deps.Client == nil {
		return nil, rpcerr.Unavailable("calendar client unavailable").Response(reqID)
	}
	client, err := deps.Client()
	if err != nil {
		return nil, rpcerr.WrapUnavailable("calendar client unavailable", err).Response(reqID)
	}
	return client, nil
}

// projectEventOut maps a domain Event to the wire shape. List view gets HasMeet
// (a boolean badge) but no full conference object; detail gets the conference
// object instead. Detail intentionally omits HasMeet so clients have exactly one
// signal — the conference field — and cannot drift between two booleans that
// should always agree. Local is derived from the ID prefix so the client can
// gate edit/delete affordances on it.
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
		Local:    localcal.IsLocalID(e.ID),
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

// formatTime returns RFC3339 or empty string for zero time. We surface zero
// times as "" to keep the JSON shape predictable for the client.
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

// errCalendarNotFound is a sentinel callers can wrap; primarily kept for tests
// that don't want to construct a full *calendar.APIError.
var errCalendarNotFound = errors.New("calendar: event not found")
