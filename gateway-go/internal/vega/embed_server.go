// embed_server.go — Manages an SGLang embedding server subprocess.
//
// On DGX Spark the gateway auto-launches a dedicated SGLang embedding
// server on port 30001 so no manual setup is required.
package vega

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Default port for the embedding server.
	defaultEmbedPort = 30001
	// Default embedding model — multilingual, good for Korean.
	defaultEmbedModel = "BAAI/bge-m3"
	// How long to wait for the embedding server to become healthy.
	embedServerStartTimeout = 120 * time.Second
	// Poll interval when waiting for the server to start.
	embedServerPollInterval = 2 * time.Second
)

// EmbedServer manages an SGLang embedding server subprocess.
type EmbedServer struct {
	cmd    *exec.Cmd
	model  string
	port   int
	url    string // e.g. "http://127.0.0.1:30001/v1"
	logger *slog.Logger
}

// EmbedServerConfig configures the auto-launched embedding server.
type EmbedServerConfig struct {
	// Model to serve (default: BAAI/bge-m3).
	Model string
	// Port to bind (default: 30001).
	Port int
	Logger *slog.Logger
}

// LaunchEmbedServer starts an SGLang embedding server if one isn't already running.
// Returns the EmbedServer handle (call Stop() on shutdown) and the detected endpoint.
// Returns (nil, endpoint) if a server is already running on the target port.
// Returns (nil, nil) if python/sglang is unavailable.
func LaunchEmbedServer(cfg EmbedServerConfig) (*EmbedServer, *EmbedEndpoint) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Model == "" {
		cfg.Model = resolveEmbedModel()
	}
	if cfg.Port == 0 {
		cfg.Port = defaultEmbedPort
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/v1", cfg.Port)

	// Already running? Just return the endpoint.
	if model := probeEmbedModel(url); model != "" {
		cfg.Logger.Info("embed-server: already running, reusing",
			"url", url, "model", model)
		return nil, &EmbedEndpoint{URL: url, Model: model}
	}

	// Check if python3 + sglang are available.
	if _, err := exec.LookPath("python3"); err != nil {
		cfg.Logger.Info("embed-server: python3 not found, skipping auto-launch")
		return nil, nil
	}

	cfg.Logger.Info("embed-server: launching SGLang embedding server",
		"model", cfg.Model, "port", cfg.Port)

	cmd := exec.Command("python3", "-m", "sglang.launch_server",
		"--model-path", cfg.Model,
		"--is-embedding",
		"--port", fmt.Sprintf("%d", cfg.Port),
		"--mem-fraction-static", "0.10",
	)

	// Log to file so stdout/stderr don't clutter the gateway.
	logPath := embedServerLogPath()
	if logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		cfg.Logger.Info("embed-server: logging to", "path", logPath)
	}

	if err := cmd.Start(); err != nil {
		cfg.Logger.Warn("embed-server: failed to start", "error", err)
		return nil, nil
	}

	es := &EmbedServer{
		cmd:    cmd,
		model:  cfg.Model,
		port:   cfg.Port,
		url:    url,
		logger: cfg.Logger,
	}

	// Wait for it to become healthy in the background.
	// Return the handle immediately so gateway startup isn't blocked.
	go es.waitForReady()

	return es, &EmbedEndpoint{URL: url, Model: cfg.Model}
}

// Stop gracefully shuts down the embedding server.
func (es *EmbedServer) Stop() {
	if es == nil || es.cmd == nil || es.cmd.Process == nil {
		return
	}

	es.logger.Info("embed-server: stopping", "pid", es.cmd.Process.Pid)

	// SIGTERM for graceful shutdown.
	if err := es.cmd.Process.Signal(os.Interrupt); err != nil {
		es.logger.Debug("embed-server: interrupt failed, killing", "error", err)
		es.cmd.Process.Kill()
	}

	// Wait with timeout.
	done := make(chan error, 1)
	go func() { done <- es.cmd.Wait() }()

	select {
	case <-done:
		es.logger.Info("embed-server: stopped")
	case <-time.After(10 * time.Second):
		es.logger.Warn("embed-server: shutdown timed out, killing")
		es.cmd.Process.Kill()
	}
}

// Endpoint returns the embed endpoint for this server.
func (es *EmbedServer) Endpoint() *EmbedEndpoint {
	if es == nil {
		return nil
	}
	return &EmbedEndpoint{URL: es.url, Model: es.model}
}

// waitForReady polls /v1/models until the server responds or times out.
func (es *EmbedServer) waitForReady() {
	ctx, cancel := context.WithTimeout(context.Background(), embedServerStartTimeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			es.logger.Warn("embed-server: start timed out, embedding may not work")
			return
		default:
		}

		if model := probeEmbedModel(es.url); model != "" {
			es.logger.Info("embed-server: ready", "model", model, "url", es.url)
			return
		}

		// Check if process died.
		if es.cmd.ProcessState != nil {
			es.logger.Warn("embed-server: process exited prematurely",
				"exit_code", es.cmd.ProcessState.ExitCode())
			return
		}

		time.Sleep(embedServerPollInterval)
	}
}

// resolveEmbedModel returns the embedding model to use.
// Priority: DENEB_EMBED_MODEL env → default.
func resolveEmbedModel() string {
	if m := os.Getenv("DENEB_EMBED_MODEL"); m != "" {
		return m
	}
	return defaultEmbedModel
}

// embedServerLogPath returns the log file path for the embedding server.
func embedServerLogPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".deneb", "logs")
		os.MkdirAll(dir, 0755)
		return filepath.Join(dir, "sglang-embed.log")
	}
	return "/tmp/deneb-sglang-embed.log"
}

// HasGPU checks if an NVIDIA GPU is available via nvidia-smi.
func HasGPU() bool {
	cmd := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}
