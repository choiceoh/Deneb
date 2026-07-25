package genesis

// Graduation-ladder readiness — the roadmap's "Autonomy expands on track
// record, never on calendar" made continuously machine-checked. As of
// 2026-07-13 every evidence stream a ladder row gates on is a readable
// ledger (e-process labels, dispatch-outcome land rate, staged-source
// candidate counts, per-epoch bench samples), yet readiness was re-derived
// by hand each time someone asked. This engine evaluates each row's evidence
// on every status read and surfaces it as a fifth pseudo-layer card
// (Key "GRAD") the existing viewers render without wire changes.
//
// Execution model (operator delegated unlocks 2026-07-14): rows whose
// compiled auto-threshold is met are EXECUTED by the watch
// (ladder_watch.go) with a lifecycle ledger entry and a feed-card 재잠금
// veto; READY marks rows awaiting evidence the loop cannot execute
// (out-of-process flips, manual-drill evidence). The unanimous 2026H1
// principle survives as: the loop exercises the policy, it never edits it —
// this file is a forbidden self-edit surface.

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Ladder row evidence thresholds. Proposal values from the roadmap table —
// operator-tunable by editing the row when the first batches teach otherwise.
const (
	// ladderDispatchMinDecided/MinLandRate mirror the "N dispatches with
	// >=50% land rate" cap-raise row (N proposed as 5).
	ladderDispatchMinDecided  = 5
	ladderDispatchMinLandRate = 0.5
	// ladderCalibrationBenchTarget closes the P5-2 window: "revert when
	// per-epoch bench n>=target". Per-epoch (2026-07-19): evaluator/genesis
	// benches emit a clean sample every cycle (judge degradation pairs, genesis
	// scenarios), but the producer shadow bench only samples when the model
	// returns a scorable body — it declines (Skip) most skip-cycles, so
	// producer accrues far slower. A uniform 10 mis-fit producer's generation
	// rate; matching the target to the epoch's achievable sample rate (5) is
	// honest calibration, not a lowered bar — 5 incumbent-vs-incumbent deltas
	// still estimate the noise band. The producer bench's sample thinness is a
	// separate POST-window fix (touching it mid-window would corrupt the very
	// samples being collected).
	ladderCalibrationBenchTargetDefault  = 10
	ladderCalibrationBenchTargetProducer = 5
)

// ladderCalibrationBenchTargetFor returns the per-epoch bench sample target
// for the P5-2 window close.
func ladderCalibrationBenchTargetFor(epoch string) int {
	if epoch == metaEpochProducer {
		return ladderCalibrationBenchTargetProducer
	}
	return ladderCalibrationBenchTargetDefault
}

// ladderCalibrationOpenedMs is when the operator opened the P5-2 calibration
// window (2026-07-12, rsi-calibration.conf) — bench samples before it belong
// to the default-cadence era and don't count toward closing it.
var ladderCalibrationOpenedMs = time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC).UnixMilli()

// Ladder row states (Korean display text — these live in metric VALUES and
// the diagnosis, not in the layer-state machine keys).
const (
	ladderStateReady   = "준비됨"
	ladderStateGrowing = "축적 중"
	ladderStateManual  = "수동 판단"
	ladderStateDone    = "완료"
)

// ladderRow is one evaluated graduation-ladder row.
type ladderRow struct {
	Title  string
	State  string
	Detail string
}

// ladderRows evaluates every graduation-ladder row — shared by the GRAD card
// (rsiAssessLadder) and the transition watch (LadderWatchTask).
func (t *Tracker) ladderRows() []ladderRow {
	return []ladderRow{
		t.ladderEProcessRow(),
		t.ladderDispatchCapRow(),
		t.ladderStagedSourcesRow(),
		t.ladderCalibrationRow(),
		{
			Title: "소스 자동적용 티어", State: ladderStateManual,
			Detail: "배포 롤백 1회 실전/소방훈련 완주가 증거 — 기계 판독 불가, 운영자 판단 행",
		},
	}
}

// rsiAssessLadder folds the evaluated rows into the pseudo-layer card. Layer
// state: LIVE when any row's evidence is READY (an operator decision is
// actionable now), DATA-GATED otherwise (evidence still accumulating — the
// honest steady state).
func (t *Tracker) rsiAssessLadder() rsiLayer {
	rows := t.ladderRows()
	metrics := make([]rsiMetric, 0, len(rows))
	var ready []string
	for _, r := range rows {
		metrics = append(metrics, rsiMetric{Label: r.Title, Value: r.State})
		if r.State == ladderStateReady {
			ready = append(ready, fmt.Sprintf("%s(%s)", r.Title, r.Detail))
		}
	}
	base := rsiLayer{Key: "GRAD", Title: "졸업 사다리", Metrics: metrics}
	if len(ready) > 0 {
		base.State = rsiStateLive
		base.Diagnosis = "증거 충족 — 운영자 결정 가능: " + strings.Join(ready, " · ")
		return base
	}
	base.State = rsiStateDataGated
	details := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.State == ladderStateGrowing {
			details = append(details, fmt.Sprintf("%s: %s", r.Title, r.Detail))
		}
	}
	base.Diagnosis = "전 행 증거 축적 중 — " + strings.Join(details, " · ")
	return base
}

// ladderEProcessRow: e-process observation → rollback-firing ownership.
func (t *Tracker) ladderEProcessRow() ladderRow {
	r := t.eProcessCutoverReadiness()
	switch {
	case r.EProcessOwner:
		return ladderRow{"e-process 컷오버", ladderStateDone, fmt.Sprintf("발화 소유 중 (라벨 n=%d)", r.Labels)}
	case r.Ready:
		return ladderRow{"e-process 컷오버", ladderStateReady, fmt.Sprintf("n=%d·합치 %.0f%% — DENEB_EPROCESS_OWNS_ROLLBACK=1 결정 가능", r.Labels, r.AgreementRate*100)}
	default:
		// "라벨 0/20" alone reads as "accumulating, be patient" — but a resolved
		// watch that could never have rejected produces an UNFAIR label, and
		// those do not accumulate toward anything. Live 2026-07-25: 2 watches
		// resolved, BOTH unfair (ep.N=6, under MinRejectObservations), so the
		// honest reading was "the input is structurally short", not "it is
		// filling up". This is the DATA-GATED vs STARVED distinction the status
		// engine calls its whole point, applied to a ladder row.
		detail := fmt.Sprintf("라벨 %d/%d", r.Labels, eProcessCutoverMinLabels)
		if r.UnfairLabels > 0 {
			detail += fmt.Sprintf(" · 관측수 미달로 무효 %d건 (사후 실사용이 e-process 기각 가능선에 못 미침)", r.UnfairLabels)
		}
		if r.Labels > 0 && r.FairRollbacks == 0 {
			// Agreement measured on a pure-confirm population is trivially ~1.0
			// (C1-D1), so a row sitting here with labels but no rollback is
			// waiting on a DIFFERENT input than the count suggests.
			detail += " · 공정 롤백 0건 (합치율이 확정만으로 계산돼 의미 없음)"
		}
		if r.RollbacksEver == 0 {
			// The eProcessCutoverMinFairRollbacks comment already concedes this
			// is "structurally near-unreachable" at threshold 3 and that the
			// flip is meant to stay an operator decision. Saying so here is the
			// difference between a reader waiting for evidence and a reader
			// making the call: no evolve has ever regressed, so the rollback
			// half of Ready has no path by waiting. cmd/rsi-backtest is the
			// designed substitute — it replays the archive through BOTH
			// deciders and reports their agreement today.
			detail += " · 롤백 이력 0건 — 대기로는 충족 불가(설계상), 근거는 rsi-backtest·결정은 DENEB_EPROCESS_OWNS_ROLLBACK"
		}
		return ladderRow{"e-process 컷오버", ladderStateGrowing, detail}
	}
}

// ladderDispatchCapRow: daily dispatch cap raise on the latest terminal cohort.
// Marker results are joined to the per-attempt lifecycle, so only watch_passed
// counts as landed and a rollback blocks the same cohort. The row reads 완료
// once the unlock executes.
func (t *Tracker) ladderDispatchCapRow() ladderRow {
	if row := loadGraduationState().Rows[graduationDispatchCap]; row.Unlocked {
		return ladderRow{"배차 캡 상향", ladderStateDone, fmt.Sprintf("실행됨 — 일일 캡 %d (자동 졸업)", row.Value)}
	}
	evidence, err := t.rsiDispatchEvidence(ladderDispatchMinDecided)
	if err != nil {
		return ladderRow{"배차 캡 상향", ladderStateGrowing, "배차 결과 원장을 읽을 수 없음"}
	}
	outcomes, decided, landed := evidence.CohortOutcomes, evidence.Decided, evidence.Landed
	if decided == 0 {
		return ladderRow{"배차 캡 상향", ladderStateGrowing, "판정된 배차 0건"}
	}
	rate := float64(landed) / float64(decided)
	detail := fmt.Sprintf("판정 %d건·랜딩률 %.0f%% (%s)", decided, rate*100, rsiOutcomeSummary(outcomes))
	if decided >= ladderDispatchMinDecided && rate >= ladderDispatchMinLandRate && evidence.RolledBack == 0 {
		return ladderRow{"배차 캡 상향", ladderStateReady, detail + " · 감시 롤백 0건"}
	}
	return ladderRow{"배차 캡 상향", ladderStateGrowing, detail}
}

// ladderStagedSourcesRow: novel L4 sources auto-graduate on candidate supply
// (no human first-batch review). Any still-staged open code candidates mean
// the watch can unlock on its next tick — surface READY so the dashboard
// shows actionable supply before the unlock lands.
func (t *Tracker) ladderStagedSourcesRow() ladderRow {
	cands, err := t.RecentSelfCorrectionCandidates("", "", 300)
	if err != nil {
		return ladderRow{"스테이징 소스 졸업", ladderStateGrowing, "후보 저장소를 읽을 수 없음"}
	}
	bySource := map[string]int{}
	for _, c := range cands {
		st := normalizeSelfCorrectionStatus(c.Status)
		if strings.TrimSpace(c.Scope) != "code" || (st != SelfCorrectionStatusProposed && st != SelfCorrectionStatusAccepted) {
			continue
		}
		if rsiSourceDispatchable(c.Source) {
			continue
		}
		prefix, _, _ := strings.Cut(c.Source, ":")
		if prefix == "" {
			prefix = "(no source)"
		}
		bySource[prefix]++
	}
	if len(bySource) == 0 {
		return ladderRow{"스테이징 소스 졸업", ladderStateGrowing, "스테이징 후보 0건 (마이너 대기)"}
	}
	parts := make([]string, 0, len(bySource))
	for src, n := range bySource {
		parts = append(parts, fmt.Sprintf("%s %d건", src, n))
	}
	sort.Strings(parts)
	return ladderRow{"스테이징 소스 졸업", ladderStateReady, "공급 충족 → 자동 졸업 대기: " + strings.Join(parts, "·")}
}

// ladderCalibrationRow: the P5-2 window closes when every rotating epoch has
// accumulated the target bench sample count since the window opened.
func (t *Tracker) ladderCalibrationRow() ladderRow {
	revs, err := t.RecentMetaRevisions(200)
	if err != nil {
		return ladderRow{"캘리브레이션 창 종료", ladderStateGrowing, "메타 원장을 읽을 수 없음"}
	}
	benched := map[string]int{}
	for _, r := range revs {
		// Cycle records carry a non-empty Epoch and the per-epoch bench; adoption-
		// lifecycle records (empty Epoch) don't. Key the skip off Epoch, not Action
		// — an auto_adopted cycle stamps Action="auto_adopted" on the cycle record,
		// so skipping Action!="" dropped exactly the benched cycles we must count.
		if r.CreatedAt < ladderCalibrationOpenedMs || r.Epoch == "" {
			continue
		}
		if r.BenchIncumbent != nil || r.BenchShadow != nil || r.BenchGenesis != nil {
			benched[r.Epoch]++
		}
	}
	detail := fmt.Sprintf("epoch별 벤치 n: producer %d/%d·evaluator %d/%d·genesis %d/%d",
		benched[metaEpochProducer], ladderCalibrationBenchTargetFor(metaEpochProducer),
		benched[metaEpochEvaluator], ladderCalibrationBenchTargetFor(metaEpochEvaluator),
		benched[metaEpochGenesis], ladderCalibrationBenchTargetFor(metaEpochGenesis))
	for _, epoch := range []string{metaEpochProducer, metaEpochEvaluator, metaEpochGenesis} {
		if benched[epoch] < ladderCalibrationBenchTargetFor(epoch) {
			return ladderRow{"캘리브레이션 창 종료", ladderStateGrowing, detail}
		}
	}
	return ladderRow{"캘리브레이션 창 종료", ladderStateReady, detail + " — rsi-calibration.conf 제거 결정 가능"}
}

// rsiOutcomeSummary formats an outcome histogram deterministically.
func rsiOutcomeSummary(outcomes map[string]int) string {
	keys := make([]string, 0, len(outcomes))
	for k := range outcomes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, outcomes[k]))
	}
	return strings.Join(parts, "·")
}
