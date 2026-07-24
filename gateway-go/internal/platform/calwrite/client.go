package calwrite

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/googleoauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// tokenURL is the Google OAuth2 token endpoint used when refreshing access tokens.
var tokenURL = "https://oauth2.googleapis.com/token" //nolint:gosec // G101 false positive — not a credential

// apiBase is the Calendar v3 root for the authenticated user's primary calendar.
// Kept a const; tests reroute via an http.RoundTripper (see client_test.go).
const apiBase = "https://www.googleapis.com/calendar/v3"

// eventsPath is the collection the write client POSTs/PATCHes/DELETEs against.
const eventsPath = "/calendars/primary/events"

// maxAPIResponseBytes bounds every external body read — same rationale as the
// read client: a runaway upstream must not balloon the always-on gateway.
const maxAPIResponseBytes = 16 << 20

// Client is a write-capable Calendar API client with auto-refreshing OAuth2
// tokens. It shares the credential/token file pair with the read-only client
// (~/.deneb/credentials/calendar_*.json); the operator must re-consent with a
// write scope (calendar.events) for the writes here to succeed.
type Client struct {
	tokens     *googleoauth.Source
	httpClient *http.Client
}

// writeEnabled reports whether the Deneb → Google write mirror is on. ON BY
// DEFAULT: the existing calendar token carries the full read/write scope
// (auth/calendar), so mirroring is the desired behavior; set
// DENEB_CALENDAR_GOOGLE_WRITE=0 to turn it off. When the mirror is on but no
// calendar credentials are present, DefaultSyncer's client init fails and every
// caller still degrades cleanly to local-only (no error surfaced, no log spam).
func writeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DENEB_CALENDAR_GOOGLE_WRITE"))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func credentialsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".deneb", "credentials")
}

func newClient() (*Client, error) {
	return newClientFromDir(credentialsDir())
}

// newClientFromDir loads credentials + token from dir. Extracted for tests; the
// live caller uses ~/.deneb/credentials.
func newClientFromDir(dir string) (*Client, error) {
	clientPath := filepath.Join(dir, "calendar_client.json")
	tokenPath := filepath.Join(dir, "calendar_token.json")

	loaded, err := googleoauth.Load("Calendar", clientPath, tokenPath)
	if err != nil {
		return nil, err
	}

	httpClient := httputil.NewClient(60 * time.Second)
	return &Client{
		tokens:     googleoauth.NewSource("Calendar", loaded, tokenPath, httpClient),
		httpClient: httpClient,
	}, nil
}

func (c *Client) validToken(ctx context.Context) (string, error) {
	return c.tokens.ValidToken(ctx, tokenURL)
}

func (c *Client) doAPI(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return googleoauth.DoBearer(ctx, c.httpClient, c.validToken, apiBase, method, path, body)
}

func apiOptions() googleoauth.APIOptions {
	return googleoauth.APIOptions{
		Service:          "Calendar",
		MaxResponseBytes: maxAPIResponseBytes,
		StatusError: func(statusCode int, body string) error {
			return &APIError{StatusCode: statusCode, Body: body}
		},
	}
}

// Insert creates a new event on the primary calendar and returns its Google
// event id, which the syncer stores so later edits/deletes target the same event.
func (c *Client) Insert(ctx context.Context, localID string, ev calendar.Event) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := googleoauth.JSON(ctx, c.doAPI, http.MethodPost, eventsPath, toGoogleEvent(localID, ev), &out, apiOptions()); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.ID) == "" {
		return "", fmt.Errorf("calwrite: Google insert returned no event id")
	}
	return out.ID, nil
}

// Patch updates an existing Google event in place (PATCH merges only the fields
// we send). googleID is the value stored by a prior Insert.
func (c *Client) Patch(ctx context.Context, googleID, localID string, ev calendar.Event) error {
	path := eventsPath + "/" + url.PathEscape(googleID)
	return googleoauth.JSON(ctx, c.doAPI, http.MethodPatch, path, toGoogleEvent(localID, ev), nil, apiOptions())
}

// Delete removes a Google event. An already-gone event (404/410) is treated as
// success — the desired end state (no such event) already holds. DELETE returns
// 204 No Content, which the generic JSON helper would reject, so it is handled
// directly here.
func (c *Client) Delete(ctx context.Context, googleID string) error {
	path := eventsPath + "/" + url.PathEscape(googleID)
	resp, err := c.doAPI(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponseBytes))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound, http.StatusGone:
		return nil
	default:
		return &APIError{StatusCode: resp.StatusCode, Body: truncate(string(body), 500)}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// APIError carries Calendar API HTTP failures so callers can branch on the
// status code instead of substring-matching. Mirrors calendar.APIError.
type APIError struct {
	StatusCode int
	Body       string
}

// Error returns the human-readable error message.
func (e *APIError) Error() string {
	return fmt.Sprintf("Calendar write API 오류 (HTTP %d): %s", e.StatusCode, e.Body) //nolint:staticcheck // ST1005 — Korean error message
}
