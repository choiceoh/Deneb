package genesis

// LadderWatchTask executes graduation-ladder unlocks and surfaces the rest.
//
// Originally notify-only; the operator delegated execution 2026-07-14
// ("잠금 해제도 에이전트에게 맡겨버려. 그래야 재귀적 자기개선이지"): a row
// whose evidence meets its compiled auto-threshold is now UNLOCKED by the
// loop itself — same trust architecture as P2 auto-adoption (evidence-gated
// execution + notification card with a 재잠금 veto + kill switch
// DENEB_AUTO_GRADUATE=0 + the drift self-brake pauses it). Rows whose flip
// is out of the loop's reach (calibration drop-in, manual drill row) keep
// the notify-only READY card.
//
// Semantics: auto-graduation runs first (idempotent — an unlocked row never
// re-fires); the READY card fires once per transition for rows the loop
// cannot or may not execute. A row that falls back and re-earns READY fires
// again — a genuinely new decision moment.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ladderWatchDefaultInterval keeps the watch cheap: ladder evidence moves at
// ledger cadence (hours to days), not seconds.
const ladderWatchDefaultInterval = 6 * time.Hour

// ladderSourceMinSupply is the staged-source auto-graduation floor: at least
// this many open code candidates (proposed or accepted) and none rejected.
// A rejection is a standing veto until superseded. Human first-batch review
// was dropped 2026-07-15 — supply itself is the evidence; dispatch still
// runs propose→PR→deploy-watch before anything lands.
const ladderSourceMinSupply = 1

// autoGraduateEnabled is the kill switch (default ON per the 2026-07-14
// operator directive; set DENEB_AUTO_GRADUATE=0 to revert to notify-only).
func autoGraduateEnabled() bool {
	return os.Getenv("DENEB_AUTO_GRADUATE") != "0"
}

// LadderWatchTask is registered production-gated (it writes shared genesis
// state and posts operator-facing cards).
type LadderWatchTask struct {
	Tracker *Tracker
	Logger  *slog.Logger
	// OnReady surfaces one row whose evidence just reached READY. A delivery
	// error leaves that row below READY in the snapshot so the next run retries.
	OnReady func(title, detail string) error
	// OnGraduated surfaces one EXECUTED unlock (notification + 재잠금 veto).
	OnGraduated func(key, title, evidence string)
}

// Name identifies the task in the autonomous scheduler.
func (t *LadderWatchTask) Name() string { return "ladder-watch" }

// Interval honors DENEB_LADDER_WATCH_INTERVAL_HOURS.
func (t *LadderWatchTask) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_LADDER_WATCH_INTERVAL_HOURS")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return ladderWatchDefaultInterval
}

// ladderWatchStatePath sits next to the tracker ledgers.
func (t *LadderWatchTask) ladderWatchStatePath() string {
	return filepath.Join(filepath.Dir(t.Tracker.logPath), "ladder_watch_state.json")
}

// Run executes evidence-met unlocks, then fires OnReady for fresh READY
// transitions among the remaining rows, and persists the snapshot
// (tmp+rename) so restarts never re-fire.
func (t *LadderWatchTask) Run(_ context.Context) error {
	if t.Tracker == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Auto-graduation first: unlocked rows read 완료 below, so an executed
	// unlock never double-surfaces as a READY decision card. The drift brake
	// pauses execution together with L2 auto-adoption — a loop on a
	// reward-hacking trajectory must not widen its own autonomy.
	if autoGraduateEnabled() && !t.Tracker.AutoAdoptFrozen() {
		for _, g := range t.Tracker.autoGraduations() {
			fresh, err := t.Tracker.unlockGraduation(g.Key, g.Evidence, g.Value, true)
			if err != nil {
				logger.Warn("ladder-watch: graduation unlock failed", "row", g.Key, "error", err)
				continue
			}
			if !fresh {
				continue
			}
			logger.Info("ladder-watch: graduation EXECUTED (evidence-met unlock)",
				"row", g.Key, "evidence", g.Evidence, "value", g.Value)
			if t.OnGraduated != nil {
				t.OnGraduated(g.Key, g.Title, g.Evidence)
			}
		}
	}

	prev := map[string]string{}
	if raw, err := os.ReadFile(t.ladderWatchStatePath()); err == nil {
		_ = json.Unmarshal(raw, &prev) // corrupt snapshot = first-run semantics
	}
	rows := t.Tracker.ladderRows()
	next := make(map[string]string, len(rows))
	for _, r := range rows {
		next[r.Title] = r.State
		if r.State != ladderStateReady || prev[r.Title] == ladderStateReady {
			continue
		}
		logger.Info("ladder-watch: row reached READY — surfacing operator decision",
			"row", r.Title, "detail", r.Detail)
		if t.OnReady == nil {
			logger.Warn("ladder-watch: READY delivery unavailable; transition remains retryable", "row", r.Title)
			next[r.Title] = prev[r.Title]
			continue
		}
		if err := t.OnReady(r.Title, r.Detail); err != nil {
			logger.Warn("ladder-watch: READY delivery failed; transition remains retryable", "row", r.Title, "error", err)
			next[r.Title] = prev[r.Title]
		}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		logger.Warn("ladder-watch: snapshot marshal failed", "error", err)
		return nil
	}
	tmp := t.ladderWatchStatePath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		logger.Warn("ladder-watch: snapshot write failed", "error", err)
		return nil
	}
	if err := os.Rename(tmp, t.ladderWatchStatePath()); err != nil {
		logger.Warn("ladder-watch: snapshot rename failed", "error", err)
	}
	return nil
}

// autoGraduation is one executable unlock whose compiled evidence policy is
// met right now.
type autoGraduation struct {
	Key      string
	Title    string
	Evidence string
	Value    int
}

// autoGraduations evaluates the executable ladder rows against their
// compiled auto-thresholds. The thresholds live HERE and in rsi_ladder.go —
// the loop executes the policy, it never edits it (both files are forbidden
// self-edit surfaces).
func (t *Tracker) autoGraduations() []autoGraduation {
	var out []autoGraduation

	// e-process cutover: the ladder threshold (n>=20, agreement>=90%) IS the
	// ratified policy; ownership flips via the graduation state (the env knob
	// still overrides in both directions).
	if r := t.eProcessCutoverReadiness(); r.Ready && !r.EProcessOwner {
		out = append(out, autoGraduation{
			Key: graduationEProcess, Title: "e-process 컷오버",
			Evidence: fmt.Sprintf("라벨 n=%d·레거시 합치 %.0f%% (임계 n≥%d·%.0f%%)",
				r.Labels, r.AgreementRate*100, eProcessCutoverMinLabels, eProcessCutoverMinAgreement*100),
		})
	}

	// Dispatch cap: latest terminal cohort, with marker outcomes joined to the
	// authoritative attempt lifecycle. Only watch_passed is a landed success;
	// any rollback in the cohort blocks graduation.
	// One rung per graduation, each earned at the rung below: the evidence floor
	// is the current unlock's timestamp, so the cohort that bought this cap can
	// never also buy the next. At the top of the ladder there is nothing to emit.
	capRow := loadGraduationState().Rows[graduationDispatchCap]
	if _, next := graduationDispatchCapRung(capRow); next > 0 {
		floor := int64(0)
		if capRow.Unlocked {
			floor = capRow.UnlockedAt
		}
		evidence, err := t.rsiDispatchEvidenceSince(ladderDispatchMinDecided, floor)
		if err == nil && evidence.Decided >= ladderDispatchMinDecided {
			outcomes, decided, landed := evidence.CohortOutcomes, evidence.Decided, evidence.Landed
			rate := float64(landed) / float64(decided)
			if rate >= ladderDispatchMinLandRate && evidence.RolledBack == 0 {
				out = append(out, autoGraduation{
					Key: graduationDispatchCap, Title: "배차 캡 상향",
					Value: next,
					Evidence: fmt.Sprintf("최근 판정 %d건·랜딩률 %.0f%% (%s)·감시 롤백 0건 → 일일 캡 %d",
						decided, rate*100, rsiOutcomeSummary(outcomes), next),
				})
			}
		}
	}

	// Staged sources: candidate supply (proposed|accepted >= floor, rejected
	// == 0) graduates the source into the dispatch allowlist — no human
	// first-batch endorsement. A rejection remains a standing veto.
	for prefix, st := range t.stagedSourceReviewStats() {
		key := graduationSourceKey(prefix)
		supply := st.proposed + st.accepted
		if graduationUnlocked(key) || st.rejected > 0 || supply < ladderSourceMinSupply {
			continue
		}
		out = append(out, autoGraduation{
			Key: key, Title: "스테이징 소스 졸업: " + prefix,
			Evidence: fmt.Sprintf("스테이징 후보 %d건·기각 0건 (임계 공급≥%d, 사람 리뷰 없음) → 배차 허용목록 편입",
				supply, ladderSourceMinSupply),
		})
	}
	return out
}

// sourceReviewStats aggregates open supply + vetoes for one staged source prefix.
type sourceReviewStats struct{ proposed, accepted, rejected int }

// stagedSourceReviewStats counts open code candidates per NON-dispatchable
// source prefix — the supply evidence auto-graduation consumes.
func (t *Tracker) stagedSourceReviewStats() map[string]sourceReviewStats {
	cands, err := t.RecentSelfCorrectionCandidates("", "", 300)
	if err != nil {
		return nil
	}
	stats := map[string]sourceReviewStats{}
	for _, c := range cands {
		if strings.TrimSpace(c.Scope) != "code" || rsiSourceDispatchable(c.Source) {
			continue
		}
		prefix, _, _ := strings.Cut(strings.TrimSpace(c.Source), ":")
		if rsiCompiledDispatchNamespace(prefix) {
			continue
		}
		if prefix == "" {
			continue
		}
		st := stats[prefix]
		switch normalizeSelfCorrectionStatus(c.Status) {
		case SelfCorrectionStatusProposed:
			st.proposed++
		case SelfCorrectionStatusAccepted:
			st.accepted++
		case selfCorrectionStatusRejected:
			st.rejected++
		}
		stats[prefix] = st
	}
	return stats
}
