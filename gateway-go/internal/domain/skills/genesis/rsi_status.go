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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
// data-gated, not broken. Includes the escalated weaken tier (probe
// curriculum ladder — deployed only after the drop tier saturates).
var rsiSubtleDegradationClasses = map[string]bool{
	"imperative-drop": true, "safety-drop": true,
	"imperative-weaken": true, "scope-narrow": true,
}

// rsiWeakenDegradationClasses is the escalated tier subset — seeing one in
// the ledger means the lane already probes at its current difficulty ceiling.
var rsiWeakenDegradationClasses = map[string]bool{"imperative-weaken": true, "scope-narrow": true}

// rsiDispatchSources mirrors coding-dispatch.sh's accepted candidate sources: a
// code candidate from any other source is not yet dispatchable. health-finding
// graduated 2026-07-12 (first mined batch reviewed clean — roadmap P5 ladder);
// tool-quality graduated 2026-07-13 (operator directive); runtime-error and
// deadcode-finding stay staged until their own batch review. MUST match the
// allowlist in scripts/dev/coding-dispatch.sh (and scripts/audit/rsi_status.py).
var rsiDispatchSources = []string{"evolve-tool-gap", "self-harness", "health-finding", "tool-quality"}

// SourceAutoDispatches reports whether a self-correction candidate from this
// source is on the auto-dispatch track (graduated into coding-dispatch.sh's
// allowlist) vs staged for review. Exported for the miniapp wire projection so
// clients can label each candidate 자동수리 vs 검토 대기.
func SourceAutoDispatches(source string) bool { return rsiSourceDispatchable(source) }

// rsiLayerDetails is the per-layer "what is this loop" explanation the viewers
// reveal on tap — static role text, keyed by layer.
var rsiLayerDetails = map[string]string{
	"L1":   "저성과 스킬의 본문을 자동으로 다시 쓰고, 보류 검증과 롤백으로 회귀를 막는 기본 자가개선 루프입니다.",
	"L2":   "스킬을 고치는 프롬프트(생성·판정) 자체를 주간 단위로 개정하는 메타 루프입니다. 벤치를 통과하면 자동 채택되고, 드리프트가 감지되면 스스로 동결합니다.",
	"L3":   "판정자가 자신의 오판으로 학습하는 검증기 공진화 루프입니다. 판정 정확도 레인이 심은 결함을 재생해 오판 라벨을 만듭니다.",
	"L4":   "게이트웨이 소스 자체를 고치는 자가편집 루프입니다. 근거 있는 후보만 코딩 레인에 배차되고, 게이트 통과와 배포 롤백 워치로 보호됩니다.",
	"GRAD": "자율성 졸업 사다리의 행별 증거를 상시 심사하는 계기판입니다. 증거가 임계에 도달하면 '준비됨'으로 표시될 뿐, 잠금 해제 결정은 항상 운영자의 몫입니다.",
}

// RSILoopStatus is the whole recursive-self-improvement snapshot.
type RSILoopStatus struct {
	Layers  []RSILayer
	Turning int // count of layers in LIVE or FROZEN
	Health  RSIHealth
}

// RSIHealth is the structured evolution-health scoreboard behind the layer
// diagnoses — the numeric fields the layer metric strings only render as text,
// surfaced so clients can draw a real scoreboard (confirm/false-accept rates,
// activity counts, self-brake state) instead of parsing preformatted strings.
type RSIHealth struct {
	Evolves7d         int     // L1 evolves in the window
	Confirmed7d       int     // evolves that held up
	Rejected7d        int     // evolves rejected at the gate
	RolledBack7d      int     // evolves auto-reverted post-apply
	Genesis7d         int     // new skills created
	ConfirmRate       float64 // confirmed/(confirmed+rolledBack), 0..1
	FalseAcceptRate   float64 // rolledBack/(confirmed+rolledBack) — judge going soft
	ResolvedEvolves7d int     // sample size behind the two rates
	Thrash            bool    // rapid evolve/rollback churn detected
	AutoAdoptFrozen   bool    // L2 drift self-brake engaged
	MetaRevisions7d   int     // L2 meta-artifact revisions in the window
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
	layers := []RSILayer{t.rsiAssessL1(), t.rsiAssessL2(), t.rsiAssessL3(), t.rsiAssessL4(), t.rsiAssessLadder()}
	turning := 0
	for i := range layers {
		layers[i].Detail = rsiLayerDetails[layers[i].Key]
		// The graduation-ladder pseudo-layer is an evidence dashboard, not a
		// loop — it never counts toward the "N/4 turning" headline.
		if layers[i].Key == "GRAD" {
			continue
		}
		if layers[i].State == RSIStateLive || layers[i].State == RSIStateFrozen {
			turning++
		}
	}
	eh := t.EvolutionHealth()
	meta := t.MetaEvolutionHealth()
	return RSILoopStatus{
		Layers:  layers,
		Turning: turning,
		Health: RSIHealth{
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

func (t *Tracker) rsiAssessL1() RSILayer {
	h := t.EvolutionHealth()
	committed := h.Evolves7d + h.Genesis7d
	metrics := []RSIMetricKV{
		{"진화(7일)", strconv.Itoa(h.Evolves7d)},
		{"신규 스킬", strconv.Itoa(h.Genesis7d)},
		{"제안", strconv.Itoa(h.Proposals7d)},
		{"기각", strconv.Itoa(h.EvolveRejected7d)},
		{"확정률", fmt.Sprintf("%.0f%%", h.ConfirmRate*100)},
		{"e-process", rsiEProcessValue(t.EProcessCutoverReadiness())},
		{"라벨러 사각", strconv.Itoa(len(t.LabelerBlindSpots(evolutionHealthWindow)))},
	}
	base := RSILayer{Key: "L1", Title: "스킬 진화", Metrics: metrics}
	switch {
	case committed > 0:
		base.State = RSIStateLive
		base.Diagnosis = fmt.Sprintf("이번 주 진화 %d · 신규 스킬 %d · 제안 %d · 기각 %d", h.Evolves7d, h.Genesis7d, h.Proposals7d, h.EvolveRejected7d)
	case h.EvolveRejected7d > 0 || h.Proposals7d > 0:
		// Proposals/rejections without commits = the lane is alive but gated
		// (Python rsi_status assess_l1 parity). Counting only rejects previously
		// left proposal-only weeks looking IDLE.
		base.State = RSIStateDataGated
		base.Diagnosis = fmt.Sprintf("제안 %d · 기각 %d — 후보는 있지만 이번 주 게이트를 통과한 진화가 없습니다", h.Proposals7d, h.EvolveRejected7d)
	default:
		base.State = RSIStateIdle
		base.Diagnosis = "최근 7일간 스킬 진화 활동이 없습니다"
	}
	return base
}

func (t *Tracker) rsiAssessL2() RSILayer {
	// Scoreboard stays on the 7d MetaEvolutionHealth window; LIVE/IDLE for the
	// slow loop uses a 14d look-back (Python rsi_status assess_l2 parity — the
	// weekly cadence would otherwise flip IDLE mid-week after a quiet 7d).
	h := t.MetaEvolutionHealth()
	metrics := []RSIMetricKV{
		{"개정(7일)", strconv.Itoa(h.Revisions7d)},
		{"제안(7일)", strconv.Itoa(h.Proposed7d)},
	}
	if strings.TrimSpace(h.LastEpoch) != "" {
		metrics = append(metrics, RSIMetricKV{"최근 에폭", h.LastEpoch})
	}
	base := RSILayer{Key: "L2", Title: "메타 진화", Metrics: metrics}
	switch {
	case t.AutoAdoptFrozen():
		base.State = RSIStateFrozen
		base.Diagnosis = "드리프트 자기 브레이크 작동 — 자동 채택이 제안 전용으로 동결됐습니다"
	case t.metaActivityIn(metaEvolutionAssessWindow):
		base.State = RSIStateLive
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
		base.State = RSIStateIdle
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

// metaCycleCountsIn tallies slow-loop ledger rows inside window, matching
// scripts/audit/rsi_status.py assess_l2 (action=="" → cycle; proposed flag;
// adopted/reverted action rows).
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

func (t *Tracker) rsiAssessL3() RSILayer {
	records, err := t.RecentJudgeAccuracy(20)
	operatorLabels := len(t.RecentOperatorJudgeVerdicts(7*24*time.Hour, 100))
	if err != nil || (len(records) == 0 && operatorLabels == 0) {
		return RSILayer{Key: "L3", Title: "판정자 공진화", State: RSIStateIdle, Diagnosis: "판정 정확도 레인이 아직 실행되지 않았습니다"}
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	runs, misses, falseRejects := 0, 0, 0
	subtleDeployed, weakenDeployed := false, false
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
			if rsiWeakenDegradationClasses[cls] {
				weakenDeployed = true
			}
		}
	}
	if runs == 0 && operatorLabels == 0 {
		return RSILayer{Key: "L3", Title: "판정자 공진화", State: RSIStateIdle, Diagnosis: "판정 정확도 레인이 최근 7일간 실행되지 않았습니다"}
	}
	organic := len(t.OrganicFalseAccepts(organicFalseAcceptWindow, 50))
	metrics := []RSIMetricKV{
		{"실행(7일)", strconv.Itoa(runs)},
		{"판정 놓침", strconv.Itoa(misses)},
		{"오기각", strconv.Itoa(falseRejects)},
		{"실전 라벨(30일)", strconv.Itoa(organic)},
		{"운영자 라벨(7일)", strconv.Itoa(operatorLabels)},
	}
	base := RSILayer{Key: "L3", Title: "판정자 공진화", Metrics: metrics}
	switch {
	case misses > 0 || falseRejects > 0 || organic > 0 || operatorLabels > 0:
		base.State = RSIStateLive
		base.Diagnosis = fmt.Sprintf("%d회 실행에서 판정 놓침 %d + 오기각 %d + 실전 라벨 %d + 운영자 라벨 %d — P3 학습 연료 축적 중", runs, misses, falseRejects, organic, operatorLabels)
	case !subtleDeployed:
		base.State = RSIStateDataGated
		base.Diagnosis = fmt.Sprintf("%d회 실행; 판정자가 명백한 결함은 모두 잡았고 미묘 프로브는 아직 원장에 없습니다", runs)
	case weakenDeployed:
		base.State = RSIStateDataGated
		base.Diagnosis = fmt.Sprintf("격상된 약화 프로브까지 %d회 실행 모두 잡았습니다 — 판정자가 현행 프로브 최고 티어에서 강합니다", runs)
	default:
		base.State = RSIStateDataGated
		base.Diagnosis = fmt.Sprintf("미묘 프로브가 있는 %d회 실행이지만 아직 놓침이 없습니다 — 판정자가 강하며, 포화 %d회 연속이면 약화 프로브로 격상됩니다", runs, judgeEscalationWindow)
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
	inFlight := 0
	applied := 0
	failed := 0
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
		if scope != "code" {
			continue
		}
		phase := normalizeSelfCorrectionDispatchPhase(c.DispatchPhase)
		switch phase {
		case SelfCorrectionDispatchStarted, SelfCorrectionDispatchPROpened,
			SelfCorrectionDispatchMerged, SelfCorrectionDispatchDeployed:
			inFlight++
		case SelfCorrectionDispatchWatchPassed:
			applied++
		case SelfCorrectionDispatchFailed, SelfCorrectionDispatchRolledBack:
			failed++
		case "":
			if st == SelfCorrectionStatusProposed || st == SelfCorrectionStatusAccepted {
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
	}
	_, dispatchedToday := t.codingDispatchCounts()
	metrics := []RSIMetricKV{
		{"후보", strconv.Itoa(len(cands))},
		{"코드 후보", strconv.Itoa(byScope["code"])},
		{"배차 가능", strconv.Itoa(dispatchable)},
		{"진행 중", strconv.Itoa(inFlight)},
		{"감시 통과", strconv.Itoa(applied)},
		{"실패/롤백", strconv.Itoa(failed)},
		{"검토 대기(비배차)", strconv.Itoa(staged)},
		{"오늘 배차", strconv.Itoa(dispatchedToday)},
	}
	base := RSILayer{Key: "L4", Title: "소스 자가편집", Metrics: metrics}
	switch {
	case inFlight > 0:
		base.State = RSIStateLive
		base.Diagnosis = fmt.Sprintf("코드 후보 %d건이 PR·배포·롤백 감시 단계를 통과 중", inFlight)
	case dispatchable > 0 || dispatchedToday > 0:
		// dispatch_today keeps L4 LIVE after coding-dispatch drains the queue
		// (Python rsi_status assess_l4 parity).
		base.State = RSIStateLive
		base.Diagnosis = fmt.Sprintf("배차 가능한 코드 후보 %d건 · 오늘 배차 %d건", dispatchable, dispatchedToday)
	case applied > 0:
		base.State = RSIStateLive
		base.Diagnosis = fmt.Sprintf("소스 자가편집 %d건이 머지·배포 후 롤백 감시까지 통과", applied)
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
	// Dispatch-outcome history (graduation-ladder evidence: the cap-raise row
	// needs a measured land rate) rides the diagnosis text — no new metric row,
	// so the native card layout is untouched.
	if note := rsiDispatchOutcomeNote(t.dispatchMarkerDir()); note != "" {
		base.Diagnosis += note
	}
	return base
}

// codingDispatchCounts mirrors scripts/audit/rsi_status.py's coding_dispatch/
// marker scan: total markers and how many were written today (UTC day boundary).
func (t *Tracker) codingDispatchCounts() (total, today int) {
	dir := filepath.Join(filepath.Dir(t.selfCorrectionPath), "coding_dispatch")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		total++
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !info.ModTime().Before(dayStart) {
			today++
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
//	landed / attempted          → block
//	declined / failed / timeout → retryable (do not block)
//	outcome-less / corrupt      → block until marker mtime is older than
//	                               dispatchMarkerAbandonAfter (default = L4
//	                               SESSION_TIMEOUT 2h); after that the pick
//	                               lane may reclaim the id.
func (t *Tracker) DispatchMarkerBlocks(id string) bool {
	return t.dispatchMarkerBlocksAt(id, time.Now())
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
		return true
	}
	switch strings.TrimSpace(m.Outcome) {
	case "landed", "attempted":
		return true
	case "declined", "failed", "timeout":
		return false
	default:
		info, err := os.Stat(path)
		if err != nil {
			return true
		}
		return now.Sub(info.ModTime()) < dispatchMarkerAbandonAfter
	}
}

// rsiDispatchOutcomeNote aggregates recorded dispatch outcomes into a short
// diagnosis suffix ("" when no marker carries an outcome yet — markers
// predating outcome accounting simply have none).
func rsiDispatchOutcomeNote(dir string) string {
	outcomes, decided, landed := rsiDispatchOutcomes(dir)
	if decided == 0 {
		return ""
	}
	return fmt.Sprintf(" · 배차 결과: %s (랜딩률 %.0f%%)", rsiOutcomeSummary(outcomes), float64(landed)/float64(decided)*100)
}

// rsiDispatchOutcomes scans the dispatch markers for recorded outcomes — the
// shared read the L4 diagnosis note and the graduation-ladder cap row both
// aggregate from. Markers predating outcome accounting carry none.
func rsiDispatchOutcomes(dir string) (outcomes map[string]int, decided, landed int) {
	outcomes = map[string]int{}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var m struct {
			Outcome string `json:"outcome"`
		}
		if json.Unmarshal(raw, &m) != nil || m.Outcome == "" {
			continue
		}
		outcomes[m.Outcome]++
		decided++
		if m.Outcome == "landed" {
			landed++
		}
	}
	return outcomes, decided, landed
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
