package genesis

// Curriculum lane — RSI roadmap P5 workstream 1 (demand generation), first
// slice.
//
// The fast loop is demand-limited: one operator's organic usage yields a
// handful of underperformer signals a week, and every layer above (labels,
// meta evidence, L4 supply) inherits that famine. Reviews mine INCIDENTS —
// what just failed; this lane mines COVERAGE — what is missing. Each cycle it
// shows the strongest wired model the whole catalog, the usage shape, and the
// opportunity backlog, asks for at most ONE genuinely missing capability,
// authors its validation cases FIRST (test-first: the cases outlive the
// proposal), and files a route=genesis opportunity for the EXISTING review
// machinery to route. Nothing is created here — the lane injects demand;
// routing and every creation gate stay with the reviews (LLM produces,
// deterministic Go decides).
//
// Provenance (ground rule 3): the opportunity and its cases carry
// source="curriculum" so synthetic demand never masquerades as operator
// demand in usage statistics or review evidence.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	common "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const (
	// curriculumDefaultInterval keeps demand injection below review-routing
	// capacity: one proposal every ~3 days is fuel, not flooding.
	curriculumDefaultInterval = 72 * time.Hour
	// curriculumSourceTag marks everything this lane writes.
	curriculumSourceTag = "curriculum"
	// curriculumDedupWindow suppresses re-proposing a capability the backlog
	// already carries (any route, any source) within this window.
	curriculumDedupWindow = 14 * 24 * time.Hour
	// curriculumMaxCases bounds authored validation cases per proposal.
	curriculumMaxCases = 3
	// curriculumCatalogCap bounds the catalog listing in the evidence prompt.
	curriculumCatalogCap = 60
	// curriculumBriefMinBytes/MaxBytes bound the capability brief — too short
	// cannot seed a skill, too long is a skill body smuggled past genesis.
	curriculumBriefMinBytes = 40
	curriculumBriefMaxBytes = 2000
)

// curriculumSystemPrompt is a compiled constant on purpose (like the meta
// governor prompt): the lane that INVENTS demand must not be able to loosen
// its own honesty bar. Externalizing it is a P5-4 decision, not a default.
const curriculumSystemPrompt = `당신은 비서실장 에이전트의 능력 커리큘럼 설계자다.
주어진 것: 현재 스킬 카탈로그, 사용 통계 형상, 이미 알려진 수요 백로그.
과제: 운영자에게 반복적·구체적 가치가 있는데 카탈로그에 정말 없는 능력을 최대 1개 제안한다.

규칙:
- 기존 스킬로 이미 가능한 것, 백로그에 이미 있는 것의 재제안 금지.
- 막연한 능력("더 똑똑하게") 금지 — 트리거와 산출물이 구체적이어야 한다.
- 확신이 없으면 반드시 skip=true. 제안 0개가 나쁜 제안 1개보다 낫다.
- 검증 케이스를 먼저 설계한다: 각 케이스는 사용자 입력(input)과 관찰 가능한
  문자열 단언(required_substrings/forbidden_substrings)으로 스킬 없이도 채점
  가능해야 한다.
- 각 단언은 2자 이상의 완결된 단어/구여야 한다 ("납" 같은 잘린 조각 금지 —
  기대 응답에 실제로 그대로 나타날 문자열만).

JSON으로만 응답:
{"skip":bool,"reason":"스킵/제안 근거 한 줄","name":"kebab-case-능력명(영문)",
"brief":"이 능력이 무엇이고 언제 발동하며 무엇을 산출하는지 3-6문장",
"evidence":"환경의 어떤 신호가 이 수요를 뒷받침하는지",
"cases":[{"description":"케이스 한 줄","input":"사용자 입력",
"required_substrings":["기대 응답에 반드시 담길 문자열"],
"forbidden_substrings":["담기면 안 되는 문자열"]}]}`

// curriculumCase is one authored validation case in the LLM response.
type curriculumCase struct {
	Description         string   `json:"description,omitempty"`
	Input               string   `json:"input,omitempty"`
	RequiredSubstrings  []string `json:"required_substrings,omitempty"`
	ForbiddenSubstrings []string `json:"forbidden_substrings,omitempty"`
}

// curriculumResp is the producer's verdict for one curriculum cycle.
type curriculumResp struct {
	Skip     bool             `json:"skip"`
	Reason   string           `json:"reason,omitempty"`
	Name     string           `json:"name,omitempty"`
	Brief    string           `json:"brief,omitempty"`
	Evidence string           `json:"evidence,omitempty"`
	Cases    []curriculumCase `json:"cases,omitempty"`
}

// CurriculumTask runs the demand-generation lane on the autonomous scheduler.
// Registered production-gated (live LLM call + shared genesis writes).
type CurriculumTask struct {
	Evolver *Evolver
	Tracker *Tracker
	Logger  *slog.Logger

	// EnvDigest, when set, injects a compact environment summary (wiki
	// domains, active work) assembled by the server — the slice-2 hook that
	// widens demand mining beyond tracker-local evidence. Nil is fine.
	EnvDigest func(ctx context.Context) string

	// proposeFn overrides the LLM executor in tests.
	proposeFn func(ctx context.Context, evidence string) (curriculumResp, error)
	// catalogFn overrides catalog resolution in tests (name → description).
	catalogFn func() map[string]string
}

// Name identifies the task in the autonomous scheduler.
func (t *CurriculumTask) Name() string { return "curriculum" }

// Interval honors DENEB_CURRICULUM_INTERVAL_HOURS (calibration knob).
func (t *CurriculumTask) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_CURRICULUM_INTERVAL_HOURS")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return curriculumDefaultInterval
}

// Run executes one demand-generation cycle.
func (t *CurriculumTask) Run(ctx context.Context) error {
	if t.Tracker == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	propose := t.proposeFn
	if propose == nil {
		if t.Evolver == nil {
			return nil
		}
		propose = t.llmPropose
	}

	catalog := t.catalogNames()
	backlog, err := t.Tracker.RecentSkillOpportunities("", 30)
	if err != nil {
		backlog = nil // fail-open: a torn backlog must not stop demand mining
	}

	resp, err := propose(ctx, t.assembleDemandEvidence(ctx, catalog, backlog))
	if err != nil {
		logger.Warn("curriculum: propose failed", "error", err)
		return nil
	}
	if resp.Skip {
		logger.Info("curriculum: no capability cleared the bar",
			"reason", common.TruncateRunes(resp.Reason, 160))
		return nil
	}
	if reason := curriculumGate(resp, catalog, backlog, time.Now()); reason != "" {
		logger.Info("curriculum: proposal rejected", "name", resp.Name, "reason", reason)
		return nil
	}

	// Cases FIRST: demand must never be filed without its held-out oracle.
	name := normalizeCurriculumName(resp.Name)
	for i, c := range resp.Cases {
		rec := SkillValidationCaseRecord{
			SkillName:           name,
			Description:         strings.TrimSpace(c.Description),
			RequiredSubstrings:  c.RequiredSubstrings,
			ForbiddenSubstrings: c.ForbiddenSubstrings,
			Replay:              SkillReplayCaseRecord{Input: strings.TrimSpace(c.Input)},
			Source:              curriculumSourceTag,
		}
		if err := t.Tracker.RecordSkillValidationCase(rec); err != nil {
			logger.Warn("curriculum: case record failed — demand not filed",
				"name", name, "case", i, "error", err)
			return nil
		}
	}
	if err := t.Tracker.RecordSkillOpportunity(SkillOpportunityRecord{
		Candidate: name + ": " + strings.TrimSpace(resp.Brief),
		Route:     "genesis",
		SkillName: name,
		Evidence:  strings.TrimSpace(resp.Evidence),
		Reason:    strings.TrimSpace(resp.Reason),
		Source:    curriculumSourceTag,
	}); err != nil {
		logger.Warn("curriculum: opportunity record failed", "name", name, "error", err)
		return nil
	}
	logger.Info("curriculum: capability demand filed (route=genesis, propose-only)",
		"name", name, "cases", len(resp.Cases))
	return nil
}

// catalogNames resolves the current skill catalog as name → description.
func (t *CurriculumTask) catalogNames() map[string]string {
	if t.catalogFn != nil {
		return t.catalogFn()
	}
	out := map[string]string{}
	if t.Evolver == nil {
		return out
	}
	for _, entry := range t.Evolver.catalogEntries() {
		out[entry.Skill.Name] = entry.Skill.Description
	}
	return out
}

// assembleDemandEvidence builds the deterministic evidence block: the catalog
// (what exists), the usage shape (what is exercised), the backlog (what is
// already known demand — re-proposal forbidden), and the optional environment
// digest.
func (t *CurriculumTask) assembleDemandEvidence(ctx context.Context, catalog map[string]string, backlog []SkillOpportunityRecord) string {
	var b strings.Builder

	names := make([]string, 0, len(catalog))
	for name := range catalog {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > curriculumCatalogCap {
		names = names[:curriculumCatalogCap]
	}
	b.WriteString("## 현재 스킬 카탈로그\n")
	if len(names) == 0 {
		b.WriteString("(비어 있음)\n")
	}
	for _, name := range names {
		desc := common.TruncateRunes(strings.SplitN(catalog[name], "\n", 2)[0], 100)
		fmt.Fprintf(&b, "- %s — %s\n", name, desc)
	}

	if stats, err := t.Tracker.ListAllStats(); err == nil && len(stats) > 0 {
		sort.Slice(stats, func(i, j int) bool { return stats[i].TotalUses > stats[j].TotalUses })
		b.WriteString("\n## 사용 형상 (상위)\n")
		for i, s := range stats {
			if i >= 10 {
				break
			}
			fmt.Fprintf(&b, "- %s: %d회 (실패 %d)\n", s.SkillName, s.TotalUses, s.FailureCount)
		}
	}

	if len(backlog) > 0 {
		b.WriteString("\n## 이미 알려진 수요 백로그 (재제안 금지)\n")
		for i, rec := range backlog {
			if i >= 20 {
				break
			}
			fmt.Fprintf(&b, "- [%s] %s\n", rec.Route, common.TruncateRunes(rec.Candidate, 100))
		}
	}

	if t.EnvDigest != nil {
		if digest := strings.TrimSpace(t.EnvDigest(ctx)); digest != "" {
			b.WriteString("\n## 환경 요약\n")
			b.WriteString(common.TruncateRunes(digest, 2000))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// llmPropose asks the strongest wired model for one capability proposal.
func (t *CurriculumTask) llmPropose(ctx context.Context, evidence string) (curriculumResp, error) {
	client, model := t.Evolver.teacherModelSnapshot()
	if client == nil {
		client, model = t.Evolver.primaryModel()
	}
	if client == nil {
		return curriculumResp{}, fmt.Errorf("curriculum: no model wired")
	}
	text, err := client.Complete(ctx, llm.ChatRequest{
		Model:          model,
		Messages:       []llm.Message{llm.NewTextMessage("user", evidence)},
		System:         llm.SystemString(curriculumSystemPrompt),
		MaxTokens:      8192,
		Temperature:    evolveTemperature(),
		Thinking:       t.Evolver.thinkingOff(model),
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return curriculumResp{}, fmt.Errorf("curriculum LLM call: %w", err)
	}
	resp, perr := jsonutil.UnmarshalLLM[curriculumResp](text)
	if perr != nil {
		return curriculumResp{}, fmt.Errorf("curriculum: parse response (tail=%q): %w", tailRunes(text, 120), perr)
	}
	return resp, nil
}

// normalizeCurriculumName lowercases a proposed capability name into the
// kebab-case shape the skill catalog uses.
func normalizeCurriculumName(name string) string {
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(name)), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	return strings.Join(fields, "-")
}

// curriculumGate is the deterministic admissibility contract for a proposal.
// Returns "" when admissible, else the rejection reason. The gate is
// intentionally strict: this lane INVENTS demand, so a dropped good idea
// costs one cycle while an admitted bad one costs review budget downstream.
func curriculumGate(resp curriculumResp, catalog map[string]string, backlog []SkillOpportunityRecord, now time.Time) string {
	name := normalizeCurriculumName(resp.Name)
	if len([]rune(name)) < 4 {
		return fmt.Sprintf("name %q too short after normalization", resp.Name)
	}
	brief := strings.TrimSpace(resp.Brief)
	if len(brief) < curriculumBriefMinBytes {
		return fmt.Sprintf("brief too short (%d bytes < %d)", len(brief), curriculumBriefMinBytes)
	}
	if len(brief) > curriculumBriefMaxBytes {
		return fmt.Sprintf("brief too large (%d bytes > %d) — a skill body does not belong here", len(brief), curriculumBriefMaxBytes)
	}
	for existing := range catalog {
		if normalizeCurriculumName(existing) == name {
			return fmt.Sprintf("capability %q already in the catalog", name)
		}
	}
	cutoff := now.Add(-curriculumDedupWindow).UnixMilli()
	proposedKey := opportunityPatternKey(name + ": " + brief)
	for _, rec := range backlog {
		if rec.CreatedAt < cutoff {
			continue
		}
		if rec.SkillName != "" && normalizeCurriculumName(rec.SkillName) == name {
			return fmt.Sprintf("capability %q already in the backlog", name)
		}
		if key := opportunityPatternKey(rec.Candidate); key != "" && key == proposedKey {
			return "candidate echoes a recent backlog entry"
		}
	}
	if len(resp.Cases) == 0 || len(resp.Cases) > curriculumMaxCases {
		return fmt.Sprintf("cases=%d out of bounds [1,%d] — demand without a held-out oracle is not admissible", len(resp.Cases), curriculumMaxCases)
	}
	for i, c := range resp.Cases {
		if strings.TrimSpace(c.Input) == "" {
			return fmt.Sprintf("case %d has no input", i)
		}
		if len(c.RequiredSubstrings)+len(c.ForbiddenSubstrings) == 0 {
			return fmt.Sprintf("case %d has no assertion", i)
		}
		// Floor of 2 runes: single-rune assertions ("네") are noise, while
		// 2-rune Korean words ("안건") are legitimate oracle material.
		for _, s := range append(append([]string{}, c.RequiredSubstrings...), c.ForbiddenSubstrings...) {
			if len([]rune(strings.TrimSpace(s))) < 2 {
				return fmt.Sprintf("case %d has a trivial assertion %q", i, s)
			}
		}
	}
	return ""
}
