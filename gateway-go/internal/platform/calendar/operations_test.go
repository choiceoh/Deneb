package calendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixtureClient stands up an httptest server that mimics Calendar's
// /calendars/primary/events surface plus the OAuth token endpoint. The
// caller controls the events JSON returned.
func fixtureClient(t *testing.T, eventsJSON string) *Client {
	t.Helper()
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "calendar_client.json"), map[string]any{
		"installed": map[string]string{"client_id": "id", "client_secret": "secret"},
	})
	writeJSON(t, filepath.Join(dir, "calendar_token.json"), map[string]string{
		"access_token":  "ya29.fixture",
		"refresh_token": "1//refresh",
		// 1h-in-the-future so validToken skips refresh.
		"expiry": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/calendars/primary/events") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(eventsJSON))
	}))
	t.Cleanup(srv.Close)

	c, err := newClientFromDir(dir)
	if err != nil {
		t.Fatalf("newClientFromDir: %v", err)
	}
	// Reroute apiBase via the httpClient transport by rewriting requests.
	c.httpClient.Transport = &rewriteTransport{base: srv.URL, inner: http.DefaultTransport}
	return c
}

// rewriteTransport rewrites the Host portion of outgoing requests to
// point at the httptest server. Lets us keep apiBase as a constant.
type rewriteTransport struct {
	base  string
	inner http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasPrefix(req.URL.String(), apiBase) {
		newURL, err := url.Parse(rt.base + strings.TrimPrefix(req.URL.String(), apiBase))
		if err != nil {
			return nil, err
		}
		req.URL = newURL
		req.Host = newURL.Host
	}
	return rt.inner.RoundTrip(req)
}

func TestListUpcoming_HappyPath(t *testing.T) {
	body := `{"items":[
		{"id":"e1","summary":"부산8 진척 협의","status":"confirmed",
		 "start":{"dateTime":"2026-05-26T14:00:00+09:00"},
		 "end":{"dateTime":"2026-05-26T15:00:00+09:00"},
		 "location":"강남 본사",
		 "attendees":[
		   {"email":"PARK@example.com","displayName":"박YY","responseStatus":"accepted"},
		   {"email":"me@example.com","self":true}
		 ]
		},
		{"id":"e2","summary":"휴가","status":"cancelled",
		 "start":{"date":"2026-05-27"},"end":{"date":"2026-05-28"}}
	]}`
	c := fixtureClient(t, body)
	evs, err := c.ListUpcoming(context.Background(), time.Now(), time.Now().Add(48*time.Hour), 50)
	if err != nil {
		t.Fatalf("ListUpcoming: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("got %d events (cancelled should be filtered), want 1", len(evs))
	}
	ev := evs[0]
	if ev.ID != "e1" || ev.Summary != "부산8 진척 협의" {
		t.Errorf("ev = %+v", ev)
	}
	if ev.Location != "강남 본사" {
		t.Errorf("Location = %q", ev.Location)
	}
	if len(ev.Attendees) != 2 {
		t.Fatalf("Attendees = %d, want 2", len(ev.Attendees))
	}
	if ev.Attendees[0].Email != "park@example.com" {
		t.Errorf("attendee email not normalized to lowercase: %q", ev.Attendees[0].Email)
	}
	if !ev.Attendees[1].Self {
		t.Error("self flag not preserved on second attendee")
	}
}

func TestListUpcoming_AllDay(t *testing.T) {
	body := `{"items":[
		{"id":"e1","summary":"휴가","status":"confirmed",
		 "start":{"date":"2026-05-27","timeZone":"Asia/Seoul"},
		 "end":{"date":"2026-05-28","timeZone":"Asia/Seoul"}}
	]}`
	c := fixtureClient(t, body)
	evs, err := c.ListUpcoming(context.Background(), time.Now(), time.Now().Add(48*time.Hour), 50)
	if err != nil {
		t.Fatalf("ListUpcoming: %v", err)
	}
	if len(evs) != 1 || !evs[0].AllDay {
		t.Fatalf("expected one all-day event, got %+v", evs)
	}
	wantLoc, _ := time.LoadLocation("Asia/Seoul")
	got := evs[0].Start.In(wantLoc).Format("2006-01-02")
	if got != "2026-05-27" {
		t.Errorf("all-day start = %q, want 2026-05-27", got)
	}
}

func TestListUpcoming_ConferenceURI(t *testing.T) {
	body := `{"items":[{
		"id":"meet","summary":"Standup","status":"confirmed",
		"start":{"dateTime":"2026-05-26T10:00:00+09:00"},
		"end":{"dateTime":"2026-05-26T10:30:00+09:00"},
		"conferenceData":{
			"conferenceSolution":{"key":{"type":"hangoutsMeet"}},
			"entryPoints":[
				{"entryPointType":"phone","uri":"tel:+82-2-555-0000"},
				{"entryPointType":"video","uri":"https://meet.google.com/abc-defg-hij"}
			]
		}
	}]}`
	c := fixtureClient(t, body)
	evs, err := c.ListUpcoming(context.Background(), time.Now(), time.Now().Add(2*time.Hour), 50)
	if err != nil {
		t.Fatalf("ListUpcoming: %v", err)
	}
	if len(evs) != 1 || evs[0].Conference == nil {
		t.Fatalf("expected conference, got %+v", evs)
	}
	if evs[0].Conference.URI != "https://meet.google.com/abc-defg-hij" {
		t.Errorf("conference URI = %q (should pick video over phone)", evs[0].Conference.URI)
	}
	if evs[0].Conference.Solution != "hangoutsMeet" {
		t.Errorf("solution = %q", evs[0].Conference.Solution)
	}
}

func TestGet_MissingIDFails(t *testing.T) {
	c := fixtureClient(t, "{}")
	if _, err := c.Get(context.Background(), "  "); err == nil {
		t.Fatal("expected error for empty event ID")
	}
}

func TestGet_HappyPath(t *testing.T) {
	body := `{"id":"e1","summary":"hello","status":"confirmed",
		"start":{"dateTime":"2026-05-26T14:00:00+09:00"},
		"end":{"dateTime":"2026-05-26T15:00:00+09:00"}}`
	c := fixtureClient(t, body)
	ev, err := c.Get(context.Background(), "e1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ev == nil || ev.ID != "e1" {
		t.Fatalf("ev = %+v", ev)
	}
}

func TestParseEventTime_Fallbacks(t *testing.T) {
	// Empty input → zero time, allDay false.
	tm, allDay := parseEventTime(apiEventDateTime{})
	if !tm.IsZero() || allDay {
		t.Errorf("empty: got %v %v", tm, allDay)
	}

	// Invalid dateTime falls through to date.
	tm, allDay = parseEventTime(apiEventDateTime{DateTime: "not-rfc3339", Date: "2026-05-27"})
	if tm.IsZero() || !allDay {
		t.Errorf("dateTime fallback: %v %v", tm, allDay)
	}
}

// Sanity check: malformed JSON in Calendar response surfaces an error
// rather than a panic. Ensures readJSON propagates Unmarshal failures.
func TestReadJSON_MalformedSurfaces(t *testing.T) {
	c := fixtureClient(t, "not json at all")
	if _, err := c.ListUpcoming(context.Background(), time.Now(), time.Now().Add(time.Hour), 10); err == nil {
		t.Fatal("expected JSON decode error")
	}
}

// confirmHomeIsNotTouched ensures the credentialsDir() helper returns a
// child of the user's home, so tests that swap to t.TempDir() never
// silently fall back to a real ~/.deneb on machines without the dir.
func TestCredentialsDir_RootedInHome(t *testing.T) {
	dir := credentialsDir()
	home, _ := os.UserHomeDir()
	if home != "" && !strings.HasPrefix(dir, home) {
		t.Errorf("credentialsDir = %q, expected to be under %q", dir, home)
	}
}

// unused vars touchpoint to keep encoding/json import — guard against
// future refactors stripping the import accidentally.
var _ = json.Marshal
