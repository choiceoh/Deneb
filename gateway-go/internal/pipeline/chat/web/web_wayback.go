// web_wayback.go — Serve a dead link from the archive instead of reporting a 404.
//
// A link in a wiki page, an old mail, or a search result rots. Today that ends
// the trail: the tool reports 404/410 and the model has nothing. The Internet
// Archive usually still has the page, and for the reference material this
// assistant follows — documentation, articles, standards, vendor pages — an
// archived copy answers the question the live URL no longer can.
//
// Deliberately narrow. Only *gone* means gone: 404 and 410. A 403 is a bot wall
// the stealth ladder handles, a 5xx is a server having a bad minute and will be
// there on retry, and a timeout says nothing about the page existing. Reaching
// for the archive on those would hide a recoverable failure behind a stale copy.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	waybackAPI     = "https://archive.org/wayback/available?url="
	waybackTimeout = 6 * time.Second
)

// waybackSnapshot is the archived copy the API pointed at.
type waybackSnapshot struct {
	URL       string
	Timestamp string
}

// waybackLookupFn is swapped in tests so the recovery path is exercised without
// network.
var waybackLookupFn = lookupWaybackSnapshot

// lookupWaybackSnapshot asks the Internet Archive for the most recent usable
// snapshot of targetURL. Absence is not an error: most URLs have none, and the
// caller then reports the original failure.
func lookupWaybackSnapshot(ctx context.Context, targetURL string) (waybackSnapshot, bool) {
	reqCtx, cancel := context.WithTimeout(ctx, waybackTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, waybackAPI+targetURL, nil)
	if err != nil {
		return waybackSnapshot{}, false
	}
	resp, err := SharedClient(waybackTimeout).Do(req)
	if err != nil {
		return waybackSnapshot{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return waybackSnapshot{}, false
	}

	var payload struct {
		ArchivedSnapshots struct {
			Closest struct {
				Available bool   `json:"available"`
				URL       string `json:"url"`
				Timestamp string `json:"timestamp"`
				Status    string `json:"status"`
			} `json:"closest"`
		} `json:"archived_snapshots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return waybackSnapshot{}, false
	}
	closest := payload.ArchivedSnapshots.Closest
	// The archive records failed crawls too; a snapshot that archived an error
	// page is worth no more than the dead link itself.
	if !closest.Available || strings.TrimSpace(closest.URL) == "" || closest.Status != "200" {
		return waybackSnapshot{}, false
	}
	return waybackSnapshot{URL: closest.URL, Timestamp: closest.Timestamp}, true
}

// isGoneStatus reports whether a status means the page no longer exists, as
// opposed to being temporarily unreachable or blocked.
func isGoneStatus(status int) bool {
	return status == http.StatusNotFound || status == http.StatusGone
}

// waybackNote is the metadata line that keeps the substitution honest: the model
// must know it is reading an old copy, and when it was taken.
func waybackNote(original string, snap waybackSnapshot) string {
	when := formatWaybackTimestamp(snap.Timestamp)
	if when == "" {
		return fmt.Sprintf("Archived: original %s is gone; served from the Internet Archive", original)
	}
	return fmt.Sprintf("Archived: original %s is gone; served from the Internet Archive snapshot of %s", original, when)
}

// formatWaybackTimestamp renders the archive's YYYYMMDDhhmmss stamp as a date.
func formatWaybackTimestamp(ts string) string {
	ts = strings.TrimSpace(ts)
	if len(ts) < 8 {
		return ""
	}
	parsed, err := time.Parse("20060102", ts[:8])
	if err != nil {
		return ""
	}
	return parsed.Format("2006-01-02")
}

// logWaybackRecovery records the substitution. Info, not Debug: a page the model
// cites came from an archive, which is worth being able to see afterwards.
func logWaybackRecovery(original string, snap waybackSnapshot) {
	slog.Info("web fetch served from archive",
		"url", original, "snapshot", snap.URL, "timestamp", snap.Timestamp)
}

// statusFromFetchErrCode reads the status back out of a classified code
// ("http_404" → 404). Zero when the failure was not an HTTP status at all —
// DNS, TLS, timeout — which is the correct answer for the archive decision.
func statusFromFetchErrCode(code string) int {
	rest, ok := strings.CutPrefix(code, "http_")
	if !ok {
		return 0
	}
	status := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0
		}
		status = status*10 + int(r-'0')
	}
	return status
}
