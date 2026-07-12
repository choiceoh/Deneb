package heartbeat

// heartbeat_auto_apply.go — P2 of the instruction-surface evolve program
// (docs/research/instruction-surface-evolve.md; RSI 2026H2 addendum #4).
// P1's shadow-replay gate is a dry-run report; this adds the flag-gated apply
// half exactly as designed: DENEB_HEARTBEAT_AUTO_APPLY=1 (default OFF — the
// operator flips it after observing P1 reports) lets an accept-verdict
// candidate overwrite HEARTBEAT.md with a timestamped backup, and a
// deterministic anomaly watch (K consecutive failed heartbeat turns after an
// apply) auto-restores the backup — mirroring the skill rollback watch. The
// declared surface tier stays propose-only until the operator turns the flag
// on; the flag IS the tier flip, same pattern as the e-process cutover knob.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	// heartbeatAutoApplyEnv is the operator lever (default off).
	heartbeatAutoApplyEnv = "DENEB_HEARTBEAT_AUTO_APPLY"
	// heartbeatAutoApplyBackup keeps the pre-apply contract for the anomaly
	// watch and the operator (distinct from heartbeat_update's .prev, which
	// the agent's own updates overwrite every turn).
	heartbeatAutoApplyBackup = "HEARTBEAT.md.autoapply.bak"
	// heartbeatAutoApplyMarker persists the in-flight anomaly watch across
	// restarts (same argument as the skill evolve watch persistence).
	heartbeatAutoApplyMarker = "HEARTBEAT.md.autoapply.json"
	// heartbeatAutoApplyRollbackThreshold is K consecutive failed heartbeat
	// turns after an apply before the backup is restored.
	heartbeatAutoApplyRollbackThreshold = 3
)

// AutoApplyEnabled reports the operator flag (instruction-surface P2).
func AutoApplyEnabled() bool {
	return os.Getenv(heartbeatAutoApplyEnv) == "1"
}

// autoApplyMarker is the persisted anomaly-watch state.
type autoApplyMarker struct {
	AppliedAt  int64  `json:"appliedAt"`
	BackupPath string `json:"backupPath"`
	Failures   int    `json:"failures"`
}

// MaybeAutoApplyCandidate runs the P1 shadow-replay gate over candidate and,
// when the flag is on AND the verdict is "accept", backs up the live
// HEARTBEAT.md and writes the candidate. Returns (applied, verdict+reason).
// Flag off or a non-accept verdict leaves the file untouched — the caller's
// propose-only path proceeds as before with the report attached.
func MaybeAutoApplyCandidate(ctx context.Context, heartbeatPath, fixturePath, candidate string, complete ShadowCompleteFunc, logger *slog.Logger) (bool, string, error) {
	if logger == nil {
		logger = slog.Default()
	}
	report, err := RunShadowReplay(ctx, fixturePath, candidate, 0, complete)
	if err != nil {
		return false, "", fmt.Errorf("heartbeat auto-apply: shadow replay: %w", err)
	}
	verdict := report.Verdict + ": " + report.Reason
	if !AutoApplyEnabled() {
		return false, verdict + " (auto-apply flag off — propose-only)", nil
	}
	if report.Verdict != "accept" {
		return false, verdict, nil
	}
	current, err := os.ReadFile(heartbeatPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, verdict, fmt.Errorf("heartbeat auto-apply: read live contract: %w", err)
	}
	backupPath := heartbeatPath + ".autoapply.bak"
	if err := os.WriteFile(backupPath, current, 0o644); err != nil {
		return false, verdict, fmt.Errorf("heartbeat auto-apply: backup write: %w", err)
	}
	if err := os.WriteFile(heartbeatPath, []byte(strings.TrimSpace(candidate)+"\n"), 0o644); err != nil {
		return false, verdict, fmt.Errorf("heartbeat auto-apply: contract write: %w", err)
	}
	marker := autoApplyMarker{AppliedAt: time.Now().UnixMilli(), BackupPath: backupPath}
	if raw, merr := json.Marshal(marker); merr == nil {
		if werr := os.WriteFile(heartbeatPath+".autoapply.json", raw, 0o644); werr != nil {
			logger.Warn("heartbeat auto-apply: marker write failed (anomaly watch disarmed)", "error", werr)
		}
	}
	logger.Info("heartbeat auto-apply: candidate APPLIED (shadow gate accept; anomaly watch armed)",
		"heldIn", fmt.Sprintf("%d/%d→%d/%d", report.HeldInOriginal, report.HeldInTotal, report.HeldInCandidate, report.HeldInTotal),
		"heldOut", fmt.Sprintf("%d/%d→%d/%d", report.HeldOutOriginal, report.HeldOutTotal, report.HeldOutCandidate, report.HeldOutTotal))
	return true, verdict, nil
}

// noteHeartbeatTurnOutcome advances the post-apply anomaly watch: a
// successful heartbeat turn resets the failure streak; K consecutive failures
// restore the pre-apply backup and disarm the watch. No-op without a marker
// (nothing was auto-applied). Deterministic and best-effort — the heartbeat
// turn itself must never fail because of the watch.
func noteHeartbeatTurnOutcome(heartbeatPath string, ok bool, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	markerPath := heartbeatPath + ".autoapply.json"
	raw, err := os.ReadFile(markerPath)
	if err != nil {
		return // no in-flight watch
	}
	var marker autoApplyMarker
	if err := json.Unmarshal(raw, &marker); err != nil {
		logger.Warn("heartbeat auto-apply: marker corrupt, disarming watch", "error", err)
		_ = os.Remove(markerPath)
		return
	}
	if ok {
		if marker.Failures != 0 {
			marker.Failures = 0
			if out, merr := json.Marshal(marker); merr == nil {
				_ = os.WriteFile(markerPath, out, 0o644)
			}
		}
		return
	}
	marker.Failures++
	if marker.Failures < heartbeatAutoApplyRollbackThreshold {
		if out, merr := json.Marshal(marker); merr == nil {
			_ = os.WriteFile(markerPath, out, 0o644)
		}
		return
	}
	backup, err := os.ReadFile(marker.BackupPath)
	if err != nil {
		logger.Error("heartbeat auto-apply: anomaly threshold hit but backup unreadable — manual restore needed",
			"backup", marker.BackupPath, "error", err)
		_ = os.Remove(markerPath)
		return
	}
	if err := os.WriteFile(heartbeatPath, backup, 0o644); err != nil {
		logger.Error("heartbeat auto-apply: rollback restore FAILED", "error", err)
		return
	}
	_ = os.Remove(markerPath)
	logger.Error("heartbeat auto-apply: contract AUTO-RESTORED after consecutive heartbeat failures",
		"failures", marker.Failures, "appliedAt", time.UnixMilli(marker.AppliedAt).Format(time.RFC3339))
}
