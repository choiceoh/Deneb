// heartbeat_fixtures.go — P0 of the instruction-surface evolve program
// (docs/research/instruction-surface-evolve.md): harvest every heartbeat
// firing into a replay fixture so a future shadow-replay gate can compare the
// current HEARTBEAT.md contract against a candidate over REAL signal contexts
// instead of authored ones. Deterministic, no LLM calls; the corpus is a
// rolling JSONL window pruned in place (same sidecar discipline as the
// genesis tracker stores).
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	// heartbeatFixtureCap is the rolling-window size consumers see; pruning
	// runs with hysteresis so the file is rewritten at most every
	// heartbeatFixturePruneSlack appends, not on each one.
	heartbeatFixtureCap        = 100
	heartbeatFixturePruneSlack = 20
	// heartbeatFixtureBodyLimit caps stored text fields (runes). HEARTBEAT.md
	// keeps enough to replay as-fired; nudges/outcomes are prompt-sized anyway.
	heartbeatFixtureBodyLimit = 8000
)

// heartbeatFixture is one recorded firing: the per-tick variable inputs kept
// SEPARATE from the HEARTBEAT.md contract (so replay can re-assemble the
// trigger with a candidate contract), plus the actual turn outcome as ground
// truth for deterministic verifiers (NO_REPLY discipline, action selection).
type heartbeatFixture struct {
	FiredAt         int64  `json:"firedAt"`
	SessionKey      string `json:"sessionKey,omitempty"`
	SignalSummary   string `json:"signalSummary,omitempty"`
	SelfCodingNudge string `json:"selfCodingNudge,omitempty"`
	SweepNudge      string `json:"sweepNudge,omitempty"`
	ResearchNudge   string `json:"researchNudge,omitempty"`
	// HeartbeatMD is the contract content at fire time; HeartbeatHash lets a
	// replay consumer filter fixtures recorded under a drifted contract
	// without diffing bodies.
	HeartbeatMD   string `json:"heartbeatMd,omitempty"`
	HeartbeatHash string `json:"heartbeatHash,omitempty"`
	// OutcomeText is the turn's final text (truncated); OutcomeErr is set when
	// the turn failed instead. Exactly one is normally non-empty.
	OutcomeText string `json:"outcomeText,omitempty"`
	OutcomeErr  string `json:"outcomeErr,omitempty"`
}

func (t *heartbeatTask) heartbeatFixturePath() string {
	return heartbeatFixturePathFor(t.homeDir)
}

// heartbeatFixturePathFor is shared with the shadow-replay backend wiring
// (init_genesis.go), which has no heartbeatTask at hand.
func heartbeatFixturePathFor(homeDir string) string {
	return filepath.Join(homeDir, ".deneb", "data", "heartbeat_fixtures.jsonl")
}

// recordHeartbeatFixture persists one firing. Best-effort: a failed write must
// never affect the heartbeat turn itself, but it is logged so a persistently
// broken corpus is visible (the shadow-replay gate starves silently without
// fixtures — the exact failure mode the backfill lane exists to prevent for
// skills).
func (t *heartbeatTask) recordHeartbeatFixture(fixture heartbeatFixture) {
	if t.homeDir == "" {
		return
	}
	fixture.SignalSummary = truncateHeartbeatFixtureText(fixture.SignalSummary)
	fixture.SelfCodingNudge = truncateHeartbeatFixtureText(fixture.SelfCodingNudge)
	fixture.SweepNudge = truncateHeartbeatFixtureText(fixture.SweepNudge)
	fixture.ResearchNudge = truncateHeartbeatFixtureText(fixture.ResearchNudge)
	fixture.HeartbeatMD = truncateHeartbeatFixtureText(fixture.HeartbeatMD)
	fixture.OutcomeText = truncateHeartbeatFixtureText(fixture.OutcomeText)
	fixture.OutcomeErr = truncateHeartbeatFixtureText(fixture.OutcomeErr)
	if fixture.HeartbeatMD != "" {
		sum := sha256.Sum256([]byte(fixture.HeartbeatMD))
		fixture.HeartbeatHash = hex.EncodeToString(sum[:8])
	}
	if fixture.FiredAt == 0 {
		fixture.FiredAt = time.Now().UnixMilli()
	}

	path := t.heartbeatFixturePath()
	if err := jsonlstore.Append(path, fixture); err != nil {
		t.logger.Warn("heartbeat: fixture append failed", "error", err)
		return
	}
	pruneHeartbeatFixtures(t, path)
}

// pruneHeartbeatFixtures keeps the newest heartbeatFixtureCap records once the
// file overshoots the slack, rewriting via Snapshot. Ordering trusts append
// order (fixtures are written by the single heartbeat task); FiredAt sorting
// is deliberately avoided so a clock step can't shuffle the corpus.
func pruneHeartbeatFixtures(t *heartbeatTask, path string) {
	entries, err := jsonlstore.Load[heartbeatFixture](path)
	if err != nil {
		t.logger.Warn("heartbeat: fixture prune load failed", "error", err)
		return
	}
	if len(entries) <= heartbeatFixtureCap+heartbeatFixturePruneSlack {
		return
	}
	keep := entries[len(entries)-heartbeatFixtureCap:]
	if err := jsonlstore.Snapshot(path, keep); err != nil {
		t.logger.Warn("heartbeat: fixture prune snapshot failed", "error", err)
	}
}

func truncateHeartbeatFixtureText(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= heartbeatFixtureBodyLimit {
		return s
	}
	return string(runes[:heartbeatFixtureBodyLimit]) + " …[truncated]"
}
