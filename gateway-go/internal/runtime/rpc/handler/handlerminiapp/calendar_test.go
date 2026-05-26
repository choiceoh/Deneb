package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeCalendarClient struct {
	listFn func(ctx context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error)
	getFn  func(ctx context.Context, id string) (*calendar.Event, error)
}

func (f *fakeCalendarClient) ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error) {
	if f.listFn == nil {
		return nil, errors.New("ListUpcoming not stubbed")
	}
	return f.listFn(ctx, from, to, maxResults)
}

func (f *fakeCalendarClient) Get(ctx context.Context, id string) (*calendar.Event, error) {
	if f.getFn == nil {
		return nil, errors.New("Get not stubbed")
	}
	return f.getFn(ctx, id)
}

func calDepsFor(client CalendarClient) CalendarDeps {
	return CalendarDeps{Client: func() (CalendarClient, error) { return client, nil }}
}

func TestCalendarListUpcoming_ShapeAndDefaults(t *testing.T) {
	var seenFrom, seenTo time.Time
	var seenMax int
	client := &fakeCalendarClient{
		listFn: func(_ context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error) {
			seenFrom, seenTo, seenMax = from, to, maxResults
			return []calendar.Event{
				{
					ID:      "e1",
					Summary: "부산8 협의",
					Start:   time.Date(2026, 5, 26, 14, 0, 0, 0, time.UTC),
					End:     time.Date(2026, 5, 26, 15, 0, 0, 0, time.UTC),
					Status:  "confirmed",
					Attendees: []calendar.Attendee{
						{Email: "park@example.com", DisplayName: "박YY", ResponseStatus: "accepted"},
					},
					Conference: &calendar.ConferenceInfo{URI: "https://meet.google.com/x"},
				},
			}, nil
		},
	}
	h := calendarListUpcoming(calDepsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.list_upcoming", nil))

	var out struct {
		Events []calendarEventOut `json:"events"`
	}
	decode(t, resp, &out)
	if len(out.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(out.Events))
	}
	got := out.Events[0]
	if got.ID != "e1" || got.Summary != "부산8 협의" {
		t.Errorf("event = %+v", got)
	}
	if got.Start != "2026-05-26T14:00:00Z" {
		t.Errorf("start = %q", got.Start)
	}
	if !got.HasMeet {
		t.Error("hasMeet expected true when Conference.URI is set")
	}
	// Detail-only fields must not leak into the list shape.
	if got.Description != "" {
		t.Errorf("description leaked into list shape: %q", got.Description)
	}
	if got.Conference != nil {
		t.Errorf("conference object leaked into list shape: %+v", got.Conference)
	}
	if len(got.Attendees) != 1 || got.Attendees[0].DisplayName != "박YY" {
		t.Errorf("attendees = %+v", got.Attendees)
	}

	// Defaults: 48h ahead, 50 max.
	if seenMax != 50 {
		t.Errorf("limit = %d, want default 50", seenMax)
	}
	delta := seenTo.Sub(seenFrom).Hours()
	if delta < 47.9 || delta > 48.1 {
		t.Errorf("hoursAhead window = %v, want ~48h", delta)
	}
}

func TestCalendarListUpcoming_HonorsParams(t *testing.T) {
	var seenMax int
	var seenWindow float64
	client := &fakeCalendarClient{
		listFn: func(_ context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error) {
			seenMax = maxResults
			seenWindow = to.Sub(from).Hours()
			return nil, nil
		},
	}
	h := calendarListUpcoming(calDepsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.list_upcoming",
		map[string]any{"hoursAhead": 6, "limit": 10}))
	if !resp.OK {
		t.Fatalf("not ok: %+v", resp.Error)
	}
	if seenMax != 10 {
		t.Errorf("limit = %d", seenMax)
	}
	if seenWindow < 5.9 || seenWindow > 6.1 {
		t.Errorf("window = %v, want ~6h", seenWindow)
	}
}

func TestCalendarListUpcoming_ClampsExcessivelyLargeRequests(t *testing.T) {
	var seenMax int
	var seenWindow float64
	client := &fakeCalendarClient{
		listFn: func(_ context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error) {
			seenMax = maxResults
			seenWindow = to.Sub(from).Hours()
			return nil, nil
		},
	}
	h := calendarListUpcoming(calDepsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.list_upcoming",
		map[string]any{"hoursAhead": 100000, "limit": 100000}))
	if !resp.OK {
		t.Fatalf("not ok: %+v", resp.Error)
	}
	if seenMax > maxUpcomingLimit {
		t.Errorf("max not clamped: %d", seenMax)
	}
	if seenWindow > float64(maxUpcomingHours)+0.1 {
		t.Errorf("window not clamped: %v", seenWindow)
	}
}

func TestCalendarListUpcoming_PropagatesError(t *testing.T) {
	client := &fakeCalendarClient{
		listFn: func(context.Context, time.Time, time.Time, int) ([]calendar.Event, error) {
			return nil, &calendar.APIError{StatusCode: 403, Body: "forbidden"}
		},
	}
	h := calendarListUpcoming(calDepsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.list_upcoming", nil))
	if resp.OK {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != protocol.ErrForbidden {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrForbidden)
	}
}

// Regression: a non-404 response whose body contains the substring
// "not found" must NOT be mapped to NOT_FOUND. The old substring-match
// classifier would have misrouted this; the typed APIError path keeps
// it as UNAVAILABLE.
func TestCalendarListUpcoming_500WithMisleadingBodyStaysUnavailable(t *testing.T) {
	client := &fakeCalendarClient{
		listFn: func(context.Context, time.Time, time.Time, int) ([]calendar.Event, error) {
			return nil, &calendar.APIError{
				StatusCode: 500,
				Body:       "internal error: no calendar entry found in cache, retrying",
			}
		},
	}
	h := calendarListUpcoming(calDepsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.list_upcoming", nil))
	if resp.OK {
		t.Fatal("expected error response")
	}
	if resp.Error.Code == protocol.ErrNotFound {
		t.Errorf("500 body containing 'not found' must not map to NOT_FOUND, got %s", resp.Error.Code)
	}
}

func TestCalendarGet_404MapsToNotFound(t *testing.T) {
	client := &fakeCalendarClient{
		getFn: func(context.Context, string) (*calendar.Event, error) {
			return nil, &calendar.APIError{StatusCode: 404, Body: "not found"}
		},
	}
	h := calendarGet(calDepsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.get", map[string]string{"id": "missing"}))
	if resp.OK {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("404 should map to NOT_FOUND, got %s", resp.Error.Code)
	}
}

func TestCalendarGet_HappyPath(t *testing.T) {
	client := &fakeCalendarClient{
		getFn: func(_ context.Context, id string) (*calendar.Event, error) {
			if id != "e1" {
				t.Errorf("id = %q", id)
			}
			return &calendar.Event{
				ID:          "e1",
				Summary:     "회의",
				Description: "상세 내용",
				Status:      "confirmed",
				Start:       time.Date(2026, 5, 26, 14, 0, 0, 0, time.UTC),
				Conference:  &calendar.ConferenceInfo{Solution: "hangoutsMeet", URI: "https://meet.google.com/x"},
			}, nil
		},
	}
	h := calendarGet(calDepsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.get", map[string]string{"id": "e1"}))

	var got calendarEventOut
	decode(t, resp, &got)
	if got.ID != "e1" || got.Description != "상세 내용" {
		t.Errorf("event = %+v", got)
	}
	if got.HasMeet {
		t.Errorf("HasMeet should be omitted on detail (use Conference instead): %+v", got)
	}
	if got.Conference == nil || got.Conference.URI != "https://meet.google.com/x" {
		t.Errorf("conference object missing on detail: %+v", got.Conference)
	}
}

func TestCalendarGet_NotFound(t *testing.T) {
	client := &fakeCalendarClient{
		getFn: func(context.Context, string) (*calendar.Event, error) {
			return nil, nil
		},
	}
	h := calendarGet(calDepsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.get", map[string]string{"id": "missing"}))
	if resp.OK {
		t.Fatal("expected error response for missing event")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrNotFound)
	}
}

func TestCalendarGet_MissingIDRejected(t *testing.T) {
	h := calendarGet(calDepsFor(&fakeCalendarClient{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.get", map[string]string{"id": "  "}))
	if resp.OK {
		t.Fatal("expected validation error for blank id")
	}
}

func TestCalendarMethods_NilDeps(t *testing.T) {
	if got := CalendarMethods(CalendarDeps{}); got != nil {
		t.Errorf("nil-deps factory should return nil map, got %v", got)
	}
}

// Smoke: payload always JSON-serializable, no time.Time fields leaking.
func TestCalendarListUpcoming_JSONSerializable(t *testing.T) {
	client := &fakeCalendarClient{
		listFn: func(context.Context, time.Time, time.Time, int) ([]calendar.Event, error) {
			return []calendar.Event{{ID: "e1", Summary: "x"}}, nil
		},
	}
	h := calendarListUpcoming(calDepsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.list_upcoming", nil))
	if _, err := json.Marshal(resp); err != nil {
		t.Errorf("response not JSON-serializable: %v", err)
	}
}
