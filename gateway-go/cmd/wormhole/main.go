// Command wormhole is a thin OpenAI-compatible router — the "wormhole api" — that
// fans one /v1 endpoint out to many model backends (local vLLM and cloud
// providers alike), picked by the requested model name. External clients (Claude
// Code, scripts, future apps) see one URL and one token; the upstream provider
// keys stay here.
//
// First slice: PURE pass-through for OpenAI-compatible upstreams (local vLLM,
// OpenRouter, Kimi, MiMo, …). It rewrites the upstream URL, auth, and (optionally)
// the model id, then streams the response straight back — so streaming, tool
// calls, and every OpenAI parameter ride through untouched. Native Anthropic-API
// translation is a planned fast-follow that will reuse internal/ai/llm's hardened
// Anthropic client; until then, reach Claude via an OpenAI-compatible front
// (OpenRouter).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// modelEntry maps a client-facing model name onto an upstream OpenAI-compatible
// backend. UpstreamModel rewrites the model id when forwarding (so "claude" can
// map to "anthropic/claude-opus-4" on OpenRouter); empty means "same as Name".
type modelEntry struct {
	Name          string `json:"name"`
	URL           string `json:"url"` // upstream OpenAI base, e.g. http://127.0.0.1:8000/v1
	Key           string `json:"key,omitempty"`
	UpstreamModel string `json:"upstreamModel,omitempty"`
	// Protocol is the backend's wire API: "openai" (default, reached via
	// /v1/chat/completions) or "anthropic" (reached via /v1/messages). wormhole
	// forwards the matching protocol straight through — no cross-translation.
	Protocol string `json:"protocol,omitempty"`
	// ToggleKwarg is the vLLM chat_template_kwargs boolean that disables this
	// model's thinking phase ("thinking" for DeepSeek V4, "enable_thinking" for
	// Qwen3). Set it to enable effort-based thinking routing: wormhole turns
	// thinking OFF for obviously-simple turns. Empty = no routing (pass through).
	ToggleKwarg string `json:"toggleKwarg,omitempty"`
	// Reasoning selects a cloud model's NATIVE reasoning dialect for effort
	// routing — for backends that don't use vLLM's chat_template_kwargs toggle.
	// Unlike ToggleKwarg, this runs even under X-Wormhole-No-Effort, because the
	// Deneb gateway can't express these params itself (so wormhole is the only
	// place the translation can happen — there's no double-routing to avoid).
	// Currently one style:
	//   "glm" — z.ai / GLM-5.x. Per-turn, like dsv4: an obviously-simple turn →
	//   thinking:{"type":"disabled"} (off); otherwise reasoning_effort:"high"
	//   (on). GLM honors only reasoning_effort high|max and resolves anything but
	//   an explicit "high" to MAX, so on-mode pins "high" rather than leaking max.
	Reasoning string `json:"reasoning,omitempty"`
	// Local overrides the loopback/private-IP auto-detection (privacy.go). Set it
	// false to mark an on-box endpoint as cloud (e.g. a local tunnel that egresses)
	// or true for a public URL you trust as local. Nil = auto-detect from URL.
	Local *bool `json:"local,omitempty"`
	// Fleet makes this an explicit entry whose backend URL is resolved LIVE from
	// SparkFleet discovery (keyed by UpstreamModel/Name) instead of a static url —
	// so the model can move between nodes without editing this file, while the entry
	// still carries its own routing config (toggleKwarg, protocol, key, upstreamModel)
	// that bare discovery can't express. Requires a top-level "sparkfleet" source.
	// A static url, if also set, is the fallback used while no live backend serves
	// the model; with neither a live backend nor a static url the entry is unroutable
	// (clean 404 / auto-fallback) — never a stale pin to a dead node.
	Fleet bool `json:"fleet,omitempty"`
}

// config is the wormhole config file (default ~/.wormhole/config.json). Token and
// each model Key support ${ENV} expansion so secrets live in the environment, not
// the file.
type config struct {
	Listen string       `json:"listen,omitempty"`
	Token  string       `json:"token,omitempty"`
	Models []modelEntry `json:"models"`
	// LocalOnly air-gaps this wormhole: every cloud-backed model is refused, so a
	// routing slip can't egress private data. Per-request, a sensitive caller can
	// force the same with the X-Wormhole-Local-Only header.
	LocalOnly bool `json:"localOnly,omitempty"`
	// Auto is the ordered candidate list for the reserved "auto" model name —
	// clients that send model:"auto" get the first candidate that connects (local
	// first), with fallback down the list. Empty disables auto-routing.
	Auto []string `json:"auto,omitempty"`
	// AutoName overrides the reserved auto model name (default "auto").
	AutoName string `json:"autoName,omitempty"`
	// EffortRouting gates the thinking/non-thinking routing globally. Nil (absent)
	// means ON — a model with a toggleKwarg routes by effort. Set false to turn
	// the whole feature off without editing every model. Pointer so absent ≠ off.
	EffortRouting *bool `json:"effortRouting,omitempty"`
	// Sparkfleet, when set, auto-discovers local vLLM backends from a SparkFleet
	// control-plane (its GET /api/services): each live model becomes a routable
	// entry, refreshed periodically, so launching a model in SparkFleet makes it
	// reachable here without editing this file. Configured Models win on a name
	// clash (an explicit entry overrides discovery). Nil = no discovery. See
	// fleet.go.
	Sparkfleet *fleetSource `json:"sparkfleet,omitempty"`
}

// effortRoutingOn reports whether thinking routing is active (default on; an
// explicit "effortRouting": false turns it off globally).
func (c config) effortRoutingOn() bool {
	return c.EffortRouting == nil || *c.EffortRouting
}

func loadConfig(path string) (config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return config{}, err
	}
	var cfg config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.Token = os.ExpandEnv(cfg.Token)
	for i := range cfg.Models {
		cfg.Models[i].Key = os.ExpandEnv(cfg.Models[i].Key)
		if cfg.Models[i].UpstreamModel == "" {
			cfg.Models[i].UpstreamModel = cfg.Models[i].Name
		}
	}
	if cfg.Sparkfleet != nil {
		cfg.Sparkfleet.Token = os.ExpandEnv(cfg.Sparkfleet.Token)
	}
	return cfg, nil
}

func defaultConfigPath() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h + "/.wormhole/config.json"
	}
	return "wormhole.json"
}

func main() {
	var configPath, listen string
	flag.StringVar(&configPath, "config", defaultConfigPath(), "path to the wormhole config JSON")
	flag.StringVar(&listen, "listen", "", "override listen address (e.g. :18800)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Load wormhole's own secrets file (sibling of the config) into the process env
	// BEFORE loadConfig expands ${VAR} refs. Absent file = no-op, so env-only /
	// systemd-EnvironmentFiles deploys are unchanged. See secrets.go.
	secretsPath := secretsFileFor(configPath)
	if n, err := loadSecretsEnv(secretsPath); err != nil {
		log.Warn("secrets file load failed", "path", secretsPath, "error", err)
	} else if n > 0 {
		log.Info("loaded wormhole secrets", "keys", n, "path", secretsPath)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Error("config load failed", "path", configPath, "error", err)
		os.Exit(1)
	}
	if listen != "" {
		cfg.Listen = listen
	}
	if cfg.Listen == "" {
		cfg.Listen = ":18800"
	}
	if cfg.Token == "" {
		if isLoopbackListen(cfg.Listen) {
			log.Warn("no token configured — wormhole is open, but only on loopback", "listen", cfg.Listen)
		} else {
			// Bound to a routable address with no auth: anyone on the network (e.g.
			// the tailnet) can reach every model, including cloud egress. Set a token.
			log.Error("INSECURE: wormhole is bound to a non-loopback address WITHOUT a token — it is OPEN to the network; set a token", "listen", cfg.Listen)
		}
	}
	logConfigWarnings(log, cfg) // catch misconfig (e.g. anthropic url missing /v1) at boot

	rt := newRouter(cfg, configPath, log)

	// Egress visibility: name every model whose data leaves the box, so the
	// operator sees the cloud surface at a glance (local-first hygiene).
	var cloud []string
	for _, e := range cfg.Models {
		if !e.isLocal() {
			cloud = append(cloud, e.Name)
		}
	}
	if cfg.LocalOnly {
		log.Info("local-only mode: cloud-backed models are refused")
	} else if len(cloud) > 0 {
		log.Warn("cloud egress models — requests to these leave this box", "models", cloud)
	}
	if cfg.Sparkfleet != nil && cfg.Sparkfleet.URL != "" {
		log.Info("sparkfleet discovery enabled", "url", cfg.Sparkfleet.URL)
	}

	srv := &http.Server{
		Addr:    cfg.Listen,
		Handler: rt.handler(),
		// No WriteTimeout: SSE streams run long; the per-request context handles
		// cancellation when the client disconnects.
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go rt.watch(ctx) // hot-reload the config file so management toggles apply live

	go func() {
		log.Info("wormhole listening", "addr", cfg.Listen, "models", len(rt.cur().models))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}
