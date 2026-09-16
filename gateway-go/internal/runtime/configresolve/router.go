package configresolve

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// routerConfigEnv points the router lookup at another config file. Empty →
// ~/.wormhole/config.json.
const routerConfigEnv = "DENEB_WORMHOLE_CONFIG"

// RouterMeter returns what is needed to read the router's own per-model meter:
// its base URL, the gate token, and which of its entries point at a local
// serving engine.
//
// The local set is derived from each entry's URL rather than from a flag: an
// entry is local when it targets a host this deployment owns, which is the same
// rule every other engine probe uses. That keeps one definition of "local"
// instead of a second list that drifts from the first.
//
// Everything is empty when the config is absent or unreadable. The meter is a
// diagnostic; its absence must never be an error.
func RouterMeter(logger *slog.Logger) (baseURL, token string, localModels map[string]bool) {
	path := strings.TrimSpace(os.Getenv(routerConfigEnv))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", nil
		}
		path = filepath.Join(home, ".wormhole", "config.json")
	}
	raw, err := os.ReadFile(path) //nolint:gosec // operator-owned router config, path from env or $HOME
	if err != nil {
		return "", "", nil
	}
	var cfg struct {
		Listen string `json:"listen"`
		Token  string `json:"token"`
		Models []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		if logger != nil {
			logger.Warn("router config unparseable; the routing meter is unavailable", "path", path, "error", err)
		}
		return "", "", nil
	}
	local := make(map[string]bool)
	for _, m := range cfg.Models {
		if name := strings.TrimSpace(m.Name); name != "" && isLocalRouterTarget(m.URL) {
			local[name] = true
		}
	}
	// The router's own token, with ${ENV} expansion the way it loads it itself.
	// Used ONLY for the loopback meter call; it is never logged or returned to
	// any caller outside this process.
	return routerBaseURL(cfg.Listen), os.ExpandEnv(cfg.Token), local
}

// RouterEntryNames lists every entry name in the router's current config —
// what the meter can be reconciled against. A metered name absent from this
// set belongs to an entry that was renamed or removed inside the month; it is
// neither local nor remote now, and must not be counted as either.
func RouterEntryNames() map[string]bool {
	path := strings.TrimSpace(os.Getenv(routerConfigEnv))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(home, ".wormhole", "config.json")
	}
	raw, err := os.ReadFile(path) //nolint:gosec // operator-owned router config, path from env or $HOME
	if err != nil {
		return nil
	}
	var cfg struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	out := make(map[string]bool, len(cfg.Models))
	for _, m := range cfg.Models {
		if name := strings.TrimSpace(m.Name); name != "" {
			out[name] = true
		}
	}
	return out
}

// routerBaseURL turns the router's listen address into a loopback base URL.
// A router listening on all interfaces is still reached over loopback.
func routerBaseURL(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// isLocalRouterTarget reports whether a router entry's upstream is a host this
// deployment owns. Parsing failures read as remote: a target we cannot classify
// is not one we should credit to the local engine.
func isLocalRouterTarget(rawURL string) bool {
	host := hostOf(rawURL)
	if host == "" {
		return false
	}
	return isOwnedHost(host)
}

// hostOf and isOwnedHost keep this file free of a chat/pkg import cycle while
// using the same rule as pkg/httputil.IsPrivateHost.
func hostOf(rawURL string) string {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimRight(u.Hostname(), "."))
}

// isOwnedHost is pkg/httputil.IsPrivateHost's rule: only the literal name
// "localhost", loopback, RFC1918, or the CGNAT range the fleet reaches its
// other nodes through. Every other name is rejected rather than resolved, so
// DNS never decides what counts as local.
func isOwnedHost(host string) bool {
	return httputil.IsPrivateHost(host)
}
