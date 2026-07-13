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

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// ladderWatchDefaultInterval keeps the watch cheap: ladder evidence moves at
// ledger cadence (hours to days), not seconds.
const ladderWatchDefaultInterval = 6 * time.Hour

// ladderSourceMinEndorsed is the review-lane endorsement floor that
// auto-graduates a staged dispatch source: at least this many of its
// candidates review-ACCEPTED and none rejected (an operator/review veto
// blocks the source until the rejection is superseded — mirrors the
// reopen semantics). "Candidates exist" alone stays notify-only.
const ladderSourceMinEndorsed = 2

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

	// Dispatch cap: measured land rate over decided dispatches AND zero
	// ledgered deploy-watch rollbacks — the full roadmap-row evidence, now
	// machine-readable end to end.
	if !graduationUnlocked(graduationDispatchCap) {
		outcomes, decided, landed := rsiDispatchOutcomes(t.dispatchMarkerDir())
		if decided >= ladderDispatchMinDecided {
			rate := float64(landed) / float64(decided)
			if rollbacks := deployWatchRollbacks(); rate >= ladderDispatchMinLandRate && rollbacks == 0 {
				out = append(out, autoGraduation{
					Key: graduationDispatchCap, Title: "배차 캡 상향",
					Value: graduationDispatchCapStep,
					Evidence: fmt.Sprintf("판정 %d건·랜딩률 %.0f%% (%s)·배포 롤백 0건 → 일일 캡 %d",
						decided, rate*100, rsiOutcomeSummary(outcomes), graduationDispatchCapStep),
				})
			}
		}
	}

	// Staged sources: review-lane endorsement (accepted >= floor, rejected
	// == 0) graduates the source into the dispatch allowlist. A rejection is
	// a standing veto until superseded.
	for prefix, st := range t.stagedSourceReviewStats() {
		key := graduationSourceKey(prefix)
		if graduationUnlocked(key) || st.rejected > 0 || st.accepted < ladderSourceMinEndorsed {
			continue
		}
		out = append(out, autoGraduation{
			Key: key, Title: "스테이징 소스 졸업: " + prefix,
			Evidence: fmt.Sprintf("리뷰 승인 %d건·기각 0건 (임계 승인≥%d) → 배차 허용목록 편입", st.accepted, ladderSourceMinEndorsed),
		})
	}
	return out
}

// sourceReviewStats aggregates review verdicts for one staged source prefix.
type sourceReviewStats struct{ accepted, rejected int }

// stagedSourceReviewStats counts review-lane verdicts per NON-dispatchable
// code-source prefix — the endorsement evidence auto-graduation consumes.
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
		if prefix == "" {
			continue
		}
		st := stats[prefix]
		switch normalizeSelfCorrectionStatus(c.Status) {
		case SelfCorrectionStatusAccepted:
			st.accepted++
		case SelfCorrectionStatusRejected:
			st.rejected++
		}
		stats[prefix] = st
	}
	return stats
}

// deployWatchRollbacks counts ledgered post-hot-swap rollbacks
// (deploy-watch.sh appends one row per fired rollback). An absent ledger
// reads 0 — coverage is complete from the first dispatch because the ledger
// ships before dispatches begin.
func deployWatchRollbacks() int {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0
	}
	type row struct {
		Event string `json:"event"`
	}
	rows, err := jsonlstore.Load[row](filepath.Join(home, ".deneb", "data", "deploy_watch_log.jsonl"))
	if err != nil {
		return 0
	}
	n := 0
	for _, r := range rows {
		if r.Event == "rollback" {
			n++
		}
	}
	return n
}
