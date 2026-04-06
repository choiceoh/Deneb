package rl

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/process"
)

// ServiceState represents the lifecycle state of the RL pipeline.
type ServiceState string

const (
	StateIdle     ServiceState = "idle"
	StateStarting ServiceState = "starting"
	StateRunning  ServiceState = "running"
	StateStopping ServiceState = "stopping"
	StateFailed   ServiceState = "failed"
)

// ProcessInfo tracks a managed subprocess.
type ProcessInfo struct {
	Name    string `json:"name"`
	ProcID  string `json:"procId,omitempty"`
	Port    int    `json:"port"`
	Healthy bool   `json:"healthy"`
}

// ServiceStatus is the aggregate status of the RL pipeline.
type ServiceStatus struct {
	State     ServiceState  `json:"state"`
	Processes []ProcessInfo `json:"processes,omitempty"`
	Error     string        `json:"error,omitempty"`
	StartedAt int64         `json:"startedAt,omitempty"`
}

// Service orchestrates the sglang + Tinker + Atropos process trio.
// It does NOT train — it manages the external Python processes that train.
type Service struct {
	mu        sync.Mutex
	cfg       Config
	state     ServiceState
	startedAt int64
	lastError string
	processes map[string]*ProcessInfo
	procMgr   *process.Manager
	cancel    context.CancelFunc
	store     *TrajectoryStore
	logger    *slog.Logger

	// latestAdapter is the path to the most recently detected LoRA adapter.
	// Updated by watchdog; read by callers that load the adapter into inference.
	latestAdapter string
}

// NewService creates an RL service. Does not start any processes.
func NewService(cfg Config, procMgr *process.Manager, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg:       cfg,
		state:     StateIdle,
		processes: make(map[string]*ProcessInfo),
		procMgr:   procMgr,
		store:     NewTrajectoryStore(),
		logger:    logger,
	}
}

// Start launches the three-process pipeline in order:
// 1. sglang (inference) → wait for health
// 2. Tinker (trainer) → connects to sglang
// 3. Atropos (environment) → feeds trajectories to Tinker
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.state == StateRunning || s.state == StateStarting {
		s.mu.Unlock()
		return fmt.Errorf("rl: already %s", s.state)
	}
	s.state = StateStarting
	s.lastError = ""
	s.mu.Unlock()

	runCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	// Validate config.
	if s.cfg.BaseModelPath == "" {
		s.setFailed("baseModelPath not configured")
		cancel()
		return fmt.Errorf("rl: baseModelPath not configured")
	}

	pythonBin := s.pythonBin()

	// 1. Start sglang inference server.
	sglangArgs := []string{
		"-m", "sglang.launch_server",
		"--model-path", s.cfg.BaseModelPath,
		"--port", fmt.Sprintf("%d", s.cfg.SGLang.Port),
		"--mem-fraction-static", fmt.Sprintf("%.2f", s.cfg.SGLang.GPUMemFrac),
	}
	if s.cfg.SGLang.TPSize > 1 {
		sglangArgs = append(sglangArgs, "--tp-size", fmt.Sprintf("%d", s.cfg.SGLang.TPSize))
	}

	sglangID, err := s.startProcess(runCtx, "sglang", pythonBin, sglangArgs)
	if err != nil {
		s.setFailed(fmt.Sprintf("sglang start: %v", err))
		cancel()
		return err
	}
	s.setProcess("sglang", sglangID, s.cfg.SGLang.Port)

	// Wait for sglang health.
	sglangURL := fmt.Sprintf("http://localhost:%d/health", s.cfg.SGLang.Port)
	if err := s.waitForHealth(runCtx, sglangURL, 120*time.Second); err != nil {
		s.setFailed(fmt.Sprintf("sglang health: %v", err))
		cancel()
		return err
	}
	s.markHealthy("sglang")

	// 2. Start Tinker trainer.
	tinkerArgs := []string{
		"-m", "tinker.train",
		"--sglang-url", fmt.Sprintf("http://localhost:%d", s.cfg.SGLang.Port),
		"--lora-rank", fmt.Sprintf("%d", s.cfg.Tinker.LoraRank),
		"--learning-rate", fmt.Sprintf("%g", s.cfg.Tinker.LearningRate),
		"--batch-size", fmt.Sprintf("%d", s.cfg.Tinker.BatchSize),
		"--output-dir", s.cfg.AdapterDir,
	}

	tinkerID, err := s.startProcess(runCtx, "tinker", pythonBin, tinkerArgs)
	if err != nil {
		s.setFailed(fmt.Sprintf("tinker start: %v", err))
		cancel()
		return err
	}
	s.setProcess("tinker", tinkerID, 0)
	// Tinker has no HTTP health endpoint; process start is the best we can verify.
	// The watchdog will detect if it dies later.

	// 3. Start Atropos environment server.
	atroposArgs := []string{
		"-m", "atropos.server",
		"--port", fmt.Sprintf("%d", s.cfg.Atropos.Port),
		"--env", "korean_quality",
	}

	atroposID, err := s.startProcess(runCtx, "atropos", pythonBin, atroposArgs)
	if err != nil {
		s.setFailed(fmt.Sprintf("atropos start: %v", err))
		cancel()
		return err
	}
	s.setProcess("atropos", atroposID, s.cfg.Atropos.Port)

	// Wait for Atropos health.
	atroposURL := fmt.Sprintf("http://localhost:%d/health", s.cfg.Atropos.Port)
	if err := s.waitForHealth(runCtx, atroposURL, 30*time.Second); err != nil {
		s.logger.Warn("rl: atropos health check failed, continuing", "error", err)
		// Non-fatal: Atropos may not have a /health endpoint in all versions.
	} else {
		s.markHealthy("atropos")
	}

	s.mu.Lock()
	s.state = StateRunning
	s.startedAt = time.Now().UnixMilli()
	s.mu.Unlock()

	// Start unified watchdog: monitors process health + polls for new adapters.
	go s.watchdog(runCtx)

	s.logger.Info("rl: pipeline started",
		"model", s.cfg.BaseModelPath,
		"sglangPort", s.cfg.SGLang.Port,
		"atroposPort", s.cfg.Atropos.Port,
	)
	return nil
}

// watchdog runs a single goroutine that handles both process health monitoring
// and LoRA adapter hot-swap polling. Merging into one goroutine (per user feedback)
// avoids extra goroutines and simplifies lifecycle.
func (s *Service) watchdog(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkProcessHealth()
			s.checkNewAdapter()
		}
	}
}

// checkProcessHealth verifies each managed process is still alive.
// If a process died, logs the failure. Auto-restart is deferred to
// process.Manager's built-in retry logic if available.
func (s *Service) checkProcessHealth() {
	s.mu.Lock()
	if s.state != StateRunning {
		s.mu.Unlock()
		return
	}
	procs := make(map[string]*ProcessInfo, len(s.processes))
	for k, v := range s.processes {
		cp := *v
		procs[k] = &cp
	}
	s.mu.Unlock()

	for name, info := range procs {
		if info.ProcID == "" || s.procMgr == nil {
			continue
		}
		proc := s.procMgr.Get(info.ProcID)
		if proc == nil {
			s.logger.Warn("rl: process disappeared", "name", name, "id", info.ProcID)
			s.mu.Lock()
			if p, ok := s.processes[name]; ok {
				p.Healthy = false
			}
			s.mu.Unlock()
		}
	}
}

// checkNewAdapter scans the adapter directory for newly trained LoRA weights.
// When a new adapter is found, it stores the path for the Rust inference
// pipeline to pick up on next request (lazy reload via deneb_ml_lora_load).
func (s *Service) checkNewAdapter() {
	if s.cfg.AdapterDir == "" {
		return
	}
	entries, err := os.ReadDir(s.cfg.AdapterDir)
	if err != nil {
		return
	}
	// Find the most recently modified .gguf file.
	var newest string
	var newestTime time.Time
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".gguf" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
			newest = filepath.Join(s.cfg.AdapterDir, e.Name())
		}
	}
	if newest == "" {
		return
	}
	s.mu.Lock()
	changed := newest != s.latestAdapter
	if changed {
		s.latestAdapter = newest
	}
	s.mu.Unlock()

	if changed {
		s.logger.Info("rl: new adapter detected — call LatestAdapter() to load",
			"path", newest, "age", time.Since(newestTime).Round(time.Second))
	}
}

// LatestAdapter returns the path to the most recently detected LoRA adapter.
// Returns "" if no adapter has been found. Callers should pass this to
// deneb_ml_lora_load FFI to apply the adapter to inference.
func (s *Service) LatestAdapter() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latestAdapter
}

// Stop gracefully shuts down the pipeline in reverse order.
func (s *Service) Stop() error {
	s.mu.Lock()
	if s.state != StateRunning && s.state != StateStarting {
		s.mu.Unlock()
		return nil
	}
	s.state = StateStopping
	cancel := s.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	// Kill in reverse order: atropos → tinker → sglang.
	for _, name := range []string{"atropos", "tinker", "sglang"} {
		s.mu.Lock()
		info := s.processes[name]
		s.mu.Unlock()
		if info != nil && info.ProcID != "" && s.procMgr != nil {
			if err := s.procMgr.Kill(info.ProcID); err != nil {
				s.logger.Warn("rl: kill failed", "process", name, "error", err)
			}
		}
	}

	s.mu.Lock()
	s.state = StateIdle
	s.processes = make(map[string]*ProcessInfo)
	s.mu.Unlock()

	s.logger.Info("rl: pipeline stopped")
	return nil
}

// Store returns the trajectory store for this service.
func (s *Service) Store() *TrajectoryStore {
	return s.store
}

// Status returns the current pipeline status.
func (s *Service) Status() *ServiceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	procs := make([]ProcessInfo, 0, len(s.processes))
	for _, p := range s.processes {
		procs = append(procs, *p)
	}
	return &ServiceStatus{
		State:     s.state,
		Processes: procs,
		Error:     s.lastError,
		StartedAt: s.startedAt,
	}
}

// startProcess launches a Python subprocess via process.Manager.
func (s *Service) startProcess(ctx context.Context, name, pythonBin string, args []string) (string, error) {
	if s.procMgr == nil {
		return "", fmt.Errorf("rl: process manager unavailable")
	}

	reqID := fmt.Sprintf("rl-%s", name)
	procID := s.procMgr.ExecuteBackground(ctx, process.ExecRequest{
		ID:         reqID,
		Command:    pythonBin,
		Args:       args,
		WorkingDir: s.resolveWorkdir(),
		Env:        s.buildEnvMap(),
	})

	s.logger.Info("rl: process started", "name", name, "id", procID)
	return procID, nil
}

func (s *Service) setProcess(name, procID string, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processes[name] = &ProcessInfo{
		Name:   name,
		ProcID: procID,
		Port:   port,
	}
}

func (s *Service) markHealthy(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.processes[name]; ok {
		p.Healthy = true
	}
}

func (s *Service) setFailed(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateFailed
	s.lastError = msg
	s.logger.Warn("rl: failed", "error", msg)
}

// waitForHealth polls a URL until it returns 200 or the timeout expires.
func (s *Service) waitForHealth(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	client := &http.Client{Timeout: 3 * time.Second}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("health check timeout after %v", timeout)
		case <-ticker.C:
			resp, err := client.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}

// pythonBin returns the Python binary from the configured venv, or system python3.
func (s *Service) pythonBin() string {
	if s.cfg.VenvDir != "" {
		venvPython := filepath.Join(s.cfg.VenvDir, "bin", "python3")
		if _, err := os.Stat(venvPython); err == nil {
			return venvPython
		}
	}
	return "python3"
}

// buildEnvMap returns environment variable overrides for the Python subprocesses.
func (s *Service) buildEnvMap() map[string]string {
	env := make(map[string]string)
	if s.cfg.VenvDir != "" {
		venvBin := filepath.Join(s.cfg.VenvDir, "bin")
		env["VIRTUAL_ENV"] = s.cfg.VenvDir
		env["PATH"] = venvBin + ":" + os.Getenv("PATH")
	}
	return env
}

func (s *Service) resolveWorkdir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".deneb", "rl")
	}
	return ""
}
