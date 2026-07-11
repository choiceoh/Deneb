package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"
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
	idleReviewCandidates        = 3
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
// gap. lastReviewAtMs==0 means no review has ever completed — due.
func idleReviewDue(now time.Time, lastReviewAtMs int64, staleAfter time.Duration, lastAttempt time.Time, retryGap time.Duration) bool {
	if staleAfter <= 0 {
		return false
	}
	if lastReviewAtMs > 0 && now.Sub(time.UnixMilli(lastReviewAtMs)) < staleAfter {
		return false
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
	if strings.HasSuffix(key, ":dream") {
		return false
	}
	return true
}

// recentRealSessionKeys lists the newest reviewable session keys from the
// on-disk transcript dir — durable across the deploy restarts that empty the
// in-memory session manager (transcript filenames are the session keys).
func recentRealSessionKeys(dir string, limit int) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type candidate struct {
		key string
		mod time.Time
	}
	var cands []candidate
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		key := strings.TrimSuffix(name, ".jsonl")
		if !idleReviewableSessionKey(key) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cands = append(cands, candidate{key: key, mod: info.ModTime()})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mod.After(cands[j].mod) })
	if len(cands) > limit {
		cands = cands[:limit]
	}
	keys := make([]string, 0, len(cands))
	for _, c := range cands {
		keys = append(keys, c.key)
	}
	return keys
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
		if !idleReviewDue(now, live.LastReviewAt, staleAfter, lastAttempt, idleReviewRetryGap) {
			return false, ""
		}
		lastAttempt = now
		staleFor := "never"
		if live.LastReviewAt > 0 {
			staleFor = now.Sub(time.UnixMilli(live.LastReviewAt)).Round(time.Minute).String()
		}
		for _, key := range recentRealSessionKeys(transcriptBaseDir(), idleReviewCandidates) {
			sctx, err := buildSkillLifecycleSessionContext(store, key)
			if err != nil {
				continue
			}
			fired, err := nudger.RunStaleReview(key, sctx)
			if err != nil {
				// runReviewOnce already recorded the failure on liveness; the
				// lane just surfaces it and stops walking candidates this tick.
				s.logger.Warn("idle skill review: fenced review failed", "session", key, "error", err)
				return false, ""
			}
			if fired {
				return true, fmt.Sprintf("session=%s staleFor=%s", key, staleFor)
			}
		}
		s.logger.Debug("idle skill review: no recent session passed the review gate", "staleFor", staleFor)
		return false, ""
	}
}
