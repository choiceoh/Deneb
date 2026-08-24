// fact_cutover_guard.go — Bounded retry for the one-time fact-plane cutover.
//
// The cutover is fail-closed by design: if the legacy import or the projection
// swap fails, the store is closed and startup returns an error, which exits the
// process. For a TRANSIENT failure that is exactly right — the supervisor
// restarts, the idempotent import resumes, and nothing is ever served from a
// hybrid state where the journal advanced but frozen MEMORY/USER still carry
// the retired value.
//
// For a DETERMINISTIC failure it is wrong. On 2026-08-23 one unconvertible
// legacy bullet failed the import on every single attempt: the gateway
// restarted 150 times in 28 minutes and was unavailable for all of them. A
// retry that provably cannot succeed is not fail-closed, it is fail-forever,
// and a total outage is strictly worse than the degraded memory the retry was
// protecting against.
//
// So the retry is bounded, and the counter is PERSISTED — a process-local
// counter would reset on each restart and reproduce the same loop exactly. On
// the maxFactCutoverAttempts-th consecutive failure the cutover is declared
// deterministic and the gateway starts with wiki disabled, which is what the
// fail-closed path always said it wanted ("disable wiki for this process").
// A success clears the counter, so a later transient failure gets a full retry
// budget again.
package server

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// maxFactCutoverAttempts is how many consecutive startups may die on the same
// cutover before it is treated as deterministic. Three restarts cover the
// recoverable causes (a lock still held by an exiting process, a transient
// filesystem error) in well under a minute, and stop far short of the 28-minute
// outage an unbounded retry produced.
const maxFactCutoverAttempts = 3

const factCutoverFileName = "fact-cutover-state.json"

// factCutoverState is the persisted failure streak.
type factCutoverState struct {
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	LastError           string `json:"lastError,omitempty"`
	LastAttemptMs       int64  `json:"lastAttemptMs,omitempty"`
}

// factCutoverGuard reads and writes that streak. A guard with no path (no
// resolvable state dir) degrades to the old unbounded behaviour rather than
// silently allowing a degraded start on the first failure: without persistence
// the count is meaningless, and fail-closed is the safer of the two.
type factCutoverGuard struct {
	path   string
	logger *slog.Logger
}

func newFactCutoverGuard(logger *slog.Logger) *factCutoverGuard {
	dir := strings.TrimSpace(config.ResolveStateDir())
	g := &factCutoverGuard{logger: logger}
	if dir != "" {
		g.path = filepath.Join(dir, factCutoverFileName)
	}
	return g
}

func (g *factCutoverGuard) load() factCutoverState {
	if g == nil || g.path == "" {
		return factCutoverState{}
	}
	data, err := os.ReadFile(g.path)
	if err != nil {
		return factCutoverState{}
	}
	var state factCutoverState
	if err := json.Unmarshal(data, &state); err != nil {
		// An unreadable counter must not itself become a reason to degrade.
		return factCutoverState{}
	}
	return state
}

// recordFailure increments the streak and returns the new count. A count that
// cannot be persisted is reported as 1, which keeps the caller on the
// fail-closed path forever — the pre-existing behaviour, not a worse one.
func (g *factCutoverGuard) recordFailure(cause error) int {
	if g == nil || g.path == "" {
		return 1
	}
	state := g.load()
	state.ConsecutiveFailures++
	if cause != nil {
		state.LastError = cause.Error()
	}
	state.LastAttemptMs = time.Now().UnixMilli()

	data, err := json.Marshal(state)
	if err != nil {
		return 1
	}
	if err := atomicfile.WriteFile(g.path, data, nil); err != nil {
		if g.logger != nil {
			g.logger.Warn("fact cutover failure counter not persisted", "path", g.path, "error", err)
		}
		return 1
	}
	return state.ConsecutiveFailures
}

// recordSuccess clears the streak. Only writes when there is something to
// clear, so the common startup touches no disk.
func (g *factCutoverGuard) recordSuccess() {
	if g == nil || g.path == "" {
		return
	}
	if g.load().ConsecutiveFailures == 0 {
		return
	}
	if err := os.Remove(g.path); err != nil && !os.IsNotExist(err) && g.logger != nil {
		g.logger.Warn("fact cutover failure counter not cleared", "path", g.path, "error", err)
	}
}
