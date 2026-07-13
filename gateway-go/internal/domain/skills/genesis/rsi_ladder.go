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
// Invariant: the engine NEVER flips a lock. A row at READY means "evidence
// thresholds met — the decision is now available to the operator"; the flip
// itself stays a human action (allowlist edit, env knob, drop-in removal),
// per the unanimous 2026H1 acceptance principle.

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
	// per-epoch bench n>=10".
	ladderCalibrationBenchTarget = 10
)

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

// rsiAssessLadder evaluates every machine-checkable graduation-ladder row and
// folds them into the pseudo-layer card. Layer state: LIVE when any row's
// evidence is READY (an operator decision is actionable now), DATA-GATED
// otherwise (evidence still accumulating — the honest steady state).
func (t *Tracker) rsiAssessLadder() RSILayer {
	rows := []ladderRow{
		t.ladderEProcessRow(),
		t.ladderDispatchCapRow(),
		t.ladderStagedSourcesRow(),
		t.ladderCalibrationRow(),
		{
			Title: "소스 자동적용 티어", State: ladderStateManual,
			Detail: "배포 롤백 1회 실전/소방훈련 완주가 증거 — 기계 판독 불가, 운영자 판단 행",
		},
	}
	metrics := make([]RSIMetricKV, 0, len(rows))
	var ready []string
	for _, r := range rows {
		metrics = append(metrics, RSIMetricKV{r.Title, r.State})
		if r.State == ladderStateReady {
			ready = append(ready, fmt.Sprintf("%s(%s)", r.Title, r.Detail))
		}
	}
	base := RSILayer{Key: "GRAD", Title: "졸업 사다리", Metrics: metrics}
	if len(ready) > 0 {
		base.State = RSIStateLive
		base.Diagnosis = "증거 충족 — 운영자 결정 가능: " + strings.Join(ready, " · ")
		return base
	}
	base.State = RSIStateDataGated
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
	r := t.EProcessCutoverReadiness()
	switch {
	case r.EProcessOwner:
		return ladderRow{"e-process 컷오버", ladderStateDone, fmt.Sprintf("발화 소유 중 (라벨 n=%d)", r.Labels)}
	case r.Ready:
		return ladderRow{"e-process 컷오버", ladderStateReady, fmt.Sprintf("n=%d·합치 %.0f%% — DENEB_EPROCESS_OWNS_ROLLBACK=1 결정 가능", r.Labels, r.AgreementRate*100)}
	default:
		return ladderRow{"e-process 컷오버", ladderStateGrowing, fmt.Sprintf("라벨 %d/20", r.Labels)}
	}
}

// ladderDispatchCapRow: daily dispatch cap raise on measured land rate.
// Deploy-watch rollbacks are not ledgered yet, so the "0 rollbacks" half of
// the row stays a manual confirmation — the detail says so instead of
// silently claiming it.
func (t *Tracker) ladderDispatchCapRow() ladderRow {
	outcomes, decided, landed := rsiDispatchOutcomes(t.dispatchMarkerDir())
	if decided == 0 {
		return ladderRow{"배차 캡 상향", ladderStateGrowing, "판정된 배차 0건"}
	}
	rate := float64(landed) / float64(decided)
	detail := fmt.Sprintf("판정 %d건·랜딩률 %.0f%% (%s)", decided, rate*100, rsiOutcomeSummary(outcomes))
	if decided >= ladderDispatchMinDecided && rate >= ladderDispatchMinLandRate {
		return ladderRow{"배차 캡 상향", ladderStateReady, detail + " — 롤백 0건은 수동 확인 후 캡 결정"}
	}
	return ladderRow{"배차 캡 상향", ladderStateGrowing, detail}
}

// ladderStagedSourcesRow: staged L4 sources graduate on a clean first-batch
// review — candidates existing IS the actionable evidence (the review can
// happen now), so any staged supply reads READY-to-review.
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
	return ladderRow{"스테이징 소스 졸업", ladderStateReady, "첫 배치 리뷰 가능: " + strings.Join(parts, "·")}
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
		if r.CreatedAt < ladderCalibrationOpenedMs || r.Action != "" {
			continue
		}
		if r.BenchIncumbent != nil || r.BenchShadow != nil || r.BenchGenesis != nil {
			benched[r.Epoch]++
		}
	}
	detail := fmt.Sprintf("epoch별 벤치 n: producer %d·evaluator %d·genesis %d (목표 각 %d)",
		benched[metaEpochProducer], benched[metaEpochEvaluator], benched[metaEpochGenesis], ladderCalibrationBenchTarget)
	for _, epoch := range []string{metaEpochProducer, metaEpochEvaluator, metaEpochGenesis} {
		if benched[epoch] < ladderCalibrationBenchTarget {
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
