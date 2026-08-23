// dreamer_selfcompare.go — RHI-style trajectory-local self-comparison for the
// dream synthesis loop (arXiv 2607.15524, adopted 2026-07-20).
//
// Each dream cycle's proposal report is pairwise-judged against the previous
// cycle's (trajectory-local: no population, one extra small LLM call), and the
// verdict — winner plus recurring-weakness tags from a fixed vocabulary — is
// appended to a ledger. When the ledger accumulates a recurring weakness, a
// slow revision pass rewrites the externalized synthesis rules override
// (wiki-dream-rules.md), the evolvable artifact loadWikiSynthesisRules already
// anticipates. RHI's unconditional-replacement loop is deliberately NOT
// adopted: adoption passes a deterministic contract gate (load-bearing rule
// lines must survive), the previous rules are kept as a .bak, and a
// post-revision loss streak rolls back automatically — LLM proposes,
// deterministic Go decides (house rule).
//
// The whole lane is fail-closed behind SetRulesEvolution: only the production
// gateway enables it, because wiki-dream-rules.md lives in the shared
// workspace a dev/live-test instance must not mutate.
package wiki

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// shortRulesHash fingerprints a rules block so ledger entries can attribute
// comparisons to the rules version active during their cycle.
func shortRulesHash(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:6])
}

const (
	// dreamCompareFile is the self-comparison ledger (JSONL) beside the other
	// dreamer state dotfiles in the wiki dir.
	dreamCompareFile = ".dream-selfcompare.jsonl"
	// dreamCompareMaxTokens / dreamRulesReviseMaxTokens bound the two LLM
	// calls; both run under llmRequest's reasoning-headroom scaling.
	dreamCompareMaxTokens     = 1024
	dreamRulesReviseMaxTokens = 8192
	dreamCompareTimeout       = 2 * time.Minute
	dreamRulesReviseTimeout   = 3 * time.Minute
	// dreamCompareMaxUpdatesShown caps per-side proposal previews in the judge
	// prompt.
	dreamCompareMaxUpdatesShown = 12
	// dreamRulesMinComparisons comparisons must accumulate since the last
	// revision (or ever) before a revision is considered.
	dreamRulesMinComparisons = 6
	// dreamRulesReviseCooldown spaces revisions: the slow loop revises at most
	// weekly, mirroring meta-evolution's cadence.
	dreamRulesReviseCooldown = 7 * 24 * time.Hour
	// dreamRulesRollbackWindow / dreamRulesRollbackLosses: if the first N
	// post-revision comparisons carry >= L previous-wins and zero current-wins,
	// the revision is judged a regression and the .bak is restored.
	dreamRulesRollbackWindow = 3
	dreamRulesRollbackLosses = 2
	// dreamCompareLedgerMaxBytes triggers tail-rotation of the ledger.
	dreamCompareLedgerMaxBytes = 512 * 1024
	dreamCompareLedgerKeep     = 200
	// dreamRulesMinBytes/MaxBytes bound an adoptable revised rules block.
	dreamRulesMinBytes = 500
	dreamRulesMaxBytes = 16 * 1024
)

// dreamWeaknessVocab is the fixed tag vocabulary for judge verdicts — the
// deterministic aggregation (RHI's "recurring issues" delta checklist) only
// counts these, so free-text drift cannot inflate a weakness.
var dreamWeaknessVocab = []string{
	"사실누락", "라우팅오류", "중복제안", "출처부실", "과잉제안", "과소제안", "형식드리프트",
}

// dreamCompareEntry is one ledger line: a pairwise comparison, a rules
// revision, or a rollback.
type dreamCompareEntry struct {
	Ts   int64  `json:"ts"`   // unix millis (dentime)
	Kind string `json:"kind"` // "compare" | "revision" | "rollback"

	// compare fields
	Winner     string   `json:"winner,omitempty"` // current | previous | tie
	Weaknesses []string `json:"weaknesses,omitempty"`
	Rationale  string   `json:"rationale,omitempty"`
	Proposed   int      `json:"proposed,omitempty"`
	Applied    int      `json:"applied,omitempty"`
	RulesHash  string   `json:"rulesHash,omitempty"` // rules active during the cycle

	// revision/rollback fields
	FromHash string `json:"fromHash,omitempty"`
	ToHash   string `json:"toHash,omitempty"`
	Target   string `json:"target,omitempty"` // weakness the revision aimed at
}

// dreamCompareVerdict is the judge's JSON output.
type dreamCompareVerdict struct {
	Winner     string   `json:"winner"`
	Weaknesses []string `json:"weaknesses"`
	Rationale  string   `json:"rationale"`
}

// SetRulesEvolution arms the RHI self-comparison + rules-revision lane. Off by
// default (fail-closed): the server enables it only for the production state
// dir, since revisions write wiki-dream-rules.md in the shared workspace.
func (wd *WikiDreamer) SetRulesEvolution(enabled bool) {
	wd.rulesEvolve = enabled
}

// captureDreamSelfComparison runs the trajectory-local pairwise judgment for
// one cycle and, when history warrants, the slow rules-revision pass. Failures
// cost the pass, never the cycle.
func (wd *WikiDreamer) captureDreamSelfComparison(ctx context.Context, cycle *dreamCycle) {
	if !wd.rulesEvolve || wd.client == nil {
		return
	}
	if cycle.prevProposal == nil || len(cycle.prevProposal.Proposed) == 0 || len(cycle.proposal.Proposed) == 0 {
		return // nothing comparable — first cycle or an empty side
	}
	verdict, err := wd.judgeDreamCycles(ctx, cycle)
	if err != nil {
		cycle.addPhaseError("selfcompare: %v", err)
		return
	}
	entry := dreamCompareEntry{
		Ts:         dentime.Now().UnixMilli(),
		Kind:       "compare",
		Winner:     verdict.Winner,
		Weaknesses: verdict.Weaknesses,
		Rationale:  verdict.Rationale,
		Proposed:   len(cycle.proposal.Proposed),
		Applied:    cycle.created + cycle.updated,
		RulesHash:  shortRulesHash(wd.loadWikiSynthesisRules()),
	}
	if err := wd.appendDreamCompareEntry(entry); err != nil {
		cycle.addPhaseError("selfcompare-append: %v", err)
		return
	}
	wd.logger.Info("wiki-dream: self-comparison recorded",
		"winner", verdict.Winner, "weaknesses", strings.Join(verdict.Weaknesses, ","))

	if err := wd.maybeReviseDreamRules(ctx); err != nil {
		cycle.addPhaseError("rules-revise: %v", err)
	}
}

// judgeDreamCycles asks a small judge to compare this cycle's proposal report
// with the previous cycle's. The cycles digested DIFFERENT diary inputs, so
// the rubric targets process quality (routing, duplication, sourcing,
// over/under-proposing), not absolute coverage.
func (wd *WikiDreamer) judgeDreamCycles(ctx context.Context, cycle *dreamCycle) (dreamCompareVerdict, error) {
	ctx, cancel := context.WithTimeout(ctx, dreamCompareTimeout)
	defer cancel()

	demand := "없음"
	if diaryHasProjectDemand(cycle.synthInput) {
		demand = "있음"
	}
	prompt := fmt.Sprintf(`아래는 위키 드리머(일지→위키 합성)의 직전 사이클과 이번 사이클의 제안 리포트입니다.
두 사이클은 서로 다른 일지를 소화했으므로 절대 커버리지가 아니라 **소화 과정의 품질**을 비교하세요:
라우팅 정확성(올바른 카테고리/슬롯), 중복 없음, 요약·근거 품질, 출처 규율, 과잉/과소 제안.

과소제안의 정의: 이번 입력에 프로젝트수요가 있는데 프로젝트 페이지를 하나도 쓰지 않은 경우만.
직전보다 제안 수가 적다는 이유만으로는 과소제안이 아니다.
이번 사이클 입력의 프로젝트수요: %s

## 직전 사이클
%s

## 이번 사이클
%s

## 출력 (JSON만, 다른 텍스트 없이)
{"winner":"current|previous|tie","weaknesses":["이번 사이클의 약점 태그(최대 3)"],"rationale":"한두 문장"}
weaknesses 태그는 다음 고정 어휘에서만 고르세요: %s. 해당 없으면 빈 배열.`,
		demand,
		renderDreamProposalForJudge(cycle.prevProposal, cycle.prevProposal.Applied.Created+cycle.prevProposal.Applied.Updated),
		renderDreamProposalForJudge(&cycle.proposal, cycle.created+cycle.updated),
		strings.Join(dreamWeaknessVocab, ", "))

	resp, err := wd.client.Complete(ctx,
		wd.llmRequest("You compare two work products. Respond only with a JSON object.", prompt, dreamCompareMaxTokens))
	if err != nil {
		return dreamCompareVerdict{}, fmt.Errorf("selfcompare LLM call: %w", err)
	}
	verdict, perr := parseDreamCompareVerdict(resp)
	if perr != nil {
		return dreamCompareVerdict{}, perr
	}
	return refineDreamCompareVerdict(verdict, cycle), nil
}

// refineDreamCompareVerdict drops a 과소제안 tag when this cycle actually
// wrote a 프로젝트 page — the judge used to stamp it whenever the proposal
// count fell vs the previous cycle, which trained the rules loop to "write
// more 기타" (2026-08-23: 16/51 과소제안, two of three revisions).
func refineDreamCompareVerdict(v dreamCompareVerdict, cycle *dreamCycle) dreamCompareVerdict {
	if cycle == nil || !proposalWroteProject(cycle) {
		return v
	}
	kept := v.Weaknesses[:0]
	for _, w := range v.Weaknesses {
		if w == "과소제안" {
			continue
		}
		kept = append(kept, w)
	}
	v.Weaknesses = kept
	return v
}

func proposalWroteProject(cycle *dreamCycle) bool {
	for _, p := range cycle.proposal.Proposed {
		if categoryFromPath(p.Path) == "프로젝트" {
			return true
		}
	}
	for _, u := range cycle.updates {
		if updateCategory(u) == "프로젝트" {
			return true
		}
	}
	return false
}

// renderDreamProposalForJudge renders one proposal report compactly.
func renderDreamProposalForJudge(report *dreamProposalReport, applied int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "제안 %d건, 적용 %d건", len(report.Proposed), applied)
	if len(report.PhaseErrors) > 0 {
		fmt.Fprintf(&b, ", 단계 오류 %d건", len(report.PhaseErrors))
	}
	b.WriteString("\n")
	for i, p := range report.Proposed {
		if i >= dreamCompareMaxUpdatesShown {
			fmt.Fprintf(&b, "- … 외 %d건\n", len(report.Proposed)-i)
			break
		}
		fmt.Fprintf(&b, "- %s %s — %s", p.Action, p.Path, p.Title)
		if p.Summary != "" {
			fmt.Fprintf(&b, " (%s)", truncateDreamReportText(p.Summary, 60))
		}
		if p.ContentHint != "" {
			fmt.Fprintf(&b, " · %s", truncateDreamReportText(p.ContentHint, 80))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// parseDreamCompareVerdict decodes and normalizes the judge output: fences
// stripped, winner defaulted to tie on drift, weaknesses filtered to the
// vocabulary and capped at 3, rationale redacted and bounded.
func parseDreamCompareVerdict(text string) (dreamCompareVerdict, error) {
	text = stripLLMFences(text)
	var v dreamCompareVerdict
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return dreamCompareVerdict{}, fmt.Errorf("parse selfcompare verdict: %w (raw: %.200s)", err, text)
	}
	switch v.Winner {
	case "current", "previous", "tie":
	default:
		v.Winner = "tie"
	}
	vocab := map[string]bool{}
	for _, w := range dreamWeaknessVocab {
		vocab[w] = true
	}
	kept := v.Weaknesses[:0]
	for _, w := range v.Weaknesses {
		w = strings.TrimSpace(w)
		if vocab[w] && len(kept) < 3 {
			kept = append(kept, w)
		}
	}
	v.Weaknesses = kept
	v.Rationale = truncateDreamReportText(redact.String(v.Rationale), 200)
	return v, nil
}

// stripLLMFences removes a wrapping markdown code fence from an LLM response.
func stripLLMFences(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text[3:], "\n"); idx >= 0 {
			text = text[3+idx+1:]
		}
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	}
	return strings.TrimSpace(text)
}

// --- ledger ---

func (wd *WikiDreamer) dreamComparePath() string {
	return filepath.Join(wd.store.Dir(), dreamCompareFile)
}

// appendDreamCompareEntry appends one JSONL line and tail-rotates the ledger
// when it outgrows its byte budget.
func (wd *WikiDreamer) appendDreamCompareEntry(entry dreamCompareEntry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal compare entry: %w", err)
	}
	path := wd.dreamComparePath()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open compare ledger: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("append compare ledger: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close compare ledger: %w", err)
	}
	if info, err := os.Stat(path); err == nil && info.Size() > dreamCompareLedgerMaxBytes {
		wd.rotateDreamCompareLedger(path)
	}
	return nil
}

// rotateDreamCompareLedger keeps the tail of the ledger. Best-effort: a
// failed rotation leaves the oversized-but-valid original in place.
func (wd *WikiDreamer) rotateDreamCompareLedger(path string) {
	entries := wd.readDreamCompareEntries()
	if len(entries) > dreamCompareLedgerKeep {
		entries = entries[len(entries)-dreamCompareLedgerKeep:]
	}
	var b strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			continue
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// readDreamCompareEntries scans the ledger, skipping malformed lines.
func (wd *WikiDreamer) readDreamCompareEntries() []dreamCompareEntry {
	data, err := os.ReadFile(wd.dreamComparePath())
	if err != nil {
		return nil
	}
	var out []dreamCompareEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e dreamCompareEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		out = append(out, e)
	}
	return out
}

// --- rules revision (the slow half of the loop) ---

// maybeReviseDreamRules considers a rollback of the last revision, then a new
// revision, from the accumulated comparison history. All conditions are
// deterministic; only the rewrite itself is an LLM call, and its output must
// pass validateDreamRules before adoption.
func (wd *WikiDreamer) maybeReviseDreamRules(ctx context.Context) error {
	// Rollback needs only file ops, so the client-nil guard sits later, just
	// before the one LLM call — a lost client must not strand a bad revision.
	if !wd.rulesEvolve || wd.workspaceDir == "" {
		return nil
	}
	entries := wd.readDreamCompareEntries()
	lastEvent := -1
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "revision" || entries[i].Kind == "rollback" {
			lastEvent = i
			break
		}
	}
	var since []dreamCompareEntry
	for _, e := range entries[lastEvent+1:] {
		if e.Kind == "compare" {
			since = append(since, e)
		}
	}

	// Rollback watch: a fresh revision whose first comparisons trend losing is
	// a regression — restore the backup before considering anything else.
	if lastEvent >= 0 && entries[lastEvent].Kind == "revision" && len(since) >= dreamRulesRollbackWindow {
		window := since[:dreamRulesRollbackWindow]
		prevWins, curWins := 0, 0
		for _, e := range window {
			switch e.Winner {
			case "previous":
				prevWins++
			case "current":
				curWins++
			}
		}
		if prevWins >= dreamRulesRollbackLosses && curWins == 0 {
			return wd.rollbackDreamRules(entries[lastEvent])
		}
	}

	if len(since) < dreamRulesMinComparisons {
		return nil
	}
	if lastEvent >= 0 && dentime.Now().Sub(time.UnixMilli(entries[lastEvent].Ts)) < dreamRulesReviseCooldown {
		return nil
	}
	counts := weaknessCounts(since)
	target, targetCount := topWeakness(counts)
	if targetCount < 2 {
		return nil // no recurring weakness — nothing to aim a revision at
	}
	if wd.client == nil {
		return nil
	}
	return wd.reviseDreamRules(ctx, since, counts, target)
}

// weaknessCounts aggregates weakness tags across comparisons — RHI's
// "recurring issues to fix" delta checklist, computed deterministically.
func weaknessCounts(entries []dreamCompareEntry) map[string]int {
	counts := map[string]int{}
	for _, e := range entries {
		for _, w := range e.Weaknesses {
			counts[w]++
		}
	}
	return counts
}

// topWeakness returns the most-recurring weakness (ties break by vocabulary
// order for determinism).
func topWeakness(counts map[string]int) (string, int) {
	best, bestCount := "", 0
	for _, w := range dreamWeaknessVocab {
		if counts[w] > bestCount {
			best, bestCount = w, counts[w]
		}
	}
	return best, bestCount
}

// reviseDreamRules asks the optimizer for a minimally-edited rules block
// targeting the recurring weaknesses, gates it, and adopts it with a backup.
func (wd *WikiDreamer) reviseDreamRules(ctx context.Context, since []dreamCompareEntry, counts map[string]int, target string) error {
	ctx, cancel := context.WithTimeout(ctx, dreamRulesReviseTimeout)
	defer cancel()

	current := wd.loadWikiSynthesisRules()
	// Utility evidence grounds the revision in consequence, not taste: the
	// self-comparison judge grades the dreamer's own prose, while the recall
	// ledger says which KINDS of page a real turn actually pulled in — and which
	// questions had no page at all (dreamer_utility_evidence.go).
	utility := wd.renderUtilityEvidence(dentime.Now())
	utilitySection := ""
	if utility != "" {
		utilitySection = fmt.Sprintf(`
## 실측 효용 (회상 원장, 최근 30일)
%s
이 분포는 취향이 아니라 실제 사용 기록입니다. 회상률이 낮은 종류는 덜/짧게 쓰고, 실제로 회상되는 종류와 답 못한 수요 주제 쪽으로 합성을 기울이세요.
`, utility)
	}
	prompt := fmt.Sprintf(`당신은 위키 드리머의 합성 규칙 유지보수자입니다. 아래는 현재 규칙과, 사이클 간 자기비교에서 집계된 반복 약점입니다.

## 반복 약점 (카운트 높은 순 — 최우선: %s)
%s

## 최근 비교 근거
%s
%s
## 현재 규칙
%s

## 과제
반복 약점을 겨냥해 규칙을 **최소 수정**하세요 — 관련 규칙 한두 줄을 고치거나 짧은 줄 하나를 추가하는 수준. 전면 재작성 금지.
불변식은 절대 제거/약화 금지: 6개 카테고리 체계, 프로젝트 폴더 슬롯(대표.md·로그.md 등), JSON 배열 출력 계약, 추측 금지 계열 규칙, 시스템 자동 관리 페이지 규칙.
수정된 **전체 규칙 블록**을 '## 규칙'부터 끝까지 그대로 출력하세요. 다른 텍스트 없이.`,
		target, renderWeaknessCounts(counts), renderRecentComparisons(since, 6), utilitySection, current)

	resp, err := wd.client.Complete(ctx,
		wd.llmRequest("You maintain a synthesis-rules document. Output only the full revised rules block.", prompt, dreamRulesReviseMaxTokens))
	if err != nil {
		return fmt.Errorf("rules-revise LLM call: %w", err)
	}
	revised := stripLLMFences(resp)
	if revised == strings.TrimSpace(current) {
		return nil // no-op proposal
	}
	if err := validateDreamRules(revised); err != nil {
		wd.logger.Warn("wiki-dream: revised rules rejected by contract gate", "error", err, "target", target)
		return nil // gate rejection is a healthy no-op, not a cycle error
	}

	rulesPath := filepath.Join(wd.workspaceDir, wikiDreamRulesFile)
	if err := os.WriteFile(rulesPath+".bak", []byte(current), 0o600); err != nil {
		return fmt.Errorf("backup rules: %w", err)
	}
	if err := atomicWriteFile(rulesPath, []byte(revised+"\n")); err != nil {
		return fmt.Errorf("write revised rules: %w", err)
	}
	event := dreamCompareEntry{
		Ts: dentime.Now().UnixMilli(), Kind: "revision",
		FromHash: shortRulesHash(current), ToHash: shortRulesHash(revised), Target: target,
	}
	if err := wd.appendDreamCompareEntry(event); err != nil {
		return err
	}
	wd.logger.Info("wiki-dream: synthesis rules revised",
		"target", target, "from", event.FromHash, "to", event.ToHash)
	return nil
}

// rollbackDreamRules restores the pre-revision rules from the backup.
func (wd *WikiDreamer) rollbackDreamRules(revision dreamCompareEntry) error {
	rulesPath := filepath.Join(wd.workspaceDir, wikiDreamRulesFile)
	backup, err := os.ReadFile(rulesPath + ".bak")
	if err != nil {
		return fmt.Errorf("rollback: read backup: %w", err)
	}
	if err := atomicWriteFile(rulesPath, backup); err != nil {
		return fmt.Errorf("rollback: restore rules: %w", err)
	}
	event := dreamCompareEntry{
		Ts: dentime.Now().UnixMilli(), Kind: "rollback",
		FromHash: revision.ToHash, ToHash: revision.FromHash,
	}
	if err := wd.appendDreamCompareEntry(event); err != nil {
		return err
	}
	wd.logger.Warn("wiki-dream: synthesis rules rolled back (post-revision loss streak)",
		"from", event.FromHash, "to", event.ToHash)
	return nil
}

// validateDreamRules is the deterministic contract gate: an adoptable rules
// block must keep its shape, size, and every load-bearing invariant line.
func validateDreamRules(text string) error {
	if !strings.HasPrefix(text, "## 규칙") {
		return fmt.Errorf("rules must start with '## 규칙'")
	}
	if len(text) < dreamRulesMinBytes || len(text) > dreamRulesMaxBytes {
		return fmt.Errorf("rules size %d outside [%d, %d]", len(text), dreamRulesMinBytes, dreamRulesMaxBytes)
	}
	required := []string{
		"프로젝트", "인물", "시스템", "업무", "사용자", "기타", // the 6-category vocabulary
		"대표.md", "로그.md", // project folder slots
		"JSON 배열만", // output contract
		"반복신호",     // system-maintained ledger guard
		"추측 금지",    // no-guessing discipline
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			return fmt.Errorf("required invariant %q missing", want)
		}
	}
	return nil
}

// renderWeaknessCounts renders the delta checklist, count desc.
func renderWeaknessCounts(counts map[string]int) string {
	type kv struct {
		tag   string
		count int
	}
	var sorted []kv
	for _, w := range dreamWeaknessVocab {
		if counts[w] > 0 {
			sorted = append(sorted, kv{w, counts[w]})
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })
	if len(sorted) == 0 {
		return "(없음)"
	}
	var b strings.Builder
	for _, s := range sorted {
		fmt.Fprintf(&b, "- [%dx] %s\n", s.count, s.tag)
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderRecentComparisons renders the last n comparison entries as evidence.
func renderRecentComparisons(entries []dreamCompareEntry, n int) string {
	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "- %s: winner=%s", time.UnixMilli(e.Ts).Format("01-02"), e.Winner)
		if len(e.Weaknesses) > 0 {
			fmt.Fprintf(&b, " 약점=%s", strings.Join(e.Weaknesses, ","))
		}
		if e.Rationale != "" {
			fmt.Fprintf(&b, " — %s", e.Rationale)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// atomicWriteFile writes via tmp+rename so a crash cannot leave a torn file.
func atomicWriteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
