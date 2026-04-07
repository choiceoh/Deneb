// tool_loop.go — Tool loop detection for the agent execution loop.
//
// Inspired by OpenClaw's tool-loop-detection.ts. Detects three stuck patterns:
//   - generic_repeat: same tool + same args called repeatedly
//   - poll_no_progress: polling tool with identical outcomes (no progress)
//   - ping_pong: alternating A→B→A→B pattern with no progress
//
// Plus a global circuit breaker as a catch-all safety net.
package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
)

// ToolLoopConfig controls tool loop detection thresholds.
type ToolLoopConfig struct {
	Enabled                       bool
	HistorySize                   int // sliding window (default 30)
	WarningThreshold              int // generic repeat warning (default 10)
	CriticalThreshold             int // poll/ping-pong critical block (default 20)
	GlobalCircuitBreakerThreshold int // absolute catch-all (default 30)
}

// DefaultToolLoopConfig returns sensible defaults for tool loop detection.
func DefaultToolLoopConfig() ToolLoopConfig {
	return ToolLoopConfig{
		Enabled:                       true,
		HistorySize:                   30,
		WarningThreshold:              10,
		CriticalThreshold:             20,
		GlobalCircuitBreakerThreshold: 30,
	}
}

// ToolLoopLevel indicates the severity of a detected loop.
type ToolLoopLevel int

const (
	ToolLoopNone     ToolLoopLevel = iota
	ToolLoopWarning                // log a warning, allow execution
	ToolLoopCritical               // block execution
)

// ToolLoopResult is the outcome of loop detection for a single tool call.
type ToolLoopResult struct {
	Stuck    bool
	Level    ToolLoopLevel
	Detector string // "generic_repeat", "poll_no_progress", "ping_pong", "circuit_breaker"
	Count    int
	Message  string
}

// toolCallRecord is a single entry in the history window.
type toolCallRecord struct {
	ToolName   string
	ArgsHash   string
	ResultHash string // populated after execution
}

// ToolLoopDetector tracks tool call history and detects stuck patterns.
type ToolLoopDetector struct {
	mu      sync.Mutex
	cfg     ToolLoopConfig
	history []toolCallRecord
	logger  *slog.Logger
}

// NewToolLoopDetector creates a detector with the given config.
func NewToolLoopDetector(cfg ToolLoopConfig, logger *slog.Logger) *ToolLoopDetector {
	if cfg.HistorySize <= 0 {
		cfg.HistorySize = 30
	}
	if cfg.WarningThreshold <= 0 {
		cfg.WarningThreshold = 10
	}
	if cfg.CriticalThreshold <= 0 {
		cfg.CriticalThreshold = 20
	}
	if cfg.CriticalThreshold <= cfg.WarningThreshold {
		cfg.CriticalThreshold = cfg.WarningThreshold + 1
	}
	if cfg.GlobalCircuitBreakerThreshold <= 0 {
		cfg.GlobalCircuitBreakerThreshold = 30
	}
	return &ToolLoopDetector{
		cfg:     cfg,
		history: make([]toolCallRecord, 0, cfg.HistorySize),
		logger:  logger,
	}
}

// knownPollingTools are tools that naturally repeat but should only be flagged
// when their results don't change (no progress).
var knownPollingTools = map[string]bool{
	"process": true,
	"exec":    true,
}

// isPollingInvocation checks if a tool call is a polling/status check.
func isPollingInvocation(name string, _ []byte) bool {
	return knownPollingTools[name]
}

// RecordAndCheck adds a tool call to history and checks for loops.
// Call this BEFORE executing the tool.
func (d *ToolLoopDetector) RecordAndCheck(name string, argsJSON []byte) ToolLoopResult {
	if !d.cfg.Enabled {
		return ToolLoopResult{}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	argsHash := hashBytes(argsJSON)
	record := toolCallRecord{
		ToolName: name,
		ArgsHash: argsHash,
	}

	// Append to history, trim to window size.
	d.history = append(d.history, record)
	if len(d.history) > d.cfg.HistorySize {
		d.history = d.history[len(d.history)-d.cfg.HistorySize:]
	}

	// Run detectors in priority order.

	// 1. Global circuit breaker: count total calls with same name+args in window.
	globalCount := d.countIdentical(name, argsHash)
	if globalCount >= d.cfg.GlobalCircuitBreakerThreshold {
		return ToolLoopResult{
			Stuck:    true,
			Level:    ToolLoopCritical,
			Detector: "circuit_breaker",
			Count:    globalCount,
			Message:  fmt.Sprintf("CRITICAL: %s has been called %d times with identical arguments — global circuit breaker tripped. Stop calling this tool.", name, globalCount),
		}
	}

	// 2. Poll no-progress: for known polling tools, check result-hash streak.
	if isPollingInvocation(name, argsJSON) {
		streak := d.noProgressStreak(name, argsHash)
		if streak >= d.cfg.CriticalThreshold {
			return ToolLoopResult{
				Stuck:    true,
				Level:    ToolLoopCritical,
				Detector: "poll_no_progress",
				Count:    streak,
				Message:  fmt.Sprintf("CRITICAL: %s has repeated identical no-progress outcomes %d times. The operation is stuck — try a different approach or stop polling.", name, streak),
			}
		}
		if streak >= d.cfg.WarningThreshold {
			return ToolLoopResult{
				Stuck:    true,
				Level:    ToolLoopWarning,
				Detector: "poll_no_progress",
				Count:    streak,
				Message:  fmt.Sprintf("WARNING: %s has been polled %d times with identical results. Consider checking if the operation is making progress.", name, streak),
			}
		}
	}

	// 3. Ping-pong: alternating between two tool calls.
	if pp := d.detectPingPong(); pp != nil {
		return *pp
	}

	// 4. Generic repeat: same tool+args repeated (skip for polling tools —
	//    poll_no_progress handles those with result-hash awareness).
	if isPollingInvocation(name, argsJSON) {
		return ToolLoopResult{}
	}
	repeatCount := d.countIdentical(name, argsHash)
	if repeatCount >= d.cfg.CriticalThreshold {
		return ToolLoopResult{
			Stuck:    true,
			Level:    ToolLoopCritical,
			Detector: "generic_repeat",
			Count:    repeatCount,
			Message:  fmt.Sprintf("CRITICAL: %s called %d times with identical arguments. Execution blocked — try a different approach.", name, repeatCount),
		}
	}
	if repeatCount >= d.cfg.WarningThreshold {
		return ToolLoopResult{
			Stuck:    true,
			Level:    ToolLoopWarning,
			Detector: "generic_repeat",
			Count:    repeatCount,
			Message:  fmt.Sprintf("WARNING: %s has been called %d times with identical arguments and is likely stuck in a loop. Vary your approach.", name, repeatCount),
		}
	}

	return ToolLoopResult{}
}

// RecordResult updates the last history entry with the tool's result hash.
// Call this AFTER executing the tool.
func (d *ToolLoopDetector) RecordResult(name string, result string, isError bool) {
	if !d.cfg.Enabled {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Find the last entry for this tool (should be the most recent).
	for i := len(d.history) - 1; i >= 0; i-- {
		if d.history[i].ToolName == name && d.history[i].ResultHash == "" {
			d.history[i].ResultHash = hashString(result)
			break
		}
	}
}

// countIdentical counts how many times tool+argsHash appears in history.
func (d *ToolLoopDetector) countIdentical(name, argsHash string) int {
	count := 0
	for _, r := range d.history {
		if r.ToolName == name && r.ArgsHash == argsHash {
			count++
		}
	}
	return count
}

// noProgressStreak counts consecutive calls to the same tool+args with
// identical result hashes from the tail of history.
func (d *ToolLoopDetector) noProgressStreak(name, argsHash string) int {
	if len(d.history) < 2 {
		return 0
	}

	// Find the latest result hash for this tool+args.
	var latestResultHash string
	for i := len(d.history) - 1; i >= 0; i-- {
		r := d.history[i]
		if r.ToolName == name && r.ArgsHash == argsHash && r.ResultHash != "" {
			latestResultHash = r.ResultHash
			break
		}
	}
	if latestResultHash == "" {
		// No results recorded yet; count by args only.
		return d.countIdentical(name, argsHash)
	}

	// Count consecutive matching entries from the tail.
	streak := 0
	for i := len(d.history) - 1; i >= 0; i-- {
		r := d.history[i]
		if r.ToolName != name || r.ArgsHash != argsHash {
			break
		}
		if r.ResultHash != "" && r.ResultHash != latestResultHash {
			break // progress detected
		}
		streak++
	}
	return streak
}

// detectPingPong checks if the last N calls alternate between two distinct
// tool+args combinations with no progress on either side.
func (d *ToolLoopDetector) detectPingPong() *ToolLoopResult {
	n := len(d.history)
	if n < 4 {
		return nil
	}

	// Identify the two most recent distinct calls.
	last := d.history[n-1]
	var prev toolCallRecord
	prevFound := false
	for i := n - 2; i >= 0; i-- {
		r := d.history[i]
		if r.ToolName != last.ToolName || r.ArgsHash != last.ArgsHash {
			prev = r
			prevFound = true
			break
		}
	}
	if !prevFound {
		return nil
	}

	// Check for alternating pattern from the tail.
	alternatingCount := 0
	expectA := true // expect last's pattern
	for i := n - 1; i >= 0; i-- {
		r := d.history[i]
		if expectA {
			if r.ToolName == last.ToolName && r.ArgsHash == last.ArgsHash {
				alternatingCount++
				expectA = false
			} else {
				break
			}
		} else {
			if r.ToolName == prev.ToolName && r.ArgsHash == prev.ArgsHash {
				alternatingCount++
				expectA = true
			} else {
				break
			}
		}
	}

	if alternatingCount >= d.cfg.CriticalThreshold {
		return &ToolLoopResult{
			Stuck:    true,
			Level:    ToolLoopCritical,
			Detector: "ping_pong",
			Count:    alternatingCount,
			Message: fmt.Sprintf("CRITICAL: ping-pong loop detected between %s and %s (%d alternating calls). Both tools are producing identical results — break the cycle.",
				last.ToolName, prev.ToolName, alternatingCount),
		}
	}
	if alternatingCount >= d.cfg.WarningThreshold {
		return &ToolLoopResult{
			Stuck:    true,
			Level:    ToolLoopWarning,
			Detector: "ping_pong",
			Count:    alternatingCount,
			Message: fmt.Sprintf("WARNING: possible ping-pong loop between %s and %s (%d alternating calls). Consider a different approach.",
				last.ToolName, prev.ToolName, alternatingCount),
		}
	}

	return nil
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8]) // 16 hex chars is plenty for dedup
}

func hashString(s string) string {
	return hashBytes([]byte(s))
}
