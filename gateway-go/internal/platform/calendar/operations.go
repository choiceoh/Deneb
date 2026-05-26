package calendar

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Google Calendar v3 JSON shapes (internal — only the fields we project
// into the domain Event type).

type apiEventList struct {
	Items         []apiEvent `json:"items"`
	NextPageToken string     `json:"nextPageToken"`
}

type apiEvent struct {
	ID          string             `json:"id"`
	Summary     string             `json:"summary"`
	Description string             `json:"description"`
	Location    string             `json:"location"`
	Status      string             `json:"status"`
	HTMLLink    string             `json:"htmlLink"`
	Start       apiEventDateTime   `json:"start"`
	End         apiEventDateTime   `json:"end"`
	Organizer   *apiEventAttendee  `json:"organizer"`
	Attendees   []apiEventAttendee `json:"attendees"`
	Conference  *apiConferenceData `json:"conferenceData"`
}

type apiEventDateTime struct {
	DateTime string `json:"dateTime"` // RFC3339 — present for timed events
	Date     string `json:"date"`     // YYYY-MM-DD — present for all-day events
	TimeZone string `json:"timeZone"`
}

type apiEventAttendee struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName"`
	ResponseStatus string `json:"responseStatus"`
	Self           bool   `json:"self"`
	Organizer      bool   `json:"organizer"`
}

type apiConferenceData struct {
	ConferenceSolution *struct {
		Key *struct {
			Type string `json:"type"`
		} `json:"key"`
	} `json:"conferenceSolution"`
	EntryPoints []struct {
		EntryPointType string `json:"entryPointType"`
		URI            string `json:"uri"`
	} `json:"entryPoints"`
}

// ListUpcoming returns events from the primary calendar starting between
// `from` and `to`, sorted by start time. `singleEvents=true` expands
// recurring events into individual instances so the Mini App doesn't
// have to do RRULE math.
func (c *Client) ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]Event, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 250 {
		maxResults = 250
	}
	params := url.Values{
		"timeMin":      {from.UTC().Format(time.RFC3339)},
		"timeMax":      {to.UTC().Format(time.RFC3339)},
		"singleEvents": {"true"},
		"orderBy":      {"startTime"},
		"maxResults":   {fmt.Sprintf("%d", maxResults)},
	}
	path := "/calendars/primary/events?" + params.Encode()

	var list apiEventList
	if err := c.readJSON(ctx, path, &list); err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(list.Items))
	for _, item := range list.Items {
		if item.Status == "cancelled" {
			continue
		}
		out = append(out, projectEvent(item))
	}
	return out, nil
}

// Get returns a single event by ID from the primary calendar. Returns
// (nil, nil) when Google responds with HTTP 404 so callers can map the
// missing-event case to NOT_FOUND without inspecting error text.
func (c *Client) Get(ctx context.Context, eventID string) (*Event, error) {
	if strings.TrimSpace(eventID) == "" {
		return nil, fmt.Errorf("event ID is required")
	}
	// url.PathEscape doesn't escape '/' (PathEscape is for path
	// SEGMENTS, but '/' is the segment separator and stays literal).
	// Imported iCal UIDs occasionally contain '/', so escape it
	// manually or the request lands on the wrong Calendar resource.
	encoded := strings.ReplaceAll(url.PathEscape(eventID), "/", "%2F")
	path := "/calendars/primary/events/" + encoded
	var item apiEvent
	if err := c.readJSON(ctx, path, &item); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	if item.ID == "" {
		return nil, nil
	}
	ev := projectEvent(item)
	return &ev, nil
}

// projectEvent reduces the Google response to our domain shape.
func projectEvent(item apiEvent) Event {
	ev := Event{
		ID:          item.ID,
		Summary:     item.Summary,
		Description: item.Description,
		Location:    item.Location,
		Status:      item.Status,
		HTMLLink:    item.HTMLLink,
	}
	ev.Start, ev.AllDay = parseEventTime(item.Start)
	ev.End, _ = parseEventTime(item.End)

	if item.Organizer != nil {
		ev.Organizer = projectAttendee(*item.Organizer)
	}
	for _, a := range item.Attendees {
		ev.Attendees = append(ev.Attendees, projectAttendee(a))
	}
	if item.Conference != nil {
		ci := projectConference(*item.Conference)
		if ci != nil {
			ev.Conference = ci
		}
	}
	return ev
}

func projectAttendee(a apiEventAttendee) Attendee {
	return Attendee{
		Email:          strings.ToLower(strings.TrimSpace(a.Email)),
		DisplayName:    strings.TrimSpace(a.DisplayName),
		ResponseStatus: a.ResponseStatus,
		Self:           a.Self,
		Organizer:      a.Organizer,
	}
}

// projectConference picks the first video entryPoint. Returns nil when
// the event has conferenceData but no usable video URL (e.g. dial-in
// only), so the UI doesn't render a broken "join" button.
func projectConference(c apiConferenceData) *ConferenceInfo {
	info := &ConferenceInfo{}
	if c.ConferenceSolution != nil && c.ConferenceSolution.Key != nil {
		info.Solution = c.ConferenceSolution.Key.Type
	}
	for _, ep := range c.EntryPoints {
		if ep.EntryPointType == "video" && ep.URI != "" {
			info.URI = ep.URI
			break
		}
	}
	if info.URI == "" {
		return nil
	}
	return info
}

// parseEventTime maps Google's union of (dateTime|date) into time.Time
// plus an all-day flag. dateTime wins when both present.
//
// dateTime parsing has two paths:
//  1. RFC3339 (the common case — Google always sends offset for events
//     with a non-default tz, e.g. "2026-05-26T14:00:00+09:00").
//  2. Wall-clock + timeZone (some Google clients send dateTime without
//     a numeric offset when timeZone is set, e.g. "2026-05-26T14:00:00"
//     with timeZone "Asia/Seoul"). Without this fallback those events
//     fall to zero time and are silently dropped from list and
//     briefing pipelines.
func parseEventTime(dt apiEventDateTime) (parsed time.Time, allDay bool) {
	if dt.DateTime != "" {
		if t, err := time.Parse(time.RFC3339, dt.DateTime); err == nil {
			return t, false
		}
		// Fallback: wall-clock in declared timeZone (no offset).
		if dt.TimeZone != "" {
			if loc, err := time.LoadLocation(dt.TimeZone); err == nil {
				if t, err := time.ParseInLocation("2006-01-02T15:04:05", dt.DateTime, loc); err == nil {
					return t, false
				}
			}
		}
	}
	if dt.Date != "" {
		// All-day events arrive as YYYY-MM-DD. Anchor in the event's
		// declared timezone so display logic later renders the right
		// local day; fall back to UTC when no tz given.
		loc := time.UTC
		if dt.TimeZone != "" {
			if l, err := time.LoadLocation(dt.TimeZone); err == nil {
				loc = l
			}
		}
		t, err := time.ParseInLocation("2006-01-02", dt.Date, loc)
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
