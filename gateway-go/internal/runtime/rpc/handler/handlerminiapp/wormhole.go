// wormhole.go — miniapp.wormhole.* RPC surface: read-only status of the wormhole
// model router (cmd/wormhole) plus live feature toggles. The gateway reads the
// wormhole config file directly and probes its /health; wormhole hot-reloads the
// file, so a toggle written here takes effect within a few seconds with no
// restart. Upstream provider keys are NEVER read out or returned — whConfig omits
// the `key` fields entirely, and set_feature preserves them as raw bytes.
package handlerminiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

// WormholeModelOut is one routable model: name, wire protocol, whether it stays
// on-box (local), whether it's effort-routable (has a thinking toggle), its source
// — "config" (declared in the file) or "fleet" (auto-discovered from SparkFleet) —
// and KeyHealth: the last cloud-key auth probe ("ok" | "auth_failed" |
// "rate_limited" | "unreachable" | "http_N" | "unchecked"; empty for local/keyless
// models or when the live view is unavailable). Lets the picker flag a dead key.
//
//deneb:wire
type WormholeModelOut struct {
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Local     bool   `json:"local"`
	Thinking  bool   `json:"thinking"`
	Source    string `json:"source"`
	KeyHealth string `json:"keyHealth,omitempty"`
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
		"miniapp.wormhole.set_key":     wormholeSetKey(deps),
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
		// Prefer wormhole's live /status: it carries the discovered SparkFleet
		// models and reflects hot-reloaded toggles, which the static config file
		// can't. Fall back to the config-file view when wormhole is unreachable or
		// rejects the token (then we show only configured models, reachable from a
		// /health probe).
		if live, ok := wormholeLiveStatus(ctx, deps.BaseURL, wormholeToken(deps.ConfigPath)); ok {
			return rpcutil.RespondOK(req.ID, statusFromLive(live))
		}
		return rpcutil.RespondOK(req.ID, statusFromConfigFile(ctx, deps))
	}
}

// statusFromLive maps wormhole's live /status readout onto the wire shape.
func statusFromLive(live *whLiveStatus) WormholeStatusOut {
	out := WormholeStatusOut{
		Reachable:     true,
		Listen:        live.Listen,
		LocalOnly:     live.LocalOnly,
		EffortRouting: live.EffortRouting,
		Auto:          live.Auto,
		Models:        make([]WormholeModelOut, 0, len(live.Models)),
	}
	for _, m := range live.Models {
		proto := m.Protocol
		if proto == "" {
			proto = "openai"
		}
		src := m.Source
		if src == "" {
			src = "config"
		}
		out.Models = append(out.Models, WormholeModelOut{
			Name:      m.Name,
			Protocol:  proto,
			Local:     m.Local,
			Thinking:  m.Thinking,
			Source:    src,
			KeyHealth: m.KeyHealth,
		})
	}
	return out
}

// statusFromConfigFile is the fallback view when wormhole's live /status is
// unavailable: the configured models read straight from the file (no discovered
// models), with reachability from a /health probe. Source is always "config".
func statusFromConfigFile(ctx context.Context, deps WormholeDeps) WormholeStatusOut {
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
			Source:   "config",
		})
	}
	return out
}

// whLiveStatus mirrors wormhole's GET /status response (cmd/wormhole/status.go).
// Parsed with its own struct — loose coupling, like the SparkFleet discovery
// client — so the two binaries share a shape, not a package. No key field exists
// here: /status never emits one.
type whLiveStatus struct {
	Listen        string   `json:"listen"`
	LocalOnly     bool     `json:"localOnly"`
	EffortRouting bool     `json:"effortRouting"`
	Auto          []string `json:"auto"`
	Models        []struct {
		Name      string `json:"name"`
		Protocol  string `json:"protocol"`
		Local     bool   `json:"local"`
		Thinking  bool   `json:"thinking"`
		Source    string `json:"source"`
		KeyHealth string `json:"keyHealth"`
	} `json:"models"`
}

// wormholeToken reads the wormhole gate token from the config (with ${ENV}
// expansion, matching wormhole's own loadConfig). Used ONLY to authenticate the
// gateway→wormhole /status call; it is never placed in any response. If the env
// var is absent the expansion yields "" and the /status call goes unauthenticated
// — wormhole then rejects it and we fall back to the config-file view.
func wormholeToken(configPath string) string {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var probe struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(b, &probe)
	return os.ExpandEnv(probe.Token)
}

// wormholeLiveStatus fetches wormhole's rich GET /status — the live routing table
// including SparkFleet-discovered models. Authenticated with the wormhole token.
// Returns ok=false (so the caller falls back to the static file view) when
// wormhole is unreachable, rejects the token, or returns an unparseable body.
func wormholeLiveStatus(ctx context.Context, baseURL, token string) (*whLiveStatus, bool) {
	if baseURL == "" {
		return nil, false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	reqr, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/status", nil)
	if err != nil {
		return nil, false
	}
	if token != "" {
		reqr.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httputil.NewClient(2 * time.Second).Do(reqr)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var live whLiveStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&live); err != nil {
		return nil, false
	}
	return &live, true
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

// wormholeSetKey rotates a cloud model's upstream key WITHOUT a restart. The model
// keeps its config `key` as a ${VAR} ref; this writes the new value into wormhole's
// secrets.env, which wormhole hot-reloads (cmd/wormhole/secrets.go) — so the secret
// never lands in the config file and there is no secret-write endpoint on wormhole
// itself (the already-authenticated gateway owns the file). After writing, it probes
// the model through wormhole to give the UI an instant verdict.
func wormholeSetKey(deps WormholeDeps) rpcutil.HandlerFunc {
	type params struct {
		Model string `json:"model"`
		Key   string `json:"key"`
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
		p.Model = strings.TrimSpace(p.Model)
		p.Key = strings.TrimSpace(p.Key)
		if p.Model == "" || p.Key == "" {
			return rpcerr.InvalidRequest("model and key are required").Response(req.ID)
		}
		// Resolve the model's ${VAR} key ref so the new value lands in secrets.env,
		// not as a literal in the config. The key field is read ONLY to extract the
		// var name — its value is never returned or logged.
		varName, err := wormholeModelKeyVar(deps.ConfigPath, p.Model)
		if err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		if err := upsertSecretLine(secretsPathFor(deps.ConfigPath), varName, p.Key); err != nil {
			return rpcerr.WrapUnavailable("wormhole secrets write failed", err).Response(req.ID)
		}
		// Validate end-to-end: wait for wormhole to hot-reload secrets.env, then probe
		// the model through it. Best-effort — the write already succeeded; the UI just
		// won't get an instant verdict if the probe times out.
		valid, status := validateModelKey(ctx, deps.BaseURL, wormholeToken(deps.ConfigPath), p.Model)
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":       true,
			"model":    p.Model,
			"variable": varName,
			"valid":    valid,
			"status":   status,
		})
	}
}

// wormholeModelKeyVar reads a model's config `key` and returns the env var name it
// references (e.g. "${MIMO_KEY}" → "MIMO_KEY"). Errors if the model is absent, has
// no key, or pins a literal (not yet migrated to a ${VAR} ref). The key value is
// only inspected to classify it — never returned in the error or anywhere else.
func wormholeModelKeyVar(configPath, model string) (string, error) {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read wormhole config: %w", err)
	}
	var cfg struct {
		Models []struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return "", fmt.Errorf("parse wormhole config: %w", err)
	}
	for _, m := range cfg.Models {
		if m.Name != model {
			continue
		}
		v := strings.TrimSpace(m.Key)
		switch {
		case v == "":
			return "", fmt.Errorf("model %q has no key (local/keyless — nothing to rotate)", model)
		case strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}"):
			name := strings.TrimSpace(v[2 : len(v)-1])
			if name == "" {
				return "", fmt.Errorf("model %q key is an empty ${} reference", model)
			}
			return name, nil
		default:
			return "", fmt.Errorf("model %q key is a literal, not a ${VAR} ref — migrate it to secrets.env first", model)
		}
	}
	return "", fmt.Errorf("model %q not found in wormhole config", model)
}

// secretsPathFor is the secrets file wormhole watches, sibling to its config
// (mirrors cmd/wormhole/secrets.go:secretsFileFor).
func secretsPathFor(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "secrets.env")
}

// upsertSecretLine sets KEY=val in a dotenv file, replacing an existing KEY line or
// appending one, preserving other lines (and comments). Atomic write at mode 0600.
func upsertSecretLine(path, key, val string) error {
	var lines []string
	replaced := false
	if b, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if k, _, ok := strings.Cut(strings.TrimSpace(line), "="); ok && strings.TrimSpace(k) == key {
				lines = append(lines, key+"="+val)
				replaced = true
			} else {
				lines = append(lines, line)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if !replaced {
		lines = append(lines, key+"="+val)
	}
	content := strings.Join(lines, "\n") + "\n"
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// validateModelKey waits for wormhole to hot-reload the rotated secrets.env (its
// config watcher ticks ~3s), then sends a 1-token completion through wormhole to the
// model. Returns whether it authenticated (HTTP 200) and the status. Best-effort:
// any transport error returns (false, 0).
func validateModelKey(ctx context.Context, baseURL, token, model string) (bool, int) {
	if baseURL == "" {
		return false, 0
	}
	select {
	case <-ctx.Done():
		return false, 0
	case <-time.After(4 * time.Second): // let wormhole's watcher pick up the new secret
	}
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
	})
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	reqr, err := http.NewRequestWithContext(probeCtx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return false, 0
	}
	reqr.Header.Set("Content-Type", "application/json")
	if token != "" {
		reqr.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httputil.NewClient(20 * time.Second).Do(reqr)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK, resp.StatusCode
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
