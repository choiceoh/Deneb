package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// lookup resolves a client-facing model name to its backend. Configured models
// win over SparkFleet-discovered ones: an explicit config entry is an operator
// override (e.g. to pin a key, protocol, or upstream id) and must beat discovery.
func (rt *router) lookup(name string) (modelEntry, bool) {
	if e, ok := rt.cur().models[name]; ok {
		if e.Fleet {
			return rt.resolveFleetEntry(e)
		}
		return e, true
	}
	if f := rt.fleet.Load(); f != nil {
		if e, ok := (*f)[name]; ok {
			return e, true
		}
	}
	return modelEntry{}, false
}

// resolveFleetEntry overlays the live SparkFleet-discovered URL onto a fleet-backed
// explicit entry (Fleet:true), preserving the entry's own routing config
// (toggleKwarg, protocol, key, upstreamModel) that bare discovery omits. The
// discovered set is keyed by served model id, which is the entry's UpstreamModel
// (loadConfig defaults it to Name). When no live backend serves the model, fall
// back to the entry's static url if present; otherwise the entry is unroutable so
// the caller 404s / auto-fallback takes over — a moved or stopped model is never
// pinned to a dead node. lookup returns entries by value, so overlaying URL here
// never mutates the stored config.
func (rt *router) resolveFleetEntry(e modelEntry) (modelEntry, bool) {
	if f := rt.fleet.Load(); f != nil {
		if d, ok := (*f)[e.UpstreamModel]; ok {
			e.URL = d.URL
			return e, true
		}
	}
	if strings.TrimSpace(e.URL) != "" {
		return e, true // static fallback while no live backend is discovered
	}
	return modelEntry{}, false
}

// mergedModels returns the full routable set for display/listing — configured
// models (in config order) followed by discovered ones not shadowed by a config
// entry of the same name. Not used on the hot path (that's lookup); only by
// listModels.
func (rt *router) mergedModels() []modelEntry {
	s := rt.cur()
	out := make([]modelEntry, 0, len(s.cfg.Models))
	out = append(out, s.cfg.Models...)
	if f := rt.fleet.Load(); f != nil {
		for name, e := range *f {
			if _, shadowed := s.models[name]; !shadowed {
				out = append(out, e)
			}
		}
	}
	return out
}

// watch re-reads the config file when its mtime advances (so management toggles
// apply live) and re-polls SparkFleet for discovered models. It exits when ctx is
// cancelled.
func (rt *router) watch(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			rt.log.Error("config watcher panic", "panic", r)
		}
	}()
	// Discover once up front so fleet models are routable, and their windows
	// known, as soon as possible. The window probe runs even for a fully static
	// config (it has local models whose max_model_len downstream wants).
	rt.refreshFleet(ctx)
	rt.refreshWindows(ctx)
	rt.refreshKeyHealth(ctx) // seed cloud-key health at startup (even for a static config)
	if rt.path == "" && rt.cur().cfg.Sparkfleet == nil {
		return // nothing to poll: static config, no discovery (windows + key health probed once above)
	}
	cfgTick := time.NewTicker(3 * time.Second)
	defer cfgTick.Stop()
	fleetTick := time.NewTicker(fleetRefreshInterval)
	defer fleetTick.Stop()
	windowTick := time.NewTicker(windowRefreshInterval)
	defer windowTick.Stop()
	keyHealthTick := time.NewTicker(keyHealthRefreshInterval)
	defer keyHealthTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-cfgTick.C:
			rt.reloadIfChanged()
			rt.usage.maybeFlush()
		case <-fleetTick.C:
			rt.refreshFleet(ctx)
		case <-windowTick.C:
			rt.refreshWindows(ctx)
		case <-keyHealthTick.C:
			rt.refreshKeyHealth(ctx)
		}
	}
}

// refreshWindows probes every LOCAL routable model's backend /v1/models for its
// max_model_len and swaps in a fresh map (keyed by client-facing model name).
// Cloud and anthropic models are skipped — max_model_len is a vLLM serving fact.
// Best-effort: a model whose probe fails just has no window this cycle (the map
// is rebuilt each pass, so a recovered backend repopulates). Sole writer, so the
// atomic swap is the only synchronization needed.
func (rt *router) refreshWindows(parent context.Context) {
	next := map[string]int{}
	for _, m := range rt.mergedModels() {
		e, ok := rt.lookup(m.Name) // resolve fleet-backed entries to a live URL
		if !ok || e.URL == "" || !e.isLocal() || e.protocol() != protocolOpenAI {
			continue
		}
		ctx, cancel := context.WithTimeout(parent, windowProbeTimeout)
		if w := probeMaxModelLen(ctx, rt.client, e); w > 0 {
			next[m.Name] = w
		}
		cancel()
	}
	rt.windows.Store(&next)
}

// probeMaxModelLen GETs a backend's /v1/models and returns the max_model_len for
// the entry's served model id (UpstreamModel, or Name), or 0 if the backend is
// unreachable, returns non-200, isn't JSON, or doesn't report the field.
func probeMaxModelLen(ctx context.Context, client *http.Client, e modelEntry) int {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(e.URL, "/")+"/models", nil)
	if err != nil {
		return 0
	}
	if e.Key != "" {
		req.Header.Set("Authorization", "Bearer "+e.Key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var out struct {
		Data []struct {
			ID          string `json:"id"`
			MaxModelLen int    `json:"max_model_len"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0
	}
	want := e.UpstreamModel
	if want == "" {
		want = e.Name
	}
	for _, m := range out.Data {
		if m.ID == want {
			return m.MaxModelLen
		}
	}
	return 0
}

// refreshFleet re-polls SparkFleet and swaps in the freshly discovered model set.
// On a transient discovery error it KEEPS the last-known set — a single failed
// poll shouldn't drop every fleet route mid-flight (a stale entry just 502s and
// auto-fallback handles it). When the source is removed (hot-reload) it clears.
func (rt *router) refreshFleet(parent context.Context) {
	src := rt.cur().cfg.Sparkfleet
	if src == nil || src.URL == "" {
		rt.clearFleet()
		return
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	entries, err := discoverFleet(ctx, rt.client, *src)
	if err != nil {
		if rt.fleetState != "down" { // log the failure once, not every poll
			rt.log.Warn("sparkfleet discovery failing, keeping last known", "url", src.URL, "error", err)
			rt.fleetState = "down"
		}
		return
	}
	m := make(map[string]modelEntry, len(entries))
	for _, e := range entries {
		m[e.Name] = e
	}
	rt.fleet.Store(&m)
	if st := fmt.Sprintf("up:%d", len(m)); st != rt.fleetState { // log only on change
		rt.log.Info("sparkfleet discovery", "models", len(m))
		rt.fleetState = st
	}
}

// clearFleet drops all discovered models (the source was removed via hot-reload).
func (rt *router) clearFleet() {
	if f := rt.fleet.Load(); f == nil || len(*f) == 0 {
		return
	}
	empty := map[string]modelEntry{}
	rt.fleet.Store(&empty)
}

// reloadIfChanged re-reads the config file and swaps in a fresh snapshot when the
// file's mtime has advanced past the loaded one. Returns true if it reloaded. A
// parse error keeps the current snapshot (a half-written file never wedges us).
func (rt *router) reloadIfChanged() bool {
	st, err := os.Stat(rt.path)
	if err != nil {
		return false
	}
	cfgChanged := st.ModTime().After(rt.cur().mtime)
	secChanged := false
	if m := secretsMtimeNanos(rt.secretsPath); m != rt.secretsMtime {
		rt.secretsMtime = m
		secChanged = true
	}
	if !cfgChanged && !secChanged {
		return false
	}
	if secChanged {
		// Re-read the secrets file into the process env so the ${VAR} re-expansion
		// below picks up a rotated key — no service restart needed.
		if n, err := loadSecretsEnv(rt.secretsPath); err != nil {
			rt.log.Warn("secrets reload failed, keeping current env", "error", err)
		} else {
			rt.log.Info("secrets reloaded", "keys", n)
		}
	}
	nc, err := loadConfig(rt.path) // re-expands ${VAR} against the current process env
	if err != nil {
		rt.log.Warn("config reload failed, keeping current", "error", err)
		return false
	}
	// The HTTP listener is fixed for the lifetime of the process. Preserve the
	// actual bound address in the live snapshot (and /status), then reject a
	// token removal that would expose that listener. This also covers a missing
	// env-backed token during secrets/config rotation.
	nc.Listen = rt.boundListen
	if err := validateInboundAuth(nc.Listen, nc.Token); err != nil {
		rt.log.Error("config reload rejected, keeping current", "error", err)
		return false
	}
	rt.snap.Store(buildSnapshot(nc, st.ModTime()))
	if cfgChanged {
		rt.log.Info("config reloaded", "models", len(nc.Models))
	}
	logConfigWarnings(rt.log, nc) // surface a bad edit at reload, not on first request
	return true
}
