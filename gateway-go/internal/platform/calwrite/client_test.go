package calwrite

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// rewriteTransport reroutes apiBase-prefixed requests to the httptest server so
// apiBase can stay a const (mirrors the read client's operations_test).
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

// fixtureClient builds a Client whose credential/token files are valid (token
// not expired, so no refresh round-trip) and whose API calls hit srv.
func fixtureClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "calendar_client.json"), map[string]any{
		"installed": map[string]string{"client_id": "id", "client_secret": "secret"},
	})
	writeJSON(t, filepath.Join(dir, "calendar_token.json"), map[string]string{
		"access_token":  "ya29.write",
		"refresh_token": "1//refresh",
		"expiry":        time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	c, err := newClientFromDir(dir)
	if err != nil {
		t.Fatalf("newClientFromDir: %v", err)
	}
	c.httpClient.Transport = &rewriteTransport{base: srv.URL, inner: http.DefaultTransport}
	return c
}

func TestWriteEnabled_DefaultsOnAndRespectsDisable(t *testing.T) {
	t.Setenv("DENEB_CALENDAR_GOOGLE_WRITE", "")
	if !writeEnabled() {
		t.Error("mirror should be ON by default (empty/unset env)")
	}
	for _, off := range []string{"0", "false", "no", "off", "OFF"} {
		t.Setenv("DENEB_CALENDAR_GOOGLE_WRITE", off)
		if writeEnabled() {
			t.Errorf("%q should disable the mirror", off)
		}
	}
	t.Setenv("DENEB_CALENDAR_GOOGLE_WRITE", "1")
	if !writeEnabled() {
		t.Error(`"1" should keep the mirror on`)
	}
}

func timedEvent() calendar.Event {
	return calendar.Event{
		Summary:  "부산 현장 협의",
		Location: "강남 본사",
		Start:    time.Date(2026, 7, 24, 14, 0, 0, 0, time.UTC),
		End:      time.Date(2026, 7, 24, 15, 0, 0, 0, time.UTC),
	}
}

func TestInsert_PostsEventWithLocalIDTagAndParsesID(t *testing.T) {
	var gotMethod, gotPath string
	var body googleEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"gABC123"}`))
	}))
	t.Cleanup(srv.Close)

	c := fixtureClient(t, srv)
	gid, err := c.Insert(context.Background(), "local:99-1", timedEvent())
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if gid != "gABC123" {
		t.Errorf("google id = %q, want gABC123", gid)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/calendars/primary/events" {
		t.Errorf("path = %s", gotPath)
	}
	if body.Summary != "부산 현장 협의" {
		t.Errorf("summary = %q", body.Summary)
	}
	if body.Start.DateTime == "" || body.Start.Date != "" {
		t.Errorf("timed event should use dateTime, got start=%+v", body.Start)
	}
	if body.ExtendedProperties == nil || body.ExtendedProperties.Private[localIDProperty] != "local:99-1" {
		t.Errorf("missing denebLocalId tag: %+v", body.ExtendedProperties)
	}
}

func TestInsert_AllDayUsesDateBounds(t *testing.T) {
	var body googleEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":"g1"}`))
	}))
	t.Cleanup(srv.Close)

	ev := calendar.Event{
		Summary: "휴가",
		AllDay:  true,
		Start:   time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC),
	}
	if _, err := fixtureClient(t, srv).Insert(context.Background(), "local:1-1", ev); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if body.Start.Date != "2026-07-24" || body.End.Date != "2026-07-25" {
		t.Errorf("all-day bounds = start:%q end:%q", body.Start.Date, body.End.Date)
	}
	if body.Start.DateTime != "" {
		t.Errorf("all-day event must not send dateTime: %q", body.Start.DateTime)
	}
}

func TestInsert_EmptyIDIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	if _, err := fixtureClient(t, srv).Insert(context.Background(), "local:1-1", timedEvent()); err == nil {
		t.Fatal("expected error when Google returns no id")
	}
}

func TestPatch_TargetsEventPath(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"id":"gABC"}`))
	}))
	t.Cleanup(srv.Close)
	if err := fixtureClient(t, srv).Patch(context.Background(), "gABC", "local:1-1", timedEvent()); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", gotMethod)
	}
	if gotPath != "/calendars/primary/events/gABC" {
		t.Errorf("path = %s", gotPath)
	}
}

func TestDelete_204IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	if err := fixtureClient(t, srv).Delete(context.Background(), "gABC"); err != nil {
		t.Fatalf("Delete 204 should succeed: %v", err)
	}
}

func TestDelete_AlreadyGoneIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	t.Cleanup(srv.Close)
	if err := fixtureClient(t, srv).Delete(context.Background(), "gGone"); err != nil {
		t.Fatalf("Delete 410 should be treated as success: %v", err)
	}
}

func TestDelete_ServerErrorSurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	t.Cleanup(srv.Close)
	err := fixtureClient(t, srv).Delete(context.Background(), "gABC")
	if err == nil {
		t.Fatal("expected error on 500")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("want *APIError with 500, got %v", err)
	}
}
