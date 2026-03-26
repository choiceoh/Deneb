// Package system provides RPC method handlers for the system domain:
// identity, monitoring, doctor, maintenance, update, usage, and logs.
//
// It exposes *Methods functions that return handler maps for bulk-registration
// on the rpc.Dispatcher.
package system

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/maintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/usage"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
)

// BroadcastFunc is the signature for broadcasting events to connected clients.
type BroadcastFunc func(event string, payload any) (int, []error)

// ---------------------------------------------------------------------------
// Identity
// ---------------------------------------------------------------------------

// IdentityMethods returns the gateway.identity.get handler.
func IdentityMethods(version string) map[string]rpcutil.HandlerFunc {
	// Pre-compute static identity fields at registration time.
	hostname, _ := os.Hostname()
	stateDir := config.ResolveStateDir()

	return map[string]rpcutil.HandlerFunc{
		"gateway.identity.get": func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			resp := protocol.MustResponseOK(req.ID, map[string]any{
				"hostname": hostname,
				"version":  version,
				"runtime":  "go",
				"os":       runtime.GOOS,
				"arch":     runtime.GOARCH,
				"stateDir": stateDir,
			})
			return resp
		},
	}
}

// ---------------------------------------------------------------------------
// Monitoring
// ---------------------------------------------------------------------------

// MonitoringDeps holds the dependencies for monitoring RPC methods.
type MonitoringDeps struct {
	ChannelHealth *monitoring.ChannelHealthMonitor
	Activity      *monitoring.ActivityTracker
}

// MonitoringMethods returns the monitoring.channel_health and
// monitoring.activity handlers.
func MonitoringMethods(deps MonitoringDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"monitoring.channel_health": func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			if deps.ChannelHealth == nil {
				resp := protocol.MustResponseOK(req.ID, map[string]any{"channels": []any{}})
				return resp
			}
			snapshot := deps.ChannelHealth.HealthSnapshot()
			resp := protocol.MustResponseOK(req.ID, map[string]any{"channels": snapshot})
			return resp
		},

		"monitoring.activity": func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			if deps.Activity == nil {
				resp := protocol.MustResponseOK(req.ID, map[string]any{"lastActivityMs": 0})
				return resp
			}
			resp := protocol.MustResponseOK(req.ID, map[string]any{
				"lastActivityMs": deps.Activity.LastActivityAt(),
			})
			return resp
		},
	}
}

// ---------------------------------------------------------------------------
// Doctor
// ---------------------------------------------------------------------------

// DoctorDeps holds dependencies for doctor RPC methods.
type DoctorDeps struct {
	// DefaultAgentID is the default agent identifier from config.
	DefaultAgentID string
	// EmbeddingProvider is the name of the configured embedding provider.
	EmbeddingProvider string
}

// DoctorMethods returns the doctor.memory.status handler.
func DoctorMethods(deps DoctorDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"doctor.memory.status": doctorMemoryStatus(deps),
	}
}

func doctorMemoryStatus(deps DoctorDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		// Collect Go runtime memory stats.
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		// Read system memory from /proc/meminfo (Linux).
		sysMemTotal, sysMemAvail := readProcMeminfo()

		embeddingOK := deps.EmbeddingProvider != ""

		result := map[string]any{
			"agentId":  deps.DefaultAgentID,
			"provider": deps.EmbeddingProvider,
			"embedding": map[string]any{
				"ok": embeddingOK,
			},
			"system": map[string]any{
				"totalMB":     sysMemTotal / (1024 * 1024),
				"availableMB": sysMemAvail / (1024 * 1024),
			},
			"runtime": map[string]any{
				"allocMB":    memStats.Alloc / (1024 * 1024),
				"sysAllocMB": memStats.Sys / (1024 * 1024),
				"numGC":      memStats.NumGC,
			},
		}

		if !embeddingOK {
			result["embedding"] = map[string]any{
				"ok":    false,
				"error": "no embedding provider configured",
			}
		}

		resp, _ := protocol.NewResponseOK(req.ID, result)
		return resp
	}
}

// readProcMeminfo reads total and available memory from /proc/meminfo.
// Returns (0, 0) on non-Linux or if reading fails.
func readProcMeminfo() (total, available uint64) {
	if runtime.GOOS != "linux" {
		return 0, 0
	}

	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d kB", &total)
			total *= 1024 // Convert to bytes.
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fmt.Sscanf(line, "MemAvailable: %d kB", &available)
			available *= 1024
		}
		if total > 0 && available > 0 {
			break
		}
	}
	return total, available
}

// ---------------------------------------------------------------------------
// Maintenance
// ---------------------------------------------------------------------------

// MaintenanceDeps holds dependencies for maintenance RPC methods.
type MaintenanceDeps struct {
	Runner *maintenance.Runner
}

// MaintenanceMethods returns the maintenance.run, maintenance.status,
// and maintenance.summary handlers.
func MaintenanceMethods(deps MaintenanceDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"maintenance.run":     maintenanceRun(deps),
		"maintenance.status":  maintenanceStatus(deps),
		"maintenance.summary": maintenanceSummary(deps),
	}
}

func maintenanceRun(deps MaintenanceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Runner == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "maintenance runner not available"))
		}

		var p struct {
			DryRun bool `json:"dryRun"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}

		report := deps.Runner.Run(p.DryRun)
		resp, _ := protocol.NewResponseOK(req.ID, report)
		return resp
	}
}

func maintenanceStatus(deps MaintenanceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Runner == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"hasReport": false})
			return resp
		}

		report := deps.Runner.LastReport()
		if report == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"hasReport": false})
			return resp
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"hasReport": true,
			"report":    report,
			"summary":   maintenance.SummarizeReport(report),
		})
		return resp
	}
}

func maintenanceSummary(deps MaintenanceDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Runner == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "maintenance runner not available"))
		}

		report := deps.Runner.LastReport()
		if report == nil {
			// No previous report -- trigger a dry-run.
			report = deps.Runner.Run(true)
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"summary": maintenance.SummarizeReport(report),
			"report":  report,
		})
		return resp
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

// UpdateDeps holds dependencies for the update.run RPC method.
type UpdateDeps struct {
	// RepoDir is the repository root directory for git operations.
	// Defaults to the current working directory if empty.
	RepoDir string
	// DenebDir is the deneb config directory (~/.deneb) for sentinel file.
	DenebDir string
}

// UpdateMethods returns the update.run handler.
func UpdateMethods(deps UpdateDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"update.run": updateRun(deps),
	}
}

// updateStep records a single step in the update process.
type updateStep struct {
	Name       string `json:"name"`
	Command    string `json:"command"`
	Cwd        string `json:"cwd,omitempty"`
	DurationMs int64  `json:"durationMs"`
	Log        string `json:"log,omitempty"`
	OK         bool   `json:"ok"`
}

func updateRun(deps UpdateDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			TimeoutMs      int64  `json:"timeoutMs"`
			RestartDelayMs int64  `json:"restartDelayMs"`
			Note           string `json:"note"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}

		if p.TimeoutMs <= 0 {
			p.TimeoutMs = 120_000
		}
		timeout := time.Duration(p.TimeoutMs) * time.Millisecond
		updateCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		repoDir := deps.RepoDir
		if repoDir == "" {
			var err error
			repoDir, err = os.Getwd()
			if err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrUnavailable, "cannot determine working directory: "+err.Error()))
			}
		}

		startTime := time.Now()
		var steps []updateStep

		// Step 1: Get current git SHA (before).
		beforeSHA := runGitRev(repoDir)

		// Step 2: git pull --rebase origin main.
		pullStep := runStep(updateCtx, repoDir, "git pull", "git", "pull", "--rebase", "origin", "main")
		steps = append(steps, pullStep)
		if !pullStep.OK {
			return updateResult(req.ID, false, "error", "git", beforeSHA, beforeSHA, steps, startTime, deps.DenebDir, p.RestartDelayMs)
		}

		// Step 3: Get new git SHA (after).
		afterSHA := runGitRev(repoDir)

		// Step 4: Rebuild via make.
		buildStep := runStep(updateCtx, repoDir, "make all", "make", "all")
		steps = append(steps, buildStep)
		if !buildStep.OK {
			return updateResult(req.ID, false, "error", "make", beforeSHA, afterSHA, steps, startTime, deps.DenebDir, p.RestartDelayMs)
		}

		return updateResult(req.ID, true, "ok", "make", beforeSHA, afterSHA, steps, startTime, deps.DenebDir, p.RestartDelayMs)
	}
}

func updateResult(reqID string, ok bool, status, mode, beforeSHA, afterSHA string, steps []updateStep, startTime time.Time, denebDir string, restartDelayMs int64) *protocol.ResponseFrame {
	durationMs := time.Since(startTime).Milliseconds()

	// Write restart sentinel on success.
	var sentinel map[string]any
	if ok && denebDir != "" {
		sentinelPath := filepath.Join(denebDir, ".update-sentinel")
		sentinelPayload := map[string]any{
			"updatedAt":  time.Now().Format(time.RFC3339),
			"beforeSHA":  beforeSHA,
			"afterSHA":   afterSHA,
			"durationMs": durationMs,
		}
		data, _ := json.Marshal(sentinelPayload)
		_ = os.WriteFile(sentinelPath, data, 0644)
		sentinel = map[string]any{
			"path":    sentinelPath,
			"payload": sentinelPayload,
		}
	}

	var restart any
	if ok && restartDelayMs > 0 {
		restart = map[string]any{"delayMs": restartDelayMs}
	}

	result := map[string]any{
		"ok": ok,
		"result": map[string]any{
			"status":     status,
			"mode":       mode,
			"before":     map[string]any{"sha": beforeSHA},
			"after":      map[string]any{"sha": afterSHA},
			"steps":      steps,
			"durationMs": durationMs,
		},
		"restart":  restart,
		"sentinel": sentinel,
	}

	resp, _ := protocol.NewResponseOK(reqID, result)
	return resp
}

func runStep(ctx context.Context, dir, name string, cmdName string, args ...string) updateStep {
	start := time.Now()
	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Dir = dir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	step := updateStep{
		Name:       name,
		Command:    fmt.Sprintf("%s %s", cmdName, strings.Join(args, " ")),
		Cwd:        dir,
		DurationMs: time.Since(start).Milliseconds(),
		Log:        truncateLog(out.String(), 4096),
		OK:         err == nil,
	}
	return step
}

func runGitRev(dir string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func truncateLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}

// ---------------------------------------------------------------------------
// Usage
// ---------------------------------------------------------------------------

// UsageDeps holds dependencies for usage RPC methods.
type UsageDeps struct {
	Tracker *usage.Tracker
}

// UsageMethods returns the usage.status and usage.cost handlers.
func UsageMethods(deps UsageDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"usage.status": usageStatus(deps),
		"usage.cost":   usageCost(deps),
	}
}

func usageStatus(deps UsageDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Tracker == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"uptime":    "0s",
				"providers": map[string]any{},
			})
			return resp
		}
		resp, _ := protocol.NewResponseOK(req.ID, deps.Tracker.Status())
		return resp
	}
}

func usageCost(deps UsageDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Tracker == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"totalCalls": 0,
				"providers":  map[string]any{},
			})
			return resp
		}
		resp, _ := protocol.NewResponseOK(req.ID, deps.Tracker.Cost())
		return resp
	}
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

// LogsDeps holds dependencies for log-related RPC methods.
type LogsDeps struct {
	// LogDir is the directory containing rolling log files.
	// Defaults to ~/.deneb/logs/ if empty.
	LogDir string
}

// LogsMethods returns the logs.tail handler.
func LogsMethods(deps LogsDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"logs.tail": logsTail(deps),
	}
}

const (
	defaultLogLimit = 500
	maxLogLimit     = 5000
	defaultMaxBytes = 250 * 1024  // 250 KB
	maxMaxBytes     = 1024 * 1024 // 1 MB
)

func logsTail(deps LogsDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Cursor   *int64 `json:"cursor"`
			Limit    int    `json:"limit"`
			MaxBytes int    `json:"maxBytes"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.Limit <= 0 || p.Limit > maxLogLimit {
			p.Limit = defaultLogLimit
		}
		if p.MaxBytes <= 0 || p.MaxBytes > maxMaxBytes {
			p.MaxBytes = defaultMaxBytes
		}

		logDir := deps.LogDir
		if logDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrUnavailable, "cannot determine home directory: "+err.Error()))
			}
			logDir = filepath.Join(home, ".deneb", "logs")
		}

		// Find the most recent log file.
		logFile, err := findLatestLogFile(logDir)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "no log files found: "+err.Error()))
		}

		f, err := os.Open(logFile)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "cannot open log file: "+err.Error()))
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrUnavailable, "cannot stat log file: "+err.Error()))
		}

		fileSize := info.Size()
		var cursor int64
		reset := false

		if p.Cursor != nil {
			cursor = *p.Cursor
			// Detect log rotation: if cursor exceeds file size, reset to start.
			if cursor > fileSize {
				cursor = 0
				reset = true
			}
		}

		// Seek to cursor position.
		if cursor > 0 {
			if _, err := f.Seek(cursor, io.SeekStart); err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrUnavailable, "seek failed: "+err.Error()))
			}
		}

		// Read up to maxBytes.
		reader := io.LimitReader(f, int64(p.MaxBytes))
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

		var lines []string
		truncated := false
		bytesRead := int64(0)

		// If resuming mid-file, skip first partial line.
		if cursor > 0 && !reset {
			if scanner.Scan() {
				bytesRead += int64(len(scanner.Bytes())) + 1 // +1 for newline
			}
		}

		for scanner.Scan() {
			if len(lines) >= p.Limit {
				truncated = true
				break
			}
			line := scanner.Text()
			bytesRead += int64(len(line)) + 1
			lines = append(lines, line)
		}

		newCursor := cursor + bytesRead

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"cursor":    newCursor,
			"size":      fileSize,
			"lines":     lines,
			"truncated": truncated,
			"reset":     reset,
			"file":      filepath.Base(logFile),
		})
		return resp
	}
}

// findLatestLogFile returns the most recently modified log file in the directory.
func findLatestLogFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var logFiles []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			logFiles = append(logFiles, e)
		}
	}
	if len(logFiles) == 0 {
		return "", os.ErrNotExist
	}

	// Sort by name descending (deneb-YYYY-MM-DD.log sorts chronologically).
	sort.Slice(logFiles, func(i, j int) bool {
		return logFiles[i].Name() > logFiles[j].Name()
	})

	return filepath.Join(dir, logFiles[0].Name()), nil
}
