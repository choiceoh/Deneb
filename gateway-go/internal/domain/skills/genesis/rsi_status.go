package genesis

// RSI loop-status computation serves the native, Andromeda, and audit viewers
// over the miniapp.rsi.status RPC. This Go path is the only layer classifier;
// scripts/audit/rsi_status.py validates and formats the resulting read model.
//
// The four operational layers use these honest states:
//
//	LIVE        producing AND consuming — the loop is turning.
//	DATA-GATED  built and running, waiting for fuel to accumulate (NOT a defect).
//	STARVED     built, but its input source is empty (a wiring gap).
//	FROZEN      the drift self-brake halted auto-adoption.
//	IDLE        no recent activity.
//
// The DATA-GATED vs STARVED distinction is the whole point: the first is the
// correct state of a young loop with no data yet; the second is an actionable
// gap. A naive "0 events" count conflates them.
//
// Display text is Korean-first (this feeds the user-facing viewers); the State
// value stays an English enum because it is a machine key the clients map to a
// color and a localized badge label.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
	rsistatus "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/status"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// Private aliases keep the engine implementation concise while exposing the
// stable status read model from the narrow status subpackage.
type (
	// RSILoopStatus keeps the established handler port stable while the full
	// read model lives in the narrow status package.
	RSILoopStatus = rsistatus.LoopStatus
	rsiLoopStatus = rsistatus.LoopStatus
	rsiHealth     = rsistatus.Health
	rsiLayer      = rsistatus.Layer
	rsiMetric     = rsistatus.Metric
)

const (
	rsiStateLive      = rsistatus.StateLive
	rsiStateDataGated = rsistatus.StateDataGated
	rsiStateStarved   = rsistatus.StateStarved
	rsiStateFrozen    = rsistatus.StateFrozen
	rsiStateIdle      = rsistatus.StateIdle
)

// rsiSubtleDegradationClasses are the judge-degradation classes that actually
// produce labeled misses (P3 fuel); a ledger with only blatant classes is
// data-gated, not broken. Includes the escalated weaken tier (probe
// curriculum ladder — deployed only after the drop tier saturates).
var rsiSubtleDegradationClasses = map[string]bool{
	"imperative-drop": true, "safety-drop": true,
	"imperative-weaken": true, "scope-narrow": true,
	"exclusivity-drop": true,
}

// rsiWeakenDegradationClasses is the escalated tier subset — seeing one in
// the ledger means the lane already probes above the drop tier. It includes
// tier 4 so the "at the ceiling" diagnosis keeps meaning the CURRENT top rung
// rather than whichever rung was top when it was written.
var rsiWeakenDegradationClasses = map[string]bool{
	"imperative-weaken": true, "scope-narrow": true,
	"exclusivity-drop": true,
}

// rsiDispatchSources is the canonical set of accepted candidate sources: a code
// candidate from any other source is not yet dispatchable until the graduation
// ladder admits it (or it is added here). health-finding graduated 2026-07-12;
// tool-quality 2026-07-13; runtime-error and deadcode-finding 2026-07-15
// (operator directive: drop the first-batch human-review gate — miners already
// ground + forbidden-surface filter, and dispatch keeps propose→PR→deploy-watch).
// Dispatch selection, status, and client projection all call this Go policy.
var rsiDispatchSources = []string{
	"evolve-tool-gap", "self-harness", "health-finding", "tool-quality",
	"runtime-error", "deadcode-finding",
}

// SourceAutoDispatches reports whether a self-correction candidate from this
// source is on the auto-dispatch track (compiled allowlist or ladder-graduated)
// vs still staged. Exported for the miniapp wire projection so clients can
// label each candidate 자동수리 vs 검토 대기.
func SourceAutoDispatches(source string) bool { return rsiSourceDispatchable(source) }

const rsiGraduationDetail = "자율성 졸업 사다리의 행별 증거를 상시 심사하고, 임계 충족 시 잠금 해제를 자동 실행하는 계기판입니다 (2026-07-14 위임). 모든 실행은 원장 기록과 재잠금 비토 카드를 남기며, 임계값 정책 자체는 루프가 편집할 수 없습니다."

func newRSILayer(layer rsilifecycle.Layer, metrics []rsiMetric) rsiLayer {
	identity, ok := rsilifecycle.IdentityFor(layer)
	if !ok {
		return rsiLayer{Key: string(layer), Metrics: metrics}
	}
	return rsiLayer{
		Key:     string(identity.Layer),
		Title:   identity.Title,
		Detail:  identity.Detail,
		Metrics: metrics,
	}
}

// RSIStatus composes the four layer assessments from the tracker's public
// aggregates. It takes no lock of its own — each aggregate locks internally.
func (t *Tracker) RSIStatus() rsistatus.LoopStatus {
	layers := []rsiLayer{t.rsiAssessL1(), t.rsiAssessL2(), t.rsiAssessL3(), t.rsiAssessL4(), t.rsiAssessLadder()}
	turning := 0
	for i := range layers {
		// The graduation-ladder pseudo-layer is an evidence dashboard, not a
		// loop — it never counts toward the "N/4 turning" headline.
		if layers[i].Key == "GRAD" {
			layers[i].Detail = rsiGraduationDetail
			continue
		}
		if layers[i].State == rsiStateLive || layers[i].State == rsiStateFrozen {
			turning++
		}
	}
	eh := t.EvolutionHealth()
	meta := t.MetaEvolutionHealth()
	return rsiLoopStatus{
		Layers:  layers,
		Turning: turning,
		Health: rsiHealth{
			Evolves7d:         eh.Evolves7d,
			Confirmed7d:       eh.EvolveConfirmed7d,
			Rejected7d:        eh.EvolveRejected7d,
			RolledBack7d:      eh.EvolveRolledBack7d,
			Genesis7d:         eh.Genesis7d,
			ConfirmRate:       eh.ConfirmRate,
			FalseAcceptRate:   eh.FalseAcceptRate,
			ResolvedEvolves7d: eh.ResolvedEvolves7d,
			Thrash:            eh.Thrash,
			AutoAdoptFrozen:   t.AutoAdoptFrozen(),
			MetaRevisions7d:   meta.Revisions7d,
		},
	}
}

func (t *Tracker) rsiAssessL1() rsiLayer {
	h := t.EvolutionHealth()
	committed := h.Evolves7d + h.Genesis7d
	metrics := []rsiMetric{
		{Label: "진화(7일)", Value: strconv.Itoa(h.Evolves7d)},
		{Label: "신규 스킬", Value: strconv.Itoa(h.Genesis7d)},
		{Label: "제안", Value: strconv.Itoa(h.Proposals7d)},
		{Label: "기각", Value: strconv.Itoa(h.EvolveRejected7d)},
		// "판정자 장애" read as "the judge is broken". It counts rejections that were
		// OUTAGES (the judge call itself failed) rather than verdicts, which is why
		// it sits beside 기각 instead of inside it.
		{Label: "판정 불능", Value: strconv.Itoa(h.EvolveRejectedInfra7d)},
		// Carry the denominator. 확정률 is confirmed/(confirmed+rolled back) — only
		// evolutions that SHIPPED and then resolved — so a bare "100%" sitting next
		// to "기각 10" reads as a contradiction when the two count entirely different
		// populations (a 기각 never shipped, so it can never be in this rate).
		{Label: "확정률", Value: rsiConfirmRateValue(h.ConfirmRate, h.EvolveConfirmed7d, h.ResolvedEvolves7d)},
		// The metric is who fires rollback, not the name of a subsystem.
		{Label: "롤백 발화", Value: rsiEProcessValue(t.eProcessCutoverReadiness())},
		// Live-use labels call the skill a success while its own held-out cases
		// fail — a disagreement between two label sources, not a person's blind spot.
		{Label: "라벨 불일치", Value: strconv.Itoa(len(t.labelerBlindSpots(evolutionHealthWindow)))},
	}
	base := newRSILayer(rsilifecycle.LayerL1, metrics)
	switch {
	case committed > 0:
		base.State = rsiStateLive
		base.Diagnosis = fmt.Sprintf("이번 주 진화 %d · 신규 스킬 %d · 제안 %d · 기각 %d", h.Evolves7d, h.Genesis7d, h.Proposals7d, h.EvolveRejected7d)
	case h.EvolveRejected7d > 0 || h.Proposals7d > 0:
		// Proposals/rejections without commits = the lane is alive but gated
		// (Python rsi_status assess_l1 parity). Counting only rejects previously
		// left proposal-only weeks looking IDLE.
		base.State = rsiStateDataGated
		base.Diagnosis = fmt.Sprintf("제안 %d · 기각 %d — 후보는 있지만 이번 주 게이트를 통과한 진화가 없습니다", h.Proposals7d, h.EvolveRejected7d)
	default:
		base.State = rsiStateIdle
		base.Diagnosis = "최근 7일간 스킬 진화 활동이 없습니다"
	}
	return base
}

func (t *Tracker) rsiAssessL2() rsiLayer {
	// Scoreboard stays on the 7d MetaEvolutionHealth window; LIVE/IDLE for the
	// slow loop uses a 14d look-back (Python rsi_status assess_l2 parity — the
	// weekly cadence would otherwise flip IDLE mid-week after a quiet 7d).
	h := t.MetaEvolutionHealth()
	metrics := []rsiMetric{
		{Label: "개정(7일)", Value: strconv.Itoa(h.Revisions7d)},
		{Label: "제안(7일)", Value: strconv.Itoa(h.Proposed7d)},
	}
	if strings.TrimSpace(h.LastEpoch) != "" {
		metrics = append(metrics, rsiMetric{Label: "최근 에폭", Value: h.LastEpoch})
	}
	// L1.5-trap telemetry (advisory): structural vs parametric mix of recent
	// proposals, plus the consecutive-parametric-adoption streak once it hits
	// the nudge threshold — all-parametric adoption is the regime Bilevel
	// Autoresearch (2603.23420) measured as a null result.
	bal := t.MetaRevisionClassBalance()
	if bal.Structural+bal.Parametric > 0 {
		metrics = append(metrics, rsiMetric{Label: "구조형·파라미터형", Value: fmt.Sprintf("%d·%d", bal.Structural, bal.Parametric)})
		if bal.AdoptedParametricStreak >= metaParametricStreakNudge {
			metrics = append(metrics, rsiMetric{Label: "연속 파라미터형 채택", Value: strconv.Itoa(bal.AdoptedParametricStreak)})
		}
	}
	base := newRSILayer(rsilifecycle.LayerL2, metrics)
	switch {
	case t.AutoAdoptFrozen():
		base.State = rsiStateFrozen
		base.Diagnosis = "드리프트 자기 브레이크 작동 — 자동 채택이 제안 전용으로 동결됐습니다"
	case t.metaActivityIn(metaEvolutionAssessWindow):
		base.State = rsiStateLive
		// Diagnosis uses the SAME 14d window as LIVE/IDLE — quoting 7d scoreboard
		// numbers here made a 10d-old weekly cycle read as "0 revisions" while
		// the layer stayed LIVE (Python assess_l2 prints 14d counts).
		cycles, proposed, adopted, reverted := t.metaCycleCountsIn(metaEvolutionAssessWindow)
		diag := fmt.Sprintf("%d 사이클 / %d 제안 / %d 채택 / %d 되돌림 (14일)",
			cycles, proposed, adopted, reverted)
		if strings.TrimSpace(h.LastEpoch) != "" {
			diag += fmt.Sprintf(" · 최근 %s", h.LastEpoch)
		}
		base.Diagnosis = diag
	default:
		base.State = rsiStateIdle
		base.Diagnosis = "최근 14일간 슬로우 루프 사이클이 없습니다 — 주간 주기를 기다리는 중"
	}
	return base
}

// metaEvolutionAssessWindow is the L2 LIVE/IDLE look-back (2× the 7d health
// window). Weekly cadence means a quiet mid-week 7d slice must not erase a
// revision from last week.
const metaEvolutionAssessWindow = 14 * 24 * time.Hour

func (t *Tracker) metaActivityIn(window time.Duration) bool {
	cycles, proposed, adopted, reverted := t.metaCycleCountsIn(window)
	return cycles+proposed+adopted+reverted > 0
}

// metaCycleCountsIn tallies slow-loop ledger rows inside window
// (action=="" → cycle; proposed flag; adopted/reverted action rows).
func (t *Tracker) metaCycleCountsIn(window time.Duration) (cycles, proposed, adopted, reverted int) {
	entries, err := t.RecentMetaRevisions(50)
	if err != nil || len(entries) == 0 {
		return 0, 0, 0, 0
	}
	cutoff := time.Now().Add(-window).UnixMilli()
	for _, e := range entries {
		if e.CreatedAt < cutoff {
			continue
		}
		switch e.Action {
		case "auto_adopted", "adopted":
			adopted++
		case "auto_reverted", "operator_reverted":
			reverted++
		case "":
			cycles++
			if e.Proposed {
				proposed++
			}
		}
	}
	return cycles, proposed, adopted, reverted
}

func (t *Tracker) rsiAssessL3() rsiLayer {
	records, err := t.recentJudgeAccuracy(20)
	operatorLabels := len(t.RecentOperatorJudgeVerdicts(7*24*time.Hour, 100))
	if err != nil || (len(records) == 0 && operatorLabels == 0) {
		base := newRSILayer(rsilifecycle.LayerL3, nil)
		base.State, base.Diagnosis = rsiStateIdle, "판정 정확도 레인이 아직 실행되지 않았습니다"
		return base
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	runs, misses, falseRejects := 0, 0, 0
	subtleDeployed, weakenDeployed := false, false
	for _, r := range records {
		if r.CreatedAt < cutoff {
			continue
		}
		if !judgeAccuracyProbeUsable(r) {
			continue // restart/warmup error storms are not L3 fuel
		}
		runs++
		for _, m := range r.Misses {
			if judgeMissCountsAsFuel(m) {
				misses++
			}
		}
		// Outage-born exhibits are recoverable candidates, not judge
		// miscalibration — counting them here would make a provider failure
		// read as "the judge is getting worse" (#4239 lineage).
		falseRejects += len(judgeMiscalibrationExhibits(r.FalseRejects))
		for cls := range r.ByClass {
			if rsiSubtleDegradationClasses[cls] {
				subtleDeployed = true
			}
			if rsiWeakenDegradationClasses[cls] {
				weakenDeployed = true
			}
		}
	}
	if runs == 0 && operatorLabels == 0 {
		base := newRSILayer(rsilifecycle.LayerL3, nil)
		base.State, base.Diagnosis = rsiStateIdle, "판정 정확도 레인이 최근 7일간 실행되지 않았습니다"
		return base
	}
	organic := len(t.organicFalseAccepts(organicFalseAcceptWindow, 50))
	metrics := []rsiMetric{
		{Label: "실행(7일)", Value: strconv.Itoa(runs)},
		{Label: "판정 놓침", Value: strconv.Itoa(misses)},
		{Label: "오기각", Value: strconv.Itoa(falseRejects)},
		{Label: "실전 라벨(30일)", Value: strconv.Itoa(organic)},
		{Label: "운영자 라벨(7일)", Value: strconv.Itoa(operatorLabels)},
	}
	base := newRSILayer(rsilifecycle.LayerL3, metrics)
	switch {
	case misses > 0 || falseRejects > 0 || organic > 0 || operatorLabels > 0:
		base.State = rsiStateLive
		base.Diagnosis = fmt.Sprintf("%d회 실행에서 판정 놓침 %d + 오기각 %d + 실전 라벨 %d + 운영자 라벨 %d — P3 학습 연료 축적 중", runs, misses, falseRejects, organic, operatorLabels)
	case !subtleDeployed:
		base.State = rsiStateDataGated
		base.Diagnosis = fmt.Sprintf("%d회 실행; 판정자가 명백한 결함은 모두 잡았고 미묘 프로브는 아직 원장에 없습니다", runs)
	case weakenDeployed:
		base.State = rsiStateDataGated
		base.Diagnosis = fmt.Sprintf("격상된 약화 프로브까지 %d회 실행 모두 잡았습니다 — 판정자가 현행 프로브 최고 티어에서 강합니다", runs)
	default:
		base.State = rsiStateDataGated
		base.Diagnosis = fmt.Sprintf("미묘 프로브가 있는 %d회 실행이지만 아직 놓침이 없습니다 — 판정자가 강하며, 포화 %d회 연속이면 약화 프로브로 격상됩니다", runs, judgeEscalationWindow)
	}
	return base
}

func (t *Tracker) rsiAssessL4() rsiLayer {
	cands, err := t.allSelfCorrectionCandidates()
	if err != nil {
		base := newRSILayer(rsilifecycle.LayerL4, nil)
		base.State, base.Diagnosis = rsiStateIdle, "후보 저장소를 읽을 수 없습니다"
		return base
	}
	tally := t.tallyL4Candidates(cands)
	_, dispatchedToday := t.codingDispatchCounts()
	runtime := t.codingDispatchRuntimeStatus()
	base := newRSILayer(rsilifecycle.LayerL4, rsiL4Metrics(tally, len(cands), dispatchedToday, runtime))
	base.State, base.Diagnosis = rsiL4Verdict(tally, len(cands), runtime)
	// Dispatch-outcome history (graduation-ladder evidence: the cap-raise row
	// needs a measured land rate) rides the diagnosis text — no new metric row,
	// so the native card layout is untouched.
	if note := t.rsiDispatchOutcomeNote(); note != "" {
		base.Diagnosis += note
	}
	return base
}

// l4Tally is the phase/status census of code candidates feeding the L4 card.
type l4Tally struct {
	byScope         map[string]int
	dispatchable    int
	staged          int
	inFlight        int
	applied         int
	declined        int
	failed          int
	impactPending   int
	impactVerified  int
	impactNoEffect  int
	impactRegressed int
	strategyBlocked int
	failureBlocked  int
	oldestPendingAt int64
}

func (l *l4Tally) markPending(createdAt int64) {
	l.dispatchable++
	if createdAt > 0 && (l.oldestPendingAt == 0 || createdAt < l.oldestPendingAt) {
		l.oldestPendingAt = createdAt
	}
}

func (l *l4Tally) markDispatchCandidate(
	record SelfCorrectionCandidateRecord,
	impactPolicies map[string]*selfCorrectionImpactPolicy,
) bool {
	if !SelfCorrectionDispatchEligible(record) {
		return false
	}
	if selfCorrectionImpactPolicyAllows(record, impactPolicies) {
		l.markPending(record.CreatedAt)
	} else {
		l.strategyBlocked++
	}
	return true
}

func (t *Tracker) tallyL4Candidates(cands []SelfCorrectionCandidateRecord) l4Tally {
	tally := l4Tally{byScope: map[string]int{}}
	impactPolicies := selfCorrectionImpactPolicies(mergeSelfCorrectionRecords(cands))
	for _, c := range cands {
		scope := strings.TrimSpace(c.Scope)
		if scope == "" {
			scope = "?"
		}
		tally.byScope[scope]++
		if scope != "code" {
			continue
		}
		// proposed = unreviewed backlog; accepted = review-endorsed, awaiting
		// implementation — both are queued dispatch supply (the heartbeat review
		// lane accepts candidates it cannot implement itself).
		review := rsilifecycle.NormalizeReview(c.Status)
		queued := rsilifecycle.CanDispatch(review, "")
		phase := rsilifecycle.NormalizeDelivery(c.DispatchPhase)
		switch rsilifecycle.ClassifyDelivery(phase) {
		case rsilifecycle.DeliveryInFlight:
			tally.inFlight++
		case rsilifecycle.DeliveryVerified:
			tally.applied++
			switch selfCorrectionImpactStatus(c) {
			case selfCorrectionImpactPending:
				tally.impactPending++
			case selfCorrectionImpactVerified:
				tally.impactVerified++
			case selfCorrectionImpactNoEffect:
				tally.impactNoEffect++
			case selfCorrectionImpactRegressed:
				tally.impactRegressed++
			}
		case rsilifecycle.DeliverySafeNoop:
			// A clean session with no diff is a terminal, healthy no-op. It is
			// neither in flight nor a failed dispatch.
			tally.declined++
		case rsilifecycle.DeliveryRetryable:
			// Compatibility for sessions completed before the declined lifecycle
			// phase shipped: the marker was correct, but the old shell wrote failed.
			if phase == rsilifecycle.DeliveryFailed && t.dispatchMarkerOutcome(c.ID) == "declined" {
				tally.declined++
				continue
			}
			tally.failed++
			if phase == rsilifecycle.DeliveryRolledBack || !t.DispatchMarkerBlocks(c.ID) {
				// A candidate the marker would re-dispatch but which has failed to
				// land too many times is withheld for operator review, not silently
				// dropped from the dispatchable count.
				if selfCorrectionDispatchFailureCapReached(c) {
					tally.failureBlocked++
				} else {
					tally.markDispatchCandidate(c, impactPolicies)
				}
			}
		case rsilifecycle.DeliveryQueued:
			if !queued {
				continue
			}
			if !tally.markDispatchCandidate(c, impactPolicies) {
				// Code candidate from a source not yet in the dispatch
				// allowlist (runtime-error, …): real L4 supply staged for
				// review, not a wiring gap.
				tally.staged++
			}
		}
	}
	return tally
}

func rsiL4Metrics(tally l4Tally, total, dispatchedToday int, runtime codingDispatchRuntime) []rsiMetric {
	return []rsiMetric{
		{Label: "후보", Value: strconv.Itoa(total)},
		{Label: "코드 후보", Value: strconv.Itoa(tally.byScope["code"])},
		{Label: "배차 가능", Value: strconv.Itoa(tally.dispatchable)},
		{Label: "진행 중", Value: strconv.Itoa(tally.inFlight)},
		{Label: "감시 통과", Value: strconv.Itoa(tally.applied)},
		{Label: "효과 판정", Value: rsiL4ImpactValue(tally)},
		{Label: "전략 변경 필요", Value: strconv.Itoa(tally.strategyBlocked)},
		{Label: "반복 실패 보류", Value: strconv.Itoa(tally.failureBlocked)},
		{Label: "안전 종료", Value: strconv.Itoa(tally.declined)},
		{Label: "실패/롤백", Value: strconv.Itoa(tally.failed)},
		{Label: "검토 대기(비배차)", Value: strconv.Itoa(tally.staged)},
		{Label: "오늘 배차", Value: strconv.Itoa(dispatchedToday)},
		{Label: "배차 틱", Value: rsiDispatchTickValue(runtime)},
		{Label: "연속 배차 실패", Value: strconv.Itoa(runtime.ConsecutiveFailures)},
		{Label: "최근 성공", Value: rsiAgeValue(runtime.LastSuccessfulAtMs)},
		{Label: "최장 대기", Value: rsiAgeValue(tally.oldestPendingAt)},
	}
}

func rsiL4ImpactValue(tally l4Tally) string {
	total := tally.impactPending + tally.impactVerified + tally.impactNoEffect + tally.impactRegressed
	if total == 0 {
		return "계약 후보 없음"
	}
	return fmt.Sprintf(
		"확인 %d · 대기 %d · 효과없음 %d · 악화 %d",
		tally.impactVerified,
		tally.impactPending,
		tally.impactNoEffect,
		tally.impactRegressed,
	)
}

func rsiL4Verdict(tally l4Tally, total int, runtime codingDispatchRuntime) (string, string) {
	switch {
	case tally.inFlight > 0:
		return rsiStateLive, fmt.Sprintf("코드 후보 %d건이 PR·배포·롤백 감시 단계를 통과 중", tally.inFlight)
	case tally.applied > 0:
		return rsiStateLive, fmt.Sprintf("소스 자가편집 %d건이 머지·배포 후 롤백 감시까지 통과", tally.applied)
	case tally.declined > 0:
		return rsiStateLive, fmt.Sprintf("코드 후보 %d건을 안전하게 변경 없음으로 종결", tally.declined)
	case tally.dispatchable > 0 && runtime.ConsecutiveFailures > 0:
		return rsiStateStarved, fmt.Sprintf("배차 대기 %d건 · 디스패처 %d회 연속 실패 (%s)", tally.dispatchable, runtime.ConsecutiveFailures, rsiDispatchTickValue(runtime))
	case tally.dispatchable > 0:
		return rsiStateIdle, fmt.Sprintf("배차 대기 %d건 · 아직 진행 중인 authoritative dispatch 없음 (%s)", tally.dispatchable, rsiDispatchTickValue(runtime))
	case tally.strategyBlocked > 0:
		return rsiStateStarved, fmt.Sprintf("효과없음·악화가 반복된 코드 후보 %d건 — 이전과 다른 수정 전략을 명시해야 재배차됩니다", tally.strategyBlocked)
	case tally.failureBlocked > 0:
		return rsiStateStarved, fmt.Sprintf("무인 세션이 %d회 이상 완료하지 못해 보류된 코드 후보 %d건 — 운영자 검토가 필요합니다", maxSelfCorrectionDispatchFailures, tally.failureBlocked)
	case total == 0:
		return rsiStateIdle, "아직 캡처된 자기교정 후보가 없습니다"
	case tally.staged > 0:
		return rsiStateStarved, fmt.Sprintf("비배차 소스의 코드 후보 %d건이 검토 대기 중 — 품질 리뷰 후 배차 소스로 졸업하면 배차됩니다", tally.staged)
	default:
		return rsiStateStarved, fmt.Sprintf("후보 %d건(%s)이지만 배차 가능한 코드 후보가 아직 없습니다", total, rsiScopeSummary(tally.byScope))
	}
}

type codingDispatchRuntime struct {
	LastTickAtMs        int64  `json:"lastTickAtMs"`
	LastResult          string `json:"lastResult"`
	Detail              string `json:"detail"`
	CandidateID         string `json:"candidateId"`
	LastDispatchAtMs    int64  `json:"lastDispatchAtMs"`
	LastSuccessfulAtMs  int64  `json:"lastSuccessfulAtMs"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
}

func (t *Tracker) codingDispatchRuntimeStatus() codingDispatchRuntime {
	path := filepath.Join(filepath.Dir(t.selfCorrectionPath), "coding_dispatch_status.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return codingDispatchRuntime{}
	}
	var status codingDispatchRuntime
	if json.Unmarshal(raw, &status) != nil {
		return codingDispatchRuntime{}
	}
	return status
}

func rsiDispatchTickValue(status codingDispatchRuntime) string {
	if strings.TrimSpace(status.LastResult) == "" {
		return "기록 없음"
	}
	return status.LastResult + " · " + rsiAgeValue(status.LastTickAtMs)
}

func rsiAgeValue(atMs int64) string {
	if atMs <= 0 {
		return "없음"
	}
	age := time.Since(time.UnixMilli(atMs))
	if age < 0 {
		age = 0
	}
	switch {
	case age < time.Minute:
		return "방금"
	case age < time.Hour:
		return fmt.Sprintf("%d분 전", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%d시간 전", int(age.Hours()))
	default:
		return fmt.Sprintf("%d일 전", int(age.Hours()/24))
	}
}

type rsiDispatchAttempt struct {
	AttemptID    string `json:"attemptId"`
	Outcome      string `json:"outcome"`
	DispatchedAt int64  `json:"dispatchedAt"`
}

type rsiDispatchMarker struct {
	AttemptID    string               `json:"attemptId"`
	Outcome      string               `json:"outcome"`
	DispatchedAt int64                `json:"dispatchedAt"`
	Attempts     []rsiDispatchAttempt `json:"attempts"`
}

// rsiDispatchAttempts returns retained retry history plus the marker's current
// top-level attempt. dispatch_prompt.py moves the prior top-level attempt into
// attempts before each retry, so one marker file is not necessarily one
// dispatch. The file mtime remains the compatibility timestamp for legacy
// markers written before dispatchedAt existed.
func rsiDispatchAttempts(path string, fallbackAt int64) []rsiDispatchAttempt {
	raw, err := os.ReadFile(path)
	if err != nil {
		return []rsiDispatchAttempt{{DispatchedAt: fallbackAt}}
	}
	var marker rsiDispatchMarker
	if json.Unmarshal(raw, &marker) != nil {
		return []rsiDispatchAttempt{{DispatchedAt: fallbackAt}}
	}
	current := rsiDispatchAttempt{AttemptID: marker.AttemptID, Outcome: marker.Outcome, DispatchedAt: marker.DispatchedAt}
	if current.DispatchedAt == 0 {
		current.DispatchedAt = fallbackAt
	}
	return append(marker.Attempts, current)
}

// codingDispatchCounts scans retained retry history plus each dispatch marker's
// current attempt.
func (t *Tracker) codingDispatchCounts() (total, today int) {
	return t.codingDispatchCountsAt(dentime.Now())
}

func (t *Tracker) codingDispatchCountsAt(now time.Time) (total, today int) {
	dir := filepath.Join(filepath.Dir(t.selfCorrectionPath), "coding_dispatch")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dayEnd := dayStart.AddDate(0, 0, 1)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		attempts := rsiDispatchAttempts(filepath.Join(dir, e.Name()), info.ModTime().UnixMilli())
		total += len(attempts)
		for _, attempt := range attempts {
			at := time.UnixMilli(attempt.DispatchedAt).In(now.Location())
			if !at.Before(dayStart) && at.Before(dayEnd) {
				today++
			}
		}
	}
	return total, today
}

// dispatchMarkerDir is where coding-dispatch.sh writes its per-dispatch
// markers — the dispatch ledger, next to the tracker's data files.
func (t *Tracker) dispatchMarkerDir() string {
	return filepath.Join(filepath.Dir(t.logPath), "coding_dispatch")
}

// DispatchMarkerBlocks reports whether coding-dispatch would skip this candidate
// id. Parity with scripts/dev/dispatch_outcome.blocks_redispatch:
//
//	landed / attempted / declined → block
//	failed / timeout              → retryable (do not block)
//	outcome-less / corrupt      → block until marker mtime is older than
//	                               dispatchMarkerAbandonAfter (default = L4
//	                               SESSION_TIMEOUT 2h); after that the pick
//	                               lane may reclaim the id.
func (t *Tracker) DispatchMarkerBlocks(id string) bool {
	return t.dispatchMarkerBlocksAt(id, time.Now())
}

func (t *Tracker) dispatchMarkerOutcome(id string) string {
	raw, err := os.ReadFile(filepath.Join(t.dispatchMarkerDir(), strings.TrimSpace(id)+".json"))
	if err != nil {
		return ""
	}
	var marker struct {
		Outcome string `json:"outcome"`
	}
	if json.Unmarshal(raw, &marker) != nil {
		return ""
	}
	return strings.TrimSpace(marker.Outcome)
}

// dispatchMarkerAbandonAfter matches Python DEFAULT_ABANDON_AFTER_SEC / the
// coding-dispatch SESSION_TIMEOUT default (7200).
const dispatchMarkerAbandonAfter = 2 * time.Hour

func (t *Tracker) dispatchMarkerBlocksAt(id string, now time.Time) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	path := filepath.Join(t.dispatchMarkerDir(), id+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var m struct {
		Outcome string `json:"outcome"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return dispatchMarkerIsFresh(path, now)
	}
	switch strings.TrimSpace(m.Outcome) {
	case "landed", "attempted", "declined":
		return true
	case "failed", "timeout":
		return false
	default:
		return dispatchMarkerIsFresh(path, now)
	}
}

func dispatchMarkerIsFresh(path string, now time.Time) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return now.Sub(info.ModTime()) < dispatchMarkerAbandonAfter
}

// rsiDispatchOutcomeNote aggregates recorded dispatch outcomes into a short
// diagnosis suffix ("" when no marker carries an outcome yet — markers
// predating outcome accounting simply have none).
func (t *Tracker) rsiDispatchOutcomeNote() string {
	evidence, err := t.rsiDispatchEvidence(0)
	if err != nil {
		return " · 배차 결과 원장을 읽을 수 없음"
	}
	outcomes, decided, landed := evidence.Outcomes, evidence.Decided, evidence.Landed
	if len(outcomes) == 0 {
		return ""
	}
	if decided == 0 {
		return fmt.Sprintf(" · 배차 결과: %s (종결 근거 대기)", rsiOutcomeSummary(outcomes))
	}
	return fmt.Sprintf(" · 배차 결과: %s (랜딩률 %.0f%%)", rsiOutcomeSummary(outcomes), float64(landed)/float64(decided)*100)
}

type rsiDispatchEvidence struct {
	Outcomes       map[string]int
	CohortOutcomes map[string]int
	Decided        int
	Landed         int
	RolledBack     int
}

type rsiDispatchEvidenceEvent struct {
	outcome      string
	dispatchedAt int64
	terminal     bool
	landed       bool
	rolledBack   bool
}

// rsiDispatchEvidence joins crash/idempotency markers to the authoritative
// lifecycle ledger. A marker's "landed" only becomes a success after the same
// attempt reaches watch_passed; attempted and pre-watch merges stay outside
// the denominator. When terminalLimit is positive, graduation uses only the
// latest terminal cohort so an old rollback cannot poison the loop forever.
func (t *Tracker) rsiDispatchEvidence(terminalLimit int) (rsiDispatchEvidence, error) {
	phases := map[string]string{}
	err := t.scanSelfCorrectionRecords(func(entry SelfCorrectionCandidateRecord) {
		if entry.Type != selfCorrectionTypeDispatch || strings.TrimSpace(entry.AttemptID) == "" {
			return
		}
		phases[strings.TrimSpace(entry.AttemptID)] = normalizeSelfCorrectionDispatchPhase(entry.DispatchPhase)
	})
	if err != nil {
		return rsiDispatchEvidence{}, err
	}

	var events []rsiDispatchEvidenceEvent
	paths, _ := filepath.Glob(filepath.Join(t.dispatchMarkerDir(), "*.json"))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		for _, attempt := range rsiDispatchAttempts(p, info.ModTime().UnixMilli()) {
			rawOutcome := strings.TrimSpace(attempt.Outcome)
			if rawOutcome == "" {
				continue
			}
			event := rsiDispatchEvidenceEvent{outcome: rawOutcome, dispatchedAt: attempt.DispatchedAt}
			switch rawOutcome {
			case "landed":
				switch phases[strings.TrimSpace(attempt.AttemptID)] {
				case selfCorrectionDispatchWatchPassed:
					event.terminal, event.landed = true, true
				case selfCorrectionDispatchRolledBack:
					event.outcome, event.terminal, event.rolledBack = "rolled_back", true, true
				default:
					event.outcome = "pending_watch"
				}
			case "declined", "failed", "timeout":
				event.terminal = true
			case "rolled_back":
				event.terminal, event.rolledBack = true, true
			}
			events = append(events, event)
		}
	}

	evidence := rsiDispatchEvidence{Outcomes: map[string]int{}, CohortOutcomes: map[string]int{}}
	for _, event := range events {
		evidence.Outcomes[event.outcome]++
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].dispatchedAt > events[j].dispatchedAt })
	for _, event := range events {
		if !event.terminal {
			continue
		}
		if terminalLimit > 0 && evidence.Decided >= terminalLimit {
			break
		}
		evidence.CohortOutcomes[event.outcome]++
		evidence.Decided++
		if event.landed {
			evidence.Landed++
		}
		if event.rolledBack {
			evidence.RolledBack++
		}
	}
	return evidence, nil
}

// rsiEProcessValue formats the L1 e-process cutover metric: who owns rollback
// firing, and how the observation-mode label evidence stands against the
// graduation thresholds (n>=20, agreement>=90%).
// rsiConfirmRateValue renders 확정률 with the counts it came from. The rate alone
// invites a false reading beside 기각: "100%" with ten rejections on the same row
// looks self-contradictory until you can see the denominator is the two evolves
// that actually shipped and resolved, not the ten that were turned away.
func rsiConfirmRateValue(rate float64, confirmed, resolved int) string {
	if resolved <= 0 {
		return "착지분 없음"
	}
	return fmt.Sprintf("%.0f%% (%d/%d)", rate*100, confirmed, resolved)
}

func rsiEProcessValue(r eProcessCutoverReadiness) string {
	switch {
	case r.EProcessOwner:
		return fmt.Sprintf("e-process 소유 (관측 n=%d)", r.Labels)
	case r.Ready:
		return fmt.Sprintf("컷오버 준비 완료 (n=%d · 합치 %.0f%%)", r.Labels, r.AgreementRate*100)
	case r.Labels == 0:
		return "관측 중 (라벨 없음)"
	default:
		return fmt.Sprintf("관측 n=%d · 합치 %.0f%%", r.Labels, r.AgreementRate*100)
	}
}

func rsiSourceDispatchable(source string) bool {
	source = strings.TrimSpace(source)
	// Separator-aware, matching coding-dispatch.sh's picker exactly: an exact
	// namespace or a "namespace:"-prefixed id. A bare prefix reported
	// "health-finding-x" as dispatchable/LIVE even though the picker will never
	// pick it — a false dashboard signal (Codex review of RSI eval M7).
	for _, s := range rsiDispatchSources {
		if rsiSourceMatchesNamespace(source, s) {
			return true
		}
	}
	// Executed graduation-ladder unlocks admit staged sources at runtime
	// (operator directive 2026-07-14). All selection and status readers use
	// this Go policy so source admission cannot drift between consumers.
	for _, s := range graduatedDispatchSources() {
		if s != "" && rsiSourceMatchesNamespace(source, s) {
			return true
		}
	}
	return false
}

// rsiSourceMatchesNamespace is the shared picker rule: exact namespace, or the
// same namespace extended past a ":" (mirrors coding-dispatch.sh and genesis
// selfCorrectionSourceMatches).
func rsiSourceMatchesNamespace(source, ns string) bool {
	return source == ns || strings.HasPrefix(source, ns+":")
}

func rsiScopeSummary(byScope map[string]int) string {
	keys := make([]string, 0, len(byScope))
	for k := range byScope {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, byScope[k]))
	}
	return strings.Join(parts, ", ")
}
