package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"time"

	runtimesession "github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle"
)

// Idle skill-review lane — the backstop that keeps the Propus review loop fed
// when no real user session crosses the nudge threshold for a long stretch.
//
// The nudger only fires mid-session on real user turns (cron/system sessions
// are excluded by design), so a quiet day — or a deploy storm cancelling
// review forks — starves the loop entirely (observed 2026-07-11: 22h with no
// completed review while crons kept running). Each heartbeat tick this lane
// checks the tracker's liveness heartbeat and, once the last completed review
// is older than DENEB_SKILL_IDLE_REVIEW_AFTER (default 6h; "0" disables),
// re-reviews the most recent real session's transcript through the exact same
// fenced review path (evaluate gate, reviewer, liveness recording) as a
// threshold fire.
//
// Pacing: a review that actually runs updates liveness lastReviewAt, which
// re-arms the staleness check naturally. Gate-rejected attempts (thin
// transcripts) do not touch lastReviewAt, so a separate retry gap keeps the
// lane from re-walking the same thin candidates every 30-minute tick.

const (
	defaultIdleReviewStaleAfter = 6 * time.Hour
	idleReviewRetryGap          = 2 * time.Hour
	idleReviewCandidates        = 10
)

// idleReviewStaleAfterFromEnv resolves the staleness threshold. Empty env →
// default; "0" (or any non-positive duration) → disabled; unparsable →
// default with a warning so a typo degrades loudly, not silently.
func idleReviewStaleAfterFromEnv(logger *slog.Logger) time.Duration {
	raw := strings.TrimSpace(os.Getenv("DENEB_SKILL_IDLE_REVIEW_AFTER"))
	if raw == "" {
		return defaultIdleReviewStaleAfter
	}
	if raw == "0" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		logger.Warn("idle skill review: invalid DENEB_SKILL_IDLE_REVIEW_AFTER, using default",
			"value", raw, "default", defaultIdleReviewStaleAfter)
		return defaultIdleReviewStaleAfter
	}
	if d <= 0 {
		return 0
	}
	return d
}

// idleReviewDue is the pure pacing check: fire only when reviews are stale
// (or have never run) AND the lane's own last attempt is outside the retry
// gap. lastReviewAtMs==0 means no review has ever completed — due. A FAILED
// last review (lastReviewOK=false) re-arms after the shorter retry gap
// instead of the full stale window, so a transient model outage doesn't
// suppress the backstop for hours (Codex P2, #3427).
func idleReviewDue(now time.Time, lastReviewAtMs int64, lastReviewOK bool, staleAfter time.Duration, lastAttempt time.Time, retryGap time.Duration) bool {
	if staleAfter <= 0 {
		return false
	}
	if lastReviewAtMs > 0 {
		threshold := staleAfter
		if !lastReviewOK && retryGap < threshold {
			threshold = retryGap
		}
		if now.Sub(time.UnixMilli(lastReviewAtMs)) < threshold {
			return false
		}
	}
	if !lastAttempt.IsZero() && now.Sub(lastAttempt) < retryGap {
		return false
	}
	return true
}

// idleReviewableSessionKey reports whether key is a real user-facing session
// worth re-reviewing. Mirrors the usage-source gate's intent: client surfaces
// qualify; cron/system/review forks (non-client prefixes), the dream session,
// and puppet-seat test sessions never do.
func idleReviewableSessionKey(key string) bool {
	if !strings.HasPrefix(key, "client:") {
		return false
	}
	if strings.HasPrefix(key, "client:puppet") {
		return false
	}
	// Delegated sub-agent runs share the client:main hierarchy; re-reviewing one
	// spends a heartbeat turn on the agent's own scratch work.
	if runtimesession.IsSpawnedChildKey(key) {
		return false
	}
	if strings.HasSuffix(key, ":dream") {
		return false
	}
	return true
}

// recentRealSessionKeys lists the newest reviewable session keys from the
// on-disk transcript dir — durable across the deploy restarts that empty the
// in-memory session manager (transcript filenames are the session keys). It is
// the capped, key-only projection of reviewableSessionsByMtime.
func recentRealSessionKeys(dir string, limit int) ([]string, error) {
	sessions, err := reviewableSessionsByMtime(dir)
	if err != nil {
		return nil, err
	}
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
	keys := make([]string, 0, len(sessions))
	for _, s := range sessions {
		keys = append(keys, s.key)
	}
	return keys, nil
}

// idleSkillReviewLaneIfProduction returns the lane closure only when this
// process owns the production state dir (productionStateDir — the same
// invariant memory-backup gates on). A dev/live-test instance
// (DENEB_STATE_DIR=/tmp/...) reads production transcripts and would write
// review liveness/proposals into production Propus state, so the lane stays
// off there (Codex P2, #3427).
func (s *Server) idleSkillReviewLaneIfProduction(homeDir string) func(ctx context.Context) (bool, string) {
	if _, prod := s.productionStateDir(homeDir); !prod {
		s.logger.Info("idle skill review lane disabled: non-production state dir")
		return nil
	}
	return s.newIdleSkillReviewLane()
}

// newIdleSkillReviewLane returns the heartbeat closure for this lane. Genesis
// deps late-bind via field reads per tick (genesis init runs after session
// wiring — same ordering-immune trick as the other heartbeat lanes). The
// attempt marker is a plain captured variable: heartbeat ticks run
// sequentially on one task goroutine, so no lock is needed; a restart resets
// it, costing at most one extra cheap gate-check per boot.
func (s *Server) newIdleSkillReviewLane() func(ctx context.Context) (bool, string) {
	staleAfter := idleReviewStaleAfterFromEnv(s.logger)
	var lastAttempt time.Time
	return func(_ context.Context) (bool, string) {
		tracker, nudger, store := s.genesisTracker, s.genesisNudger, s.genesisTranscripts
		if staleAfter <= 0 || tracker == nil || nudger == nil || store == nil {
			return false, ""
		}
		live := tracker.LivenessSnapshot()
		now := time.Now()
		if !idleReviewDue(now, live.LastReviewAt, live.LastReviewOK, staleAfter, lastAttempt, idleReviewRetryGap) {
			return false, ""
		}
		lastAttempt = now
		staleFor := "never"
		if live.LastReviewAt > 0 {
			staleFor = now.Sub(time.UnixMilli(live.LastReviewAt)).Round(time.Minute).String()
		}
		sessions, err := reviewableSessionsByMtime(transcriptBaseDir())
		if err != nil {
			// A missing dir is a fresh install (quiet); anything else is an
			// operational failure that must not masquerade as "no candidates".
			if errors.Is(err, fs.ErrNotExist) {
				s.logger.Debug("idle skill review: transcript dir absent", "dir", transcriptBaseDir())
			} else {
				s.logger.Warn("idle skill review: transcript dir unreadable", "dir", transcriptBaseDir(), "error", err)
			}
			return false, ""
		}
		// Walk the prospect frontier backward through history so OLDER substantial
		// sessions get mined too, not just the newest handful re-checked forever.
		cursorPath := prospectCursorPath()
		cursor := loadProspectCursor(cursorPath)
		candidates := sessionsOlderThan(sessions, cursor.FrontierMs)
		wrapped := false
		if len(candidates) == 0 {
			// Reached the bottom of the backlog — restart the sweep from the
			// newest so new sessions get picked up (repeats echo-dedup away).
			candidates = sessions
			wrapped = true
		}
		if len(candidates) > prospectBatch {
			candidates = candidates[:prospectBatch]
		}
		for _, cand := range candidates {
			sctx, err := skilllifecycle.BuildSessionContext(store, cand.key)
			if err != nil {
				continue
			}
			// Pre-filter thin transcripts without burning attempt/skip
			// counters — a run of short native chats must not starve the
			// lane when reviewable evidence sits just past them (Codex P2).
			if !nudger.WouldReview(sctx) {
				continue
			}
			fired, err := nudger.RunStaleReview(cand.key, sctx)
			if err != nil {
				// runReviewOnce already recorded the failure on liveness; the
				// lane just surfaces it and stops walking candidates this tick.
				s.logger.Warn("idle skill review: fenced review failed", "session", cand.key, "error", err)
				return false, ""
			}
			if fired {
				cursor.FrontierMs = cand.modMs // descend past this session next tick
				if err := saveProspectCursor(cursorPath, cursor); err != nil {
					s.logger.Warn("idle skill review: prospect cursor save failed", "error", err)
				}
				return true, fmt.Sprintf("session=%s staleFor=%s wrapped=%v", cand.key, staleFor, wrapped)
			}
		}
		// No substantial session in this batch: step the frontier past it so the
		// next fire advances to older work (and wraps once the bottom is reached).
		if n := len(candidates); n > 0 {
			cursor.FrontierMs = candidates[n-1].modMs
		} else {
			cursor.FrontierMs = 0
		}
		if err := saveProspectCursor(cursorPath, cursor); err != nil {
			s.logger.Warn("idle skill review: prospect cursor save failed", "error", err)
		}
		s.logger.Debug("idle skill review: no session in batch passed the review gate", "staleFor", staleFor, "wrapped", wrapped)
		return false, ""
	}
}
