package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
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

// calDepsWithLocal builds deps backed by a real localcal.Store in a temp file,
// optionally with a Google client too. Returns the store for direct assertions.
func calDepsWithLocal(t *testing.T, client CalendarClient) (CalendarDeps, *localcal.Store) {
	t.Helper()
	store, err := localcal.New(filepath.Join(t.TempDir(), "calendar.json"))
	if err != nil {
		t.Fatalf("localcal.New: %v", err)
	}
	deps := CalendarDeps{Local: store}
	if client != nil {
		deps.Client = func() (CalendarClient, error) { return client, nil }
	}
	return deps, store
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

// --- list_range ----------------------------------------------------------

func TestCalendarListRange_HappyPath(t *testing.T) {
	var seenFrom, seenTo time.Time
	client := &fakeCalendarClient{
		listFn: func(_ context.Context, from, to time.Time, _ int) ([]calendar.Event, error) {
			seenFrom, seenTo = from, to
			return []calendar.Event{{ID: "e1", Summary: "월간 회의"}}, nil
		},
	}
	h := calendarListRange(calDepsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.list_range", map[string]any{
		"from": "2026-06-01T00:00:00Z",
		"to":   "2026-06-30T23:59:59Z",
	}))
	var out struct {
		Events []calendarEventOut `json:"events"`
	}
	decode(t, resp, &out)
	if len(out.Events) != 1 || out.Events[0].ID != "e1" {
		t.Fatalf("events = %+v", out.Events)
	}
	if !seenFrom.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("from = %v", seenFrom)
	}
	if !seenTo.Equal(time.Date(2026, 6, 30, 23, 59, 59, 0, time.UTC)) {
		t.Errorf("to = %v", seenTo)
	}
}

func TestCalendarListRange_RejectsBadParams(t *testing.T) {
	h := calendarListRange(calDepsFor(&fakeCalendarClient{}))
	cases := []map[string]any{
		{"from": "nope", "to": "2026-06-30T00:00:00Z"},                 // bad from
		{"from": "2026-06-01T00:00:00Z", "to": "nope"},                 // bad to
		{"from": "2026-06-30T00:00:00Z", "to": "2026-06-01T00:00:00Z"}, // to <= from
	}
	for i, c := range cases {
		resp := h(authedCtx(), reqWith(t, "miniapp.calendar.list_range", c))
		if resp.OK {
			t.Errorf("case %d: expected validation error for %+v", i, c)
		}
	}
}

func TestCalendarListRange_ClampsWideWindow(t *testing.T) {
	var seenWindow time.Duration
	client := &fakeCalendarClient{
		listFn: func(_ context.Context, from, to time.Time, _ int) ([]calendar.Event, error) {
			seenWindow = to.Sub(from)
			return nil, nil
		},
	}
	h := calendarListRange(calDepsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.list_range", map[string]any{
		"from": "2026-01-01T00:00:00Z",
		"to":   "2026-12-31T00:00:00Z", // ~364 days, must clamp to maxRangeDays
	}))
	if !resp.OK {
		t.Fatalf("not ok: %+v", resp.Error)
	}
	if seenWindow > maxRangeDays*24*time.Hour {
		t.Errorf("window not clamped: %v", seenWindow)
	}
}

// --- list merge ----------------------------------------------------------

// list_range and list_upcoming must union Google + local events, sorted by start.
func TestCalendarListRange_MergesLocalEvents(t *testing.T) {
	client := &fakeCalendarClient{
		listFn: func(context.Context, time.Time, time.Time, int) ([]calendar.Event, error) {
			return []calendar.Event{
				{ID: "g1", Summary: "구글 일정", Start: time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)},
			}, nil
		},
	}
	deps, store := calDepsWithLocal(t, client)
	if _, err := store.Create(localcal.CreateInput{
		Summary: "로컬 일정", Start: time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := calendarListRange(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.list_range", map[string]any{
		"from": "2026-06-01T00:00:00Z",
		"to":   "2026-06-30T00:00:00Z",
	}))
	var out struct {
		Events []calendarEventOut `json:"events"`
	}
	decode(t, resp, &out)
	if len(out.Events) != 2 {
		t.Fatalf("events = %d, want 2 (1 google + 1 local)", len(out.Events))
	}
	// Sorted by start: local (6/10) before google (6/15).
	if !out.Events[0].Local || out.Events[0].Summary != "로컬 일정" {
		t.Errorf("first should be the local event, got %+v", out.Events[0])
	}
	if out.Events[1].Local {
		t.Errorf("second (google) must not be flagged local: %+v", out.Events[1])
	}
}

// With no Google client configured, the calendar still works from the local store.
func TestCalendarListRange_LocalOnlyWhenNoGoogle(t *testing.T) {
	deps, store := calDepsWithLocal(t, nil)
	if _, err := store.Create(localcal.CreateInput{
		Summary: "로컬", Start: time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := calendarListRange(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.list_range", map[string]any{
		"from": "2026-06-01T00:00:00Z",
		"to":   "2026-06-30T00:00:00Z",
	}))
	var out struct {
		Events []calendarEventOut `json:"events"`
	}
	decode(t, resp, &out)
	if len(out.Events) != 1 || !out.Events[0].Local {
		t.Fatalf("events = %+v", out.Events)
	}
}

// --- create / delete / update / get routing ------------------------------

func TestCalendarCreate_WritesLocalEvent(t *testing.T) {
	deps, store := calDepsWithLocal(t, nil)
	h := calendarCreate(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.create", map[string]any{
		"summary":  "신규 미팅",
		"location": "본사 3층",
		"start":    "2026-06-10T14:00:00+09:00",
		"end":      "2026-06-10T15:00:00+09:00",
		"timeZone": "Asia/Seoul",
	}))
	var ev calendarEventOut
	decode(t, resp, &ev)
	if ev.Summary != "신규 미팅" || ev.Location != "본사 3층" {
		t.Errorf("event = %+v", ev)
	}
	if !localcal.IsLocalID(ev.ID) || !ev.Local {
		t.Errorf("created event must be a local event: %+v", ev)
	}
	// Persisted to the store and findable in range.
	got := store.ListRange(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC))
	if len(got) != 1 {
		t.Fatalf("store has %d events, want 1", len(got))
	}
}

func TestCalendarCreate_RejectsMissingFields(t *testing.T) {
	deps, _ := calDepsWithLocal(t, nil)
	h := calendarCreate(deps)
	cases := []map[string]any{
		{"start": "2026-06-10T14:00:00Z"},        // missing summary
		{"summary": "x"},                         // missing start
		{"summary": "x", "start": "not-rfc3339"}, // bad start
	}
	for i, c := range cases {
		resp := h(authedCtx(), reqWith(t, "miniapp.calendar.create", c))
		if resp.OK {
			t.Errorf("case %d: expected validation error for %+v", i, c)
		}
	}
}

func TestCalendarCreate_UnavailableWithoutLocalStore(t *testing.T) {
	h := calendarCreate(calDepsFor(&fakeCalendarClient{})) // Google only, no local
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.create", map[string]any{
		"summary": "x", "start": "2026-06-10T14:00:00Z",
	}))
	if resp.OK {
		t.Fatal("expected UNAVAILABLE without a local store")
	}
}

func TestCalendarGet_RoutesLocalIDToStore(t *testing.T) {
	deps, store := calDepsWithLocal(t, nil)
	created, err := store.Create(localcal.CreateInput{
		Summary: "로컬", Start: time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := calendarGet(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.get", map[string]string{"id": created.ID}))
	var ev calendarEventOut
	decode(t, resp, &ev)
	if ev.ID != created.ID || !ev.Local {
		t.Errorf("event = %+v", ev)
	}
}

func TestCalendarDelete_RemovesLocalEvent(t *testing.T) {
	deps, store := calDepsWithLocal(t, nil)
	created, err := store.Create(localcal.CreateInput{
		Summary: "지울 일정", Start: time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := calendarDelete(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.delete", map[string]string{"id": created.ID}))
	if !resp.OK {
		t.Fatalf("delete not ok: %+v", resp.Error)
	}
	if store.Get(created.ID) != nil {
		t.Error("event still present after delete")
	}
}

func TestCalendarDelete_RejectsGoogleID(t *testing.T) {
	deps, _ := calDepsWithLocal(t, nil)
	h := calendarDelete(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.delete", map[string]string{"id": "google-event-123"}))
	if resp.OK {
		t.Fatal("expected error deleting a non-local event")
	}
	if resp.Error.Code != protocol.ErrForbidden {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrForbidden)
	}
}

func TestCalendarUpdate_EditsLocalEvent(t *testing.T) {
	deps, store := calDepsWithLocal(t, nil)
	created, err := store.Create(localcal.CreateInput{
		Summary: "원래 제목", Start: time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := calendarUpdate(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.update", map[string]any{
		"id":      created.ID,
		"summary": "수정된 제목",
		"start":   "2026-06-10T14:00:00+09:00",
	}))
	var ev calendarEventOut
	decode(t, resp, &ev)
	if ev.ID != created.ID || ev.Summary != "수정된 제목" {
		t.Errorf("event = %+v", ev)
	}
	if got := store.Get(created.ID); got == nil || got.Summary != "수정된 제목" {
		t.Errorf("store not updated: %+v", got)
	}
}

func TestCalendarUpdate_RejectsGoogleID(t *testing.T) {
	deps, _ := calDepsWithLocal(t, nil)
	h := calendarUpdate(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.calendar.update", map[string]any{
		"id": "google-event-123", "summary": "x", "start": "2026-06-10T14:00:00Z",
	}))
	if resp.OK || resp.Error.Code != protocol.ErrForbidden {
		t.Errorf("expected FORBIDDEN, got ok=%v err=%+v", resp.OK, resp.Error)
	}
}

// TestEventCategory pins the month-grid color buckets: deadline by Kind,
// others by a non-self organizer, mine for everything the user owns.
func TestEventCategory(t *testing.T) {
	cases := []struct {
		name string
		ev   calendar.Event
		want string
	}{
		{"deadline by kind", calendar.Event{Kind: "deadline"}, "deadline"},
		{"deadline beats a foreign organizer", calendar.Event{Kind: "deadline", Organizer: calendar.Attendee{Email: "boss@example.com"}}, "deadline"},
		{"others: non-self organizer (email)", calendar.Event{Organizer: calendar.Attendee{Email: "boss@example.com"}}, "others"},
		{"others: non-self organizer (display name only)", calendar.Event{Organizer: calendar.Attendee{DisplayName: "Boss"}}, "others"},
		{"mine: self organizer", calendar.Event{Organizer: calendar.Attendee{Email: "me@example.com", Self: true}}, "mine"},
		{"mine: no organizer (solo/local)", calendar.Event{Summary: "혼자 일정"}, "mine"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eventCategory(tc.ev); got != tc.want {
				t.Errorf("eventCategory(%+v) = %q, want %q", tc.ev, got, tc.want)
			}
		})
	}
}

// TestProjectEventOut_SetsCategory confirms the wire projection carries the
// bucket through so the native grid can color by it.
func TestProjectEventOut_SetsCategory(t *testing.T) {
	out := projectEventOut(calendar.Event{
		ID:        "g1",
		Summary:   "에코프로 미팅",
		Organizer: calendar.Attendee{Email: "boss@example.com"},
	}, false)
	if out.Category != "others" {
		t.Errorf("Category = %q, want %q", out.Category, "others")
	}
}
