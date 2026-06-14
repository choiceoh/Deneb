// wormhole.go — miniapp.wormhole.* RPC surface: read-only status of the wormhole
// model router (cmd/wormhole) plus live feature toggles. The gateway reads the
// wormhole config file directly and probes its /health; wormhole hot-reloads the
// file, so a toggle written here takes effect within a few seconds with no
// restart. Upstream provider keys are NEVER read out or returned — whConfig omits
// the `key` fields entirely, and set_feature preserves them as raw bytes.
package handlerminiapp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// WormholeStatusOut is the wormhole router's live state for the native settings
// tab: reachability, the global feature flags, and the configured models (names
// and classification only — no keys).
//
//deneb:wire
type WormholeStatusOut struct {
	Reachable     bool               `json:"reachable"`
	Listen        string             `json:"listen,omitempty"`
	LocalOnly     bool               `json:"localOnly"`
	EffortRouting bool               `json:"effortRouting"`
	Auto          []string           `json:"auto,omitempty"`
	Models        []WormholeModelOut `json:"models"`
}

// WormholeModelOut is one configured model: name, wire protocol, whether it stays
// on-box (local), and whether it's effort-routable (has a thinking toggle).
//
//deneb:wire
type WormholeModelOut struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Local    bool   `json:"local"`
	Thinking bool   `json:"thinking"`
}

// WormholeDeps is the wiring for the wormhole status/toggle handlers. Empty
// fields fall back to the env (DENEB_WORMHOLE_CONFIG / DENEB_WORMHOLE_URL) and
// then to the on-host defaults, so the standard single-machine layout needs no
// explicit wiring. A missing config / unreachable wormhole degrades gracefully
// (Reachable=false, empty models) rather than failing registration.
type WormholeDeps struct {
	ConfigPath string // wormhole config file (default ~/.wormhole/config.json)
	BaseURL    string // wormhole base URL for the health probe (default http://127.0.0.1:18800)
}

// WormholeMethods returns the miniapp.wormhole.* handler map.
func WormholeMethods(deps WormholeDeps) map[string]rpcutil.HandlerFunc {
	if deps.ConfigPath == "" {
		if p := os.Getenv("DENEB_WORMHOLE_CONFIG"); p != "" {
			deps.ConfigPath = p
		} else if home, err := os.UserHomeDir(); err == nil {
			deps.ConfigPath = home + "/.wormhole/config.json"
		}
	}
	if deps.BaseURL == "" {
		if u := os.Getenv("DENEB_WORMHOLE_URL"); u != "" {
			deps.BaseURL = u
		} else {
			deps.BaseURL = "http://127.0.0.1:18800"
		}
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.wormhole.status":      wormholeStatus(deps),
		"miniapp.wormhole.set_feature": wormholeSetFeature(deps),
	}
}

// whConfig mirrors the subset of the wormhole config the gateway needs. The model
// `key` fields are DELIBERATELY absent so an upstream secret is never deserialized
// here — only names and classification cross the wire.
type whConfig struct {
	Listen        string   `json:"listen"`
	LocalOnly     bool     `json:"localOnly"`
	EffortRouting *bool    `json:"effortRouting"`
	Auto          []string `json:"auto"`
	Models        []struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		Protocol    string `json:"protocol"`
		ToggleKwarg string `json:"toggleKwarg"`
		Local       *bool  `json:"local"`
	} `json:"models"`
}

func wormholeStatus(deps WormholeDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var cfg whConfig
		if b, err := os.ReadFile(deps.ConfigPath); err == nil {
			_ = json.Unmarshal(b, &cfg)
		}
		out := WormholeStatusOut{
			Reachable:     wormholeReachable(ctx, deps.BaseURL),
			Listen:        cfg.Listen,
			LocalOnly:     cfg.LocalOnly,
			EffortRouting: cfg.EffortRouting == nil || *cfg.EffortRouting,
			Auto:          cfg.Auto,
			Models:        make([]WormholeModelOut, 0, len(cfg.Models)),
		}
		for _, m := range cfg.Models {
			proto := m.Protocol
			if proto == "" {
				proto = "openai"
			}
			out.Models = append(out.Models, WormholeModelOut{
				Name:     m.Name,
				Protocol: proto,
				Local:    modelIsLocal(m.Local, m.URL),
				Thinking: m.ToggleKwarg != "",
			})
		}
		return rpcutil.RespondOK(req.ID, out)
	}
}

// wormholeSetFeature flips a global wormhole feature flag (localOnly or
// effortRouting) by rewriting just that key in the config file — every other
// field, including the model keys, is preserved as raw bytes. wormhole's watcher
// picks the change up within seconds.
func wormholeSetFeature(deps WormholeDeps) rpcutil.HandlerFunc {
	type params struct {
		Feature string `json:"feature"`
		Enabled bool   `json:"enabled"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if p.Feature != "localOnly" && p.Feature != "effortRouting" {
			return rpcerr.InvalidRequest("feature must be 'localOnly' or 'effortRouting'").Response(req.ID)
		}
		raw, err := os.ReadFile(deps.ConfigPath)
		if err != nil {
			return rpcerr.WrapUnavailable("wormhole config read failed", err).Response(req.ID)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return rpcerr.WrapUnavailable("wormhole config parse failed", err).Response(req.ID)
		}
		enc, _ := json.Marshal(p.Enabled)
		fields[p.Feature] = enc
		out, err := json.MarshalIndent(fields, "", "  ")
		if err != nil {
			return rpcerr.WrapUnavailable("wormhole config encode failed", err).Response(req.ID)
		}
		// Atomic write: temp + rename on the same dir so a reader (the watcher)
		// never sees a half-written file.
		tmp := deps.ConfigPath + ".tmp"
		if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
			return rpcerr.WrapUnavailable("wormhole config write failed", err).Response(req.ID)
		}
		if err := os.Rename(tmp, deps.ConfigPath); err != nil {
			_ = os.Remove(tmp)
			return rpcerr.WrapUnavailable("wormhole config swap failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true, "feature": p.Feature, "enabled": p.Enabled})
	}
}

// wormholeReachable probes the wormhole /health endpoint with a short timeout.
func wormholeReachable(ctx context.Context, baseURL string) bool {
	if baseURL == "" {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	reqr, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := httputil.NewClient(2 * time.Second).Do(reqr)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// modelIsLocal mirrors wormhole's local/cloud classification for display: the
// explicit override wins, else a loopback/private/localhost URL is local.
func modelIsLocal(override *bool, rawURL string) bool {
	if override != nil {
		return *override
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
