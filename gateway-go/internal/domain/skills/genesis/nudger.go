// nudger.go implements iteration-based skill nudging.
//
// Unlike session-end genesis (which fires only once after a conversation
// completes), the Nudger periodically asks the LLM — while a session is
// still running — whether the agent has just solved something reusable.
// This catches mid-session skill opportunities that would otherwise be
// forgotten before the session ends.
//
// Design notes:
//   - Strictly background: never injects into the user-facing turn or
//     modifies conversation messages. A fire is always spawned via
//     pkg/safego so a panic can't kill the process.
//   - Shares the existing Service cooldown/daily-cap so a noisy nudge
//     cycle still respects MaxSkillsPerDay.
//   - Threshold is configurable via DENEB_SKILL_NUDGE_INTERVAL; 0 disables.
package genesis

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// DefaultNudgeInterval is the default number of tool invocations between
// background skill-review fires. Modeled on Hermes' _skill_nudge_interval=10.
const DefaultNudgeInterval = 10

// nudgeGenerationTimeout caps a single background review so a stuck LLM
// call cannot leak goroutines indefinitely.
const nudgeGenerationTimeout = 90 * time.Second

// Nudger tracks per-session tool activity and fires a background skill
// review once a configurable tool-invocation threshold is crossed.
//
// One Nudger is shared across every chat session. Per-session state is
// keyed by session key. All public methods are safe for concurrent use.
type Nudger struct {
	svc      *Service
	logger   *slog.Logger
	interval int

	mu       sync.Mutex
	counts   map[string]int       // sessionKey -> tool invocations since last fire
	inflight map[string]time.Time // sessionKey -> fire started (guards against dupes)
}

// NudgerConfig configures a Nudger.
type NudgerConfig struct {
	// Interval is the tool-call threshold. <=0 disables nudging.
	Interval int
}

// NewNudger creates a Nudger bound to an existing genesis Service.
// The Service supplies the LLM client, catalog, and cooldown/daily cap
// state — the Nudger never duplicates that logic.
//
// If svc is nil the Nudger becomes a no-op so callers can install it
// unconditionally without checking whether genesis is configured.
func NewNudger(svc *Service, cfg NudgerConfig, logger *slog.Logger) *Nudger {
	if logger == nil {
		logger = slog.Default()
	}
	interval := cfg.Interval
	if interval < 0 {
		interval = 0
	}
	return &Nudger{
		svc:      svc,
		logger:   logger,
		interval: interval,
		counts:   make(map[string]int),
		inflight: make(map[string]time.Time),
	}
}

// NewNudgerFromEnv reads DENEB_SKILL_NUDGE_INTERVAL and falls back to
// DefaultNudgeInterval when the env var is unset or invalid.
func NewNudgerFromEnv(svc *Service, logger *slog.Logger) *Nudger {
	interval := DefaultNudgeInterval
	if v := os.Getenv("DENEB_SKILL_NUDGE_INTERVAL"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			interval = parsed
		}
	}
	return NewNudger(svc, NudgerConfig{Interval: interval}, logger)
}

// Enabled reports whether the nudger is configured to fire. Used by the
// chat pipeline to skip per-turn bookkeeping entirely when disabled.
func (n *Nudger) Enabled() bool {
	return n != nil && n.svc != nil && n.interval > 0
}

// Interval returns the configured threshold (0 when disabled).
func (n *Nudger) Interval() int {
	if n == nil {
		return 0
	}
	return n.interval
}

// OnToolCalls is called by the chat pipeline with the number of tool
// invocations that completed this turn. When the running counter for
// sessionKey crosses the interval threshold, the counter resets and a
// background review is spawned with `snapshot` as session context.
//
// When disabled (interval==0) or inputs are invalid, this is a no-op so
// the caller does not need defensive checks.
//
// snapshot is captured synchronously so the background goroutine works
// on a stable copy even if the agent loop continues to mutate state.
func (n *Nudger) OnToolCalls(ctx context.Context, sessionKey string, delta int, snapshot SessionContext) {
	if !n.Enabled() || sessionKey == "" || delta <= 0 {
		return
	}
	if !n.increment(sessionKey, delta) {
		return
	}
	n.logger.Info("skill nudger: threshold crossed, spawning background review",
		"session", sessionKey, "interval", n.interval, "turns", snapshot.Turns,
		"tools", len(snapshot.ToolActivities))
	n.fire(ctx, sessionKey, snapshot)
}

// Reset clears the per-session counter. Call at session start, on skill
// creation (so a genesis fire and a nudge don't double-count), or on
// session abort.
func (n *Nudger) Reset(sessionKey string) {
	if n == nil || sessionKey == "" {
		return
	}
	n.mu.Lock()
	delete(n.counts, sessionKey)
	delete(n.inflight, sessionKey)
	n.mu.Unlock()
}

// Count returns the current tool-call count for sessionKey (primarily
// for tests — production code should not depend on internal state).
func (n *Nudger) Count(sessionKey string) int {
	if n == nil {
		return 0
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.counts[sessionKey]
}

// increment bumps the per-session counter and returns true when the
// threshold has been reached, resetting the counter atomically so
// concurrent calls from multiple turns don't double-fire.
func (n *Nudger) increment(sessionKey string, delta int) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	// Skip when a prior fire is still running for this session; this
	// avoids two overlapping reviews if the interval is small and the
	// LLM call is slow.
	if _, busy := n.inflight[sessionKey]; busy {
		return false
	}
	n.counts[sessionKey] += delta
	if n.counts[sessionKey] < n.interval {
		return false
	}
	n.counts[sessionKey] = 0
	n.inflight[sessionKey] = time.Now()
	return true
}

// clearInflight marks a session as no longer running a nudge fire.
func (n *Nudger) clearInflight(sessionKey string) {
	n.mu.Lock()
	delete(n.inflight, sessionKey)
	n.mu.Unlock()
}

// fire spawns a background goroutine that runs the genesis evaluation
// and (if skill-worthy) persists a new skill. All errors are logged;
// none propagate to the caller's turn. pkg/safego adds panic recovery.
func (n *Nudger) fire(parent context.Context, sessionKey string, snapshot SessionContext) {
	// We intentionally do NOT inherit the parent ctx's deadline because
	// the review must continue even after the user-facing turn returns.
	// runOnce constructs its own bounded context below.
	_ = parent
	safego.GoWithSlog(n.logger, "skill-nudger-fire", func() {
		defer n.clearInflight(sessionKey)
		// Errors are intentionally swallowed — Nudger never propagates to
		// the user turn.
		_, _ = n.runOnce(sessionKey, snapshot)
	})
}

// runOnce performs one evaluation + generate + persist cycle. Returns
// (persisted, err) where persisted is true only when a new SKILL.md was
// written. Exported path for tests — production code uses fire().
func (n *Nudger) runOnce(sessionKey string, snapshot SessionContext) (bool, error) {
	if !n.Enabled() {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), nudgeGenerationTimeout)
	defer cancel()
	// Evaluate with the Service so cooldown/daily-cap logic stays in
	// one place. When Evaluate returns false, skip silently — that's
	// not an error, it just means the mid-session heuristics did not
	// find enough signal yet.
	if !n.svc.Evaluate(snapshot) {
		n.logger.Debug("skill nudger: evaluate rejected session",
			"session", sessionKey, "turns", snapshot.Turns,
			"tools", len(snapshot.ToolActivities))
		return false, nil
	}
	skill, err := n.svc.Generate(ctx, snapshot)
	if err != nil {
		n.logger.Warn("skill nudger: generate failed",
			"session", sessionKey, "error", err)
		return false, err
	}
	if skill == nil {
		// LLM decided no skill is worth creating.
		return false, nil
	}
	if err := n.svc.Persist(skill); err != nil {
		n.logger.Error("skill nudger: persist failed",
			"session", sessionKey, "skill", skill.Name, "error", err)
		return false, err
	}
	n.logger.Info("skill nudger: created mid-session skill",
		"session", sessionKey, "skill", skill.Name, "category", skill.Category)
	return true, nil
}
