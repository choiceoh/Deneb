// router_usage.go reads the wormhole router's own per-model meter.
//
// Why this is the only honest source: the local engine and its cloud twin serve
// under the SAME model name (both answer as "glm-5.3-flash"), so a turn's
// providerModel cannot say which one ran. The router can: it meters against the
// entry that ACTUALLY served, after failover (cmd/wormhole/router.go — commit()
// receives the entry the loop settled on, not the one the caller asked for).
//
// Without this the gateway can measure how fast the local engine is and never
// notice it served a third of the traffic. Between 2026-09-06 and 09-13 the
// router failed over 4,949 times from the local entry, every one of them
// silently: the substitute is the same model on a flat-rate subscription, so
// nothing in the reply, the token counts or the latency says it happened.
package observe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

const routerUsageTimeout = 3 * time.Second

// RouterModelUsage is one entry's served totals for the router's current window.
type RouterModelUsage struct {
	Model        string `json:"model"`
	Requests     int64  `json:"requests"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
}

// RouterUsage is the router's meter: which entries actually served, and how
// much. Window is the router's own label for the period (a month, today).
type RouterUsage struct {
	Window string             `json:"window"`
	Models []RouterModelUsage `json:"models"`
}

// FetchRouterUsage reads GET /v1/usage from the router at baseURL. ok=false on
// any failure — this is a diagnostic, never a dependency.
//
// Only a host this deployment owns is contacted (the router listens on
// loopback), by the same rule the engine probes use.
func FetchRouterUsage(ctx context.Context, baseURL, token string) (RouterUsage, bool) {
	endpoint := routerUsageURL(baseURL)
	if endpoint == "" {
		return RouterUsage{}, false
	}
	reqCtx, cancel := context.WithTimeout(ctx, routerUsageTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return RouterUsage{}, false
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httputil.NewClient(routerUsageTimeout).Do(req)
	if err != nil {
		return RouterUsage{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RouterUsage{}, false
	}
	var out RouterUsage
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return RouterUsage{}, false
	}
	sort.Slice(out.Models, func(i, j int) bool { return out.Models[i].Requests > out.Models[j].Requests })
	return out, true
}

// routerUsageURL turns any router URL (its /v1 base or the bare host) into the
// usage endpoint, or "" when the target is not a host we own.
func routerUsageURL(baseURL string) string {
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
	u.Path = path + "/v1/usage"
	return u.String()
}

// LocalShare splits the meter into what a local engine served and what a remote
// one did, using the caller's list of entry names that point at a local engine.
// Returns the two request totals; a model absent from localNames counts remote.
func (u RouterUsage) LocalShare(localNames map[string]bool) (local, remote int64) {
	for _, m := range u.Models {
		if localNames[m.Model] {
			local += m.Requests
			continue
		}
		remote += m.Requests
	}
	return local, remote
}
