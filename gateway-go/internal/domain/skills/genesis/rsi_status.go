package genesis

// RSI loop-status computation — the in-process companion to
// scripts/audit/rsi_status.py, serving the native + andromeda "recursive
// self-improvement" viewers over the miniapp.rsi.status RPC. The Python audit
// script re-parses JSONL; this composes the tracker's live 7-day aggregates.
//
// The four layers and their honest states mirror the audit script exactly:
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
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RSI layer states. These are machine keys (not display text) — the clients map
// them to a color and a localized label.
const (
	RSIStateLive      = "LIVE"
	RSIStateDataGated = "DATA-GATED"
	RSIStateStarved   = "STARVED"
	RSIStateFrozen    = "FROZEN"
	RSIStateIdle      = "IDLE"
)

// rsiSubtleDegradationClasses are the judge-degradation classes that actually
// produce labeled misses (P3 fuel); a ledger with only blatant classes is
// data-gated, not broken.
var rsiSubtleDegradationClasses = map[string]bool{"imperative-drop": true, "safety-drop": true}

// rsiDispatchSources mirrors coding-dispatch.sh's accepted candidate sources: a
// code candidate from any other source is not yet dispatchable. health-finding
// graduated 2026-07-12 (first mined batch reviewed clean — roadmap P5 ladder);
// the runtime-error source stays staged until its own batch review.
var rsiDispatchSources = []string{"evolve-tool-gap", "self-harness", "health-finding"}

// rsiLayerDetails is the per-layer "what is this loop" explanation the viewers
// reveal on tap — static role text, keyed by layer.
var rsiLayerDetails = map[string]string{
	"L1": "저성과 스킬의 본문을 자동으로 다시 쓰고, 보류 검증과 롤백으로 회귀를 막는 기본 자가개선 루프입니다.",
	"L2": "스킬을 고치는 프롬프트(생성·판정) 자체를 주간 단위로 개정하는 메타 루프입니다. 벤치를 통과하면 자동 채택되고, 드리프트가 감지되면 스스로 동결합니다.",
	"L3": "판정자가 자신의 오판으로 학습하는 검증기 공진화 루프입니다. 판정 정확도 레인이 심은 결함을 재생해 오판 라벨을 만듭니다.",
	"L4": "게이트웨이 소스 자체를 고치는 자가편집 루프입니다. 근거 있는 후보만 코딩 레인에 배차되고, 게이트 통과와 배포 롤백 워치로 보호됩니다.",
}

// RSILoopStatus is the whole recursive-self-improvement snapshot.
type RSILoopStatus struct {
	Layers  []RSILayer
	Turning int // count of layers in LIVE or FROZEN
}

// RSILayer is one loop layer's classified state with display metrics.
type RSILayer struct {
	Key       string
	Title     string
	State     string
	Diagnosis string
	Detail    string // static "what is this loop" explanation (revealed on tap)
	Metrics   []RSIMetricKV
}

// RSIMetricKV is one display metric (label + preformatted value).
type RSIMetricKV struct {
	Label string
	Value string
}

// RSIStatus composes the four layer assessments from the tracker's public
// aggregates. It takes no lock of its own — each aggregate locks internally.
func (t *Tracker) RSIStatus() RSILoopStatus {
	layers := []RSILayer{t.rsiAssessL1(), t.rsiAssessL2(), t.rsiAssessL3(), t.rsiAssessL4()}
	turning := 0
	for i := range layers {
		layers[i].Detail = rsiLayerDetails[layers[i].Key]
		if layers[i].State == RSIStateLive || layers[i].State == RSIStateFrozen {
			turning++
		}
	}
	return RSILoopStatus{Layers: layers, Turning: turning}
}

func (t *Tracker) rsiAssessL1() RSILayer {
	h := t.EvolutionHealth()
	committed := h.Evolves7d + h.Genesis7d
	metrics := []RSIMetricKV{
		{"진화(7일)", strconv.Itoa(h.Evolves7d)},
		{"신규 스킬", strconv.Itoa(h.Genesis7d)},
		{"기각", strconv.Itoa(h.EvolveRejected7d)},
		{"확정률", fmt.Sprintf("%.0f%%", h.ConfirmRate*100)},
		{"e-process", rsiEProcessValue(t.EProcessCutoverReadiness())},
	}
	base := RSILayer{Key: "L1", Title: "스킬 진화", Metrics: metrics}
	switch {
	case committed > 0:
		base.State = RSIStateLive
		base.Diagnosis = fmt.Sprintf("이번 주 진화 %d · 신규 스킬 %d · 기각 %d", h.Evolves7d, h.Genesis7d, h.EvolveRejected7d)
	case h.EvolveRejected7d > 0:
		base.State = RSIStateDataGated
		base.Diagnosis = "후보는 생성됐지만 이번 주 게이트를 통과한 진화가 없습니다"
	default:
		base.State = RSIStateIdle
		base.Diagnosis = "최근 7일간 스킬 진화 활동이 없습니다"
	}
	return base
}

func (t *Tracker) rsiAssessL2() RSILayer {
	h := t.MetaEvolutionHealth()
	metrics := []RSIMetricKV{
		{"개정(7일)", strconv.Itoa(h.Revisions7d)},
		{"제안", strconv.Itoa(h.Proposed7d)},
	}
	if strings.TrimSpace(h.LastEpoch) != "" {
		metrics = append(metrics, RSIMetricKV{"최근 에폭", h.LastEpoch})
	}
	base := RSILayer{Key: "L2", Title: "메타 진화", Metrics: metrics}
	switch {
	case t.AutoAdoptFrozen():
		base.State = RSIStateFrozen
		base.Diagnosis = "드리프트 자기 브레이크 작동 — 자동 채택이 제안 전용으로 동결됐습니다"
	case h.Revisions7d > 0:
		base.State = RSIStateLive
		diag := fmt.Sprintf("이번 주 슬로우 루프 개정 %d · 제안 %d", h.Revisions7d, h.Proposed7d)
		if strings.TrimSpace(h.LastEpoch) != "" {
			diag += fmt.Sprintf(" (최근: %s)", h.LastEpoch)
		}
		base.Diagnosis = diag
	default:
		base.State = RSIStateIdle
		base.Diagnosis = "이번 주 슬로우 루프 사이클이 없습니다 — 주간 주기를 기다리는 중"
	}
	return base
}

func (t *Tracker) rsiAssessL3() RSILayer {
	records, err := t.RecentJudgeAccuracy(20)
	if err != nil || len(records) == 0 {
		return RSILayer{Key: "L3", Title: "판정자 공진화", State: RSIStateIdle, Diagnosis: "판정 정확도 레인이 아직 실행되지 않았습니다"}
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	runs, misses, falseRejects := 0, 0, 0
	subtleDeployed := false
	for _, r := range records {
		if r.CreatedAt < cutoff {
			continue
		}
		runs++
		misses += len(r.Misses)
		falseRejects += len(r.FalseRejects)
		for cls := range r.ByClass {
			if rsiSubtleDegradationClasses[cls] {
				subtleDeployed = true
			}
		}
	}
	if runs == 0 {
		return RSILayer{Key: "L3", Title: "판정자 공진화", State: RSIStateIdle, Diagnosis: "판정 정확도 레인이 최근 7일간 실행되지 않았습니다"}
	}
	organic := len(t.OrganicFalseAccepts(organicFalseAcceptWindow, 50))
	metrics := []RSIMetricKV{
		{"실행(7일)", strconv.Itoa(runs)},
		{"판정 놓침", strconv.Itoa(misses)},
		{"오기각", strconv.Itoa(falseRejects)},
		{"실전 라벨(30일)", strconv.Itoa(organic)},
	}
	base := RSILayer{Key: "L3", Title: "판정자 공진화", Metrics: metrics}
	switch {
	case misses > 0 || falseRejects > 0 || organic > 0:
		base.State = RSIStateLive
		base.Diagnosis = fmt.Sprintf("%d회 실행에서 판정 놓침 %d + 오기각 %d + 실전 라벨 %d — P3 학습 연료 축적 중", runs, misses, falseRejects, organic)
	case !subtleDeployed:
		base.State = RSIStateDataGated
		base.Diagnosis = fmt.Sprintf("%d회 실행; 판정자가 명백한 결함은 모두 잡았고 미묘 프로브는 아직 원장에 없습니다", runs)
	default:
		base.State = RSIStateDataGated
		base.Diagnosis = fmt.Sprintf("미묘 프로브가 있는 %d회 실행이지만 아직 놓침이 없습니다 — 현재 판정자가 강합니다", runs)
	}
	return base
}

func (t *Tracker) rsiAssessL4() RSILayer {
	cands, err := t.RecentSelfCorrectionCandidates("", "", 300)
	if err != nil {
		return RSILayer{Key: "L4", Title: "소스 자가편집", State: RSIStateIdle, Diagnosis: "후보 저장소를 읽을 수 없습니다"}
	}
	byScope := map[string]int{}
	dispatchable := 0
	staged := 0
	for _, c := range cands {
		scope := strings.TrimSpace(c.Scope)
		if scope == "" {
			scope = "?"
		}
		byScope[scope]++
		// proposed = unreviewed backlog; accepted = review-endorsed, awaiting
		// implementation — both are live dispatch supply (the heartbeat review
		// lane accepts candidates it cannot implement itself).
		st := normalizeSelfCorrectionStatus(c.Status)
		if scope == "code" && (st == SelfCorrectionStatusProposed || st == SelfCorrectionStatusAccepted) {
			if rsiSourceDispatchable(c.Source) {
				dispatchable++
			} else {
				// Code candidate from a source not yet in the dispatch
				// allowlist (runtime-error, …): real L4 supply staged for
				// review, not a wiring gap.
				staged++
			}
		}
	}
	metrics := []RSIMetricKV{
		{"후보", strconv.Itoa(len(cands))},
		{"코드 후보", strconv.Itoa(byScope["code"])},
		{"배차 가능", strconv.Itoa(dispatchable)},
		{"검토 대기(비배차)", strconv.Itoa(staged)},
	}
	base := RSILayer{Key: "L4", Title: "소스 자가편집", Metrics: metrics}
	switch {
	case dispatchable > 0:
		base.State = RSIStateLive
		base.Diagnosis = fmt.Sprintf("배차 가능한 코드 후보 %d건 — 코딩 레인 대기 중", dispatchable)
	case len(cands) == 0:
		base.State = RSIStateIdle
		base.Diagnosis = "아직 캡처된 자기교정 후보가 없습니다"
	case staged > 0:
		base.State = RSIStateStarved
		base.Diagnosis = fmt.Sprintf("비배차 소스의 코드 후보 %d건이 검토 대기 중 — 품질 리뷰 후 배차 소스로 졸업하면 배차됩니다", staged)
	default:
		base.State = RSIStateStarved
		base.Diagnosis = fmt.Sprintf("후보 %d건(%s)이지만 배차 가능한 코드 후보가 아직 없습니다", len(cands), rsiScopeSummary(byScope))
	}
	return base
}

// rsiEProcessValue formats the L1 e-process cutover metric: who owns rollback
// firing, and how the observation-mode label evidence stands against the
// graduation thresholds (n>=20, agreement>=90%).
func rsiEProcessValue(r EProcessCutoverReadiness) string {
	switch {
	case r.EProcessOwner:
		return fmt.Sprintf("발화 소유 (라벨 n=%d)", r.Labels)
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
	for _, s := range rsiDispatchSources {
		if strings.HasPrefix(source, s) {
			return true
		}
	}
	return false
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
