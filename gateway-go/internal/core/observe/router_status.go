package observe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// RouterModelStatus is one router entry's live operational state: whether its
// circuit is open (traffic held off it), what its upstream key probe said, and
// whether its upstream still lists the model. This is why a local share can be
// low while the engine is up — an open circuit keeps turns on the fallback
// for its whole cooldown.
type RouterModelStatus struct {
	Name  string `json:"name"`
	Local bool   `json:"local"`
	// CircuitState is "closed", "degraded", "open" or "half_open".
	CircuitState    string `json:"circuitState"`
	CircuitFailures int    `json:"circuitFailures,omitempty"`
	RetryAfterMS    int64  `json:"retryAfterMs,omitempty"`
	// KeyHealth is the upstream-auth probe for a cloud entry ("ok",
	// "auth_failed", "rate_limited", "unreachable", "http_N", "unchecked");
	// empty for local, keyless entries.
	KeyHealth string `json:"keyHealth,omitempty"`
	// UpstreamMissing: the backend answered /v1/models without this model —
	// every call to the entry fails over.
	UpstreamMissing bool `json:"upstreamMissing,omitempty"`
}

// RouterStatus is the router's GET /status readout, reduced to what the
// engine panel shows.
type RouterStatus struct {
	Models []RouterModelStatus `json:"models"`
}

// ByName indexes the entries.
func (s RouterStatus) ByName() map[string]RouterModelStatus {
	out := make(map[string]RouterModelStatus, len(s.Models))
	for _, m := range s.Models {
		out[m.Name] = m
	}
	return out
}

// FetchRouterStatus reads GET /status from the router at baseURL, by the same
// owned-host rule and timeout as the usage meter. ok=false on any failure.
func FetchRouterStatus(ctx context.Context, baseURL, token string) (RouterStatus, bool) {
	endpoint := routerStatusURL(baseURL)
	if endpoint == "" {
		return RouterStatus{}, false
	}
	reqCtx, cancel := context.WithTimeout(ctx, routerUsageTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return RouterStatus{}, false
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httputil.NewClient(routerUsageTimeout).Do(req)
	if err != nil {
		return RouterStatus{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RouterStatus{}, false
	}
	var out RouterStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return RouterStatus{}, false
	}
	return out, true
}

// routerStatusURL is the /status sibling of routerUsageURL: the router's bare
// host or its /v1 base become <host>/status, and only an owned host qualifies.
func routerStatusURL(baseURL string) string {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	if !httputil.IsPrivateHost(u.Hostname()) {
		return ""
	}
	path := strings.TrimRight(u.Path, "/")
	path = strings.TrimSuffix(path, "/v1")
	u.Path = path + "/status"
	return u.String()
}
