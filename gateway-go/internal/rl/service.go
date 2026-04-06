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

// Phase represents the RL pipeline lifecycle state.
type Phase string

const (
	PhaseIdle      Phase = "idle"      // no processes running
	PhaseStarting  Phase = "starting"  // launching processes
	PhaseRunning   Phase = "running"   // all processes healthy
	PhaseStopping  Phase = "stopping"  // shutting down
	PhaseFailed    Phase = "failed"    // startup or runtime failure
)

// ProcessInfo tracks a managed subprocess.
type ProcessInfo struct {
	Name    string `json:"name"`
	ProcID  string `json:"procId,omitempty"`
	Port    int    `json:"port,omitempty"`
	Healthy bool   `json:"healthy"`
}

// ServiceStatus is the aggregate pipeline status.
type ServiceStatus struct {
	Phase       Phase           `json:"phase"`
	Processes   []ProcessInfo   `json:"processes,omitempty"`
	Trajectories TrajectoryStats `json:"trajectories"`
	LastAdapter string          `json:"lastAdapter,omitempty"`
	Error       string          `json:"error,omitempty"`
	StartedAt   int64           `json:"startedAt,omitempty"`
}

// Service orchestrates the sglang + Tinker + Atropos process trio.
// It does NOT train — it manages the external Python processes that train.
type Service struct {
	mu        sync.Mutex
	cfg       Config
	phase     Phase
	startedAt int64
	lastError string
	processes map[string]*ProcessInfo
	lastAdapter string

	store     *Store
	collector *Collector
	procMgr   *process.Manager
	cancel    context.CancelFunc
	logger    *slog.Logger
}

// NewService creates an RL service. Does not start any processes.
func NewService(cfg Config, procMgr *process.Manager, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	store := NewStore(cfg.MaxTrajectories)
	collector := NewCollector(store, cfg.Environments, logger)

	return &Service{
		cfg:       cfg,
		phase:     PhaseIdle,
		processes: make(map[string]*ProcessInfo),
		store:     store,
		collector: collector,
		procMgr:   procMgr,
		logger:    logger,
	}
}

// Collector returns the trajectory collector for Hub observer wiring.
func (s *Service) Collector() *Collector { return s.collector }

// Store returns the trajectory store.
func (s *Service) Store() *Store { return s.store }

// Start launches the three-process pipeline in order:
// 1. sglang (inference) -> wait for health
// 2. Tinker (trainer) -> connects to sglang
// 3. Atropos (environment) -> scores trajectories
// ctx should be the server's lifecycle context, not request context.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.phase == PhaseRunning || s.phase == PhaseStarting {
		s.mu.Unlock()
		return fmt.Errorf("rl: already %s", s.phase)
	}
	s.phase = PhaseStarting
	s.lastError = ""
	s.mu.Unlock()

	if s.cfg.BaseModelPath == "" {
		s.setFailed("baseModelPath not configured")
		return fmt.Errorf("rl: baseModelPath not configured")
	}

	runCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

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

	// Wait for sglang health (poll /models endpoint).
	sglangURL := fmt.Sprintf("http://localhost:%d/v1/models", s.cfg.SGLang.Port)
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

	// 3. Start Atropos environment server.
	atroposArgs := []string{
		"-m", "atropos.server",
		"--port", fmt.Sprintf("%d", s.cfg.Atropos.Port),
		"--env", "dispatcher", // multi-task dispatcher environment
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
		s.setFailed(fmt.Sprintf("atropos health: %v", err))
		cancel()
		return err
	}
	s.markHealthy("tinker")
	s.markHealthy("atropos")

	s.mu.Lock()
	s.phase = PhaseRunning
	s.startedAt = time.Now().UnixMilli()
	s.mu.Unlock()

	// Start watchdog: process health + adapter polling in one goroutine.
	go s.watchdog(runCtx)

	s.logger.Info("rl: pipeline started",
		"model", s.cfg.BaseModelPath,
		"sglangPort", s.cfg.SGLang.Port,
		"atroposPort", s.cfg.Atropos.Port,
	)
	return nil
}

// watchdog monitors process health and scans for new LoRA adapters.
func (s *Service) watchdog(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.WatchdogInterval)
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
func (s *Service) checkProcessHealth() {
	s.mu.Lock()
	if s.phase != PhaseRunning {
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
// When found, reloads the adapter on the serving sglang via its HTTP API.
func (s *Service) checkNewAdapter() {
	if s.cfg.AdapterDir == "" {
		return
	}
	entries, err := os.ReadDir(s.cfg.AdapterDir)
	if err != nil {
		return
	}
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

	if newest == "" || time.Since(newestTime) > 10*time.Minute {
		return
	}

	s.mu.Lock()
	if s.lastAdapter == newest {
		s.mu.Unlock()
		return
	}
	s.lastAdapter = newest
	s.mu.Unlock()

	s.logger.Info("rl: new adapter detected, reload pending",
		"path", newest,
		"age", time.Since(newestTime).Round(time.Second),
	)
}

// Stop gracefully shuts down the pipeline in reverse order.
func (s *Service) Stop() error {
	s.mu.Lock()
	if s.phase != PhaseRunning && s.phase != PhaseStarting {
		s.mu.Unlock()
		return nil
	}
	s.phase = PhaseStopping
	cancel := s.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	// Kill in reverse order: atropos -> tinker -> sglang.
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

	// Backup pending trajectories to disk before clearing state.
	if _, _, err := s.store.ExportJSONL(s.cfg.TrajectoryDir, ""); err != nil {
		s.logger.Warn("rl: trajectory backup failed", "error", err)
	}

	s.mu.Lock()
	s.phase = PhaseIdle
	s.processes = make(map[string]*ProcessInfo)
	s.mu.Unlock()

	s.logger.Info("rl: pipeline stopped")
	return nil
}

// Status returns the current pipeline status.
func (s *Service) Status() ServiceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	procs := make([]ProcessInfo, 0, len(s.processes))
	for _, p := range s.processes {
		procs = append(procs, *p)
	}
	return ServiceStatus{
		Phase:        s.phase,
		Processes:    procs,
		Trajectories: s.store.Stats(),
		LastAdapter:  s.lastAdapter,
		Error:        s.lastError,
		StartedAt:    s.startedAt,
	}
}

// --- internal helpers ---

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
	s.phase = PhaseFailed
	s.lastError = msg
	s.logger.Warn("rl: failed", "error", msg)
}

func (s *Service) pythonBin() string {
	if s.cfg.VenvDir != "" {
		venvPython := filepath.Join(s.cfg.VenvDir, "bin", "python3")
		if _, err := os.Stat(venvPython); err == nil {
			return venvPython
		}
	}
	return "python3"
}

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
