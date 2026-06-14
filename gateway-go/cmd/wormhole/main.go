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
	// Local overrides the loopback/private-IP auto-detection (privacy.go). Set it
	// false to mark an on-box endpoint as cloud (e.g. a local tunnel that egresses)
	// or true for a public URL you trust as local. Nil = auto-detect from URL.
	Local *bool `json:"local,omitempty"`
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
		log.Warn("no token configured — wormhole is OPEN to anyone who can reach it")
	}

	rt := newRouter(cfg, log)

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

	srv := &http.Server{
		Addr:    cfg.Listen,
		Handler: rt.handler(),
		// No WriteTimeout: SSE streams run long; the per-request context handles
		// cancellation when the client disconnects.
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("wormhole listening", "addr", cfg.Listen, "models", len(rt.models))
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
