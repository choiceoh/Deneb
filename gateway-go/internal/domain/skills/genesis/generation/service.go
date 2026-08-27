// Package generation provides automatic skill creation from session experience.
//
// Inspired by the Hermes agent's skill-manager pattern, genesis monitors
// completed sessions for skill-worthy patterns and auto-generates SKILL.md
// files. It also converts Aurora dreaming summaries into new skills when
// recurring patterns are detected.
//
// The pipeline:
//
//	Session completes → Evaluate (skill-worthy?) → Generate SKILL.md via LLM
//	                  → Persist to ~/.deneb/skills/ → Register in catalog
//
//	Aurora dream summary → Detect recurring pattern → Generate skill
package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// skillDedupThreshold is the Jaccard similarity (over name+description token
// sets) at or above which a generated skill is treated as a duplicate of an
// existing catalog entry and dropped. Conservative on purpose: only near-
// identical skills are rejected.
const skillDedupThreshold = 0.82

// generationMaxTokens bounds one skill-generation completion. Shared by the
// production Generate/GenerateFromDream paths and the L2 genesis shadow bench
// (bench_support.go) so the bench stays a byte-faithful mirror of production.
// 2048 truncated real outputs mid-JSON (meta-evolution ledger observed
// content_chars=2837 at completion_tokens=2048) — a full Korean SKILL.md JSON
// runs to several thousand tokens (persist size guard is 15KB), and glm-class
// models draw reasoning tokens from this same budget.
const generationMaxTokens = 8192

// ErrSkillDeduped is returned by Persist when the generated skill is too
// similar to an existing skill and was intentionally not written. Callers
// must treat this as a no-op, not a failure.
var ErrSkillDeduped = errors.New("genesis: skill deduplicated (too similar to existing skill)")

// Config configures the genesis service.
type Config struct {
	// MinToolCalls is the minimum number of tool calls in a session to
	// consider it for skill extraction. Default: 2.
	MinToolCalls int
	// MinTurns is the minimum number of agent turns. Default: 2.
	MinTurns int
	// OutputDir is the directory to write generated SKILL.md files.
	// Default: ~/.deneb/skills/genesis.
	OutputDir string
	// Model is the LLM model to use for skill generation.
	Model string
	// CooldownPerSkill prevents generating duplicate skills too quickly.
	// Default: 24h.
	CooldownPerSkill time.Duration
	// MaxSkillsPerDay caps daily skill generation to avoid runaway creation.
	// Default: 10. Deneb is single-user, so the cap protects against LLM
	// thrashing rather than billing — we can afford a generous ceiling.
	MaxSkillsPerDay int
}

// DefaultConfig returns production defaults. Pure: no env reads.
func DefaultConfig() Config {
	outputDir := ""
	if stateDir := config.ResolveStateDir(); stateDir != "" {
		outputDir = filepath.Join(stateDir, "skills", "genesis")
	}
	return Config{
		MinToolCalls:     2,
		MinTurns:         2,
		OutputDir:        outputDir,
		CooldownPerSkill: 24 * time.Hour,
		MaxSkillsPerDay:  10,
	}
}

// DefaultConfigFromEnv returns DefaultConfig with DENEB_SKILL_GENESIS_*
// overrides applied. Mirrors the SkillCuratorConfigFromEnv pattern so the
// operator can tune thresholds without rebuilding.
func DefaultConfigFromEnv() Config {
	cfg := DefaultConfig()
	cfg.MaxSkillsPerDay = genesiscommon.EnvInt("DENEB_SKILL_GENESIS_MAX_PER_DAY", cfg.MaxSkillsPerDay)
	cfg.MinToolCalls = genesiscommon.EnvInt("DENEB_SKILL_GENESIS_MIN_TOOL_CALLS", cfg.MinToolCalls)
	cfg.MinTurns = genesiscommon.EnvInt("DENEB_SKILL_GENESIS_MIN_TURNS", cfg.MinTurns)
	return cfg
}

// SessionContext captures the data needed to evaluate and generate a skill
// from a completed session.
type SessionContext struct {
	Key            string
	Label          string
	Model          string
	Turns          int
	ToolActivities []ToolActivity
	AllText        string // full conversation transcript
	RuntimeMs      int64
}

// ToolActivity mirrors agent.ToolActivity for decoupling.
type ToolActivity struct {
	Name    string `json:"name"`
	IsError bool   `json:"isError,omitempty"`
	Input   string `json:"input,omitempty"`
	Output  string `json:"output,omitempty"`
}

// GeneratedSkill is the LLM output for a new skill.
type GeneratedSkill struct {
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Emoji       string   `json:"emoji,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Body        string   `json:"body"` // markdown body after frontmatter
}

// Service orchestrates skill genesis: evaluation, generation, and persistence.
type Service struct {
	cfg       Config
	llmClient *llm.Client
	catalog   *skills.Catalog
	logger    *slog.Logger

	// Optional quality judge (stronger model, judge != producer) that rejects
	// semantic duplicates + low-value skills the specificity heuristic can't
	// catch. nil → heuristic gate only (prior behavior). See SetJudge.
	judgeClient   *llm.Client
	judgeModel    string
	judgeThinking *llm.ThinkingConfig
	// thinkingOff disables reasoning on dual-mode models for the GENERATION
	// calls. The judge got this treatment; generation did not, and the gap was
	// silent until the backlog drainer's failures reached the anomaly ledger —
	// four straight passes returning "1.  **Analyze the Request**:" instead of
	// JSON, which is chain-of-thought spent in the content channel until the
	// 8192-token budget ran out. Nil means "send no directive", which is the
	// correct behavior for single-mode models.
	thinkingOff func(model string) *llm.ThinkingConfig

	mu             sync.Mutex
	recentSkills   map[string]time.Time // skill name → last generated
	dailyCount     int
	dailyCountDate string // YYYY-MM-DD
	unsub          func()
	meta           *MetaArtifacts // RSI P1: prompt artifacts (nil → compiled-in)
}

// NewService creates a genesis service.
func NewService(cfg Config, llmClient *llm.Client, catalog *skills.Catalog, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MinToolCalls == 0 {
		cfg.MinToolCalls = 2
	}
	if cfg.MinTurns == 0 {
		cfg.MinTurns = 2
	}
	if cfg.MaxSkillsPerDay == 0 {
		cfg.MaxSkillsPerDay = 10
	}
	if cfg.CooldownPerSkill == 0 {
		cfg.CooldownPerSkill = 24 * time.Hour
	}
	svc := &Service{
		cfg:          cfg,
		llmClient:    llmClient,
		catalog:      catalog,
		logger:       logger,
		recentSkills: make(map[string]time.Time),
	}
	svc.loadDailyCap()
	return svc
}

// SetJudge wires an optional stronger model to quality-gate generated skills
// (reject semantic duplicates of existing skills and vague/one-off/low-value
// skills the specificity heuristic misses). judge != producer to avoid same-
// family self-preference bias. Safe to call with a nil client (no-op gate).
// SetMetaArtifacts wires the prompt-artifact resolver (RSI P1). Nil keeps
// compiled-in prompts — construction order stays free.
func (s *Service) SetMetaArtifacts(m *MetaArtifacts) {
	s.mu.Lock()
	s.meta = m
	s.mu.Unlock()
}

func (s *Service) metaLoad(name, fallback string) string {
	s.mu.Lock()
	m := s.meta
	s.mu.Unlock()
	return m.Load(name, fallback)
}

// SetJudge configures the client, model, and thinking policy used to judge candidates.
// SetGenerationThinking installs the per-model thinking-disable resolver used
// by every generation call. It takes a func rather than a config because the
// generation model is resolved per call (a session may pin its own).
func (s *Service) SetGenerationThinking(fn func(model string) *llm.ThinkingConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.thinkingOff = fn
}

// thinkingFor resolves the directive for one model, or nil when none applies.
func (s *Service) thinkingFor(model string) *llm.ThinkingConfig {
	s.mu.Lock()
	fn := s.thinkingOff
	s.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(model)
}

func (s *Service) SetJudge(client *llm.Client, model string, thinking *llm.ThinkingConfig) {
	s.judgeClient = client
	s.judgeModel = model
	s.judgeThinking = thinking
}

type dailyCapState struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

func (s *Service) dailyCapPath() string {
	if s.cfg.OutputDir == "" {
		return ""
	}
	return filepath.Join(s.cfg.OutputDir, ".daily-cap.json")
}

// loadDailyCap restores the daily-cap counter from disk at startup so the
// MaxSkillsPerDay limit survives the gateway's frequent SIGUSR1 restarts —
// an in-memory counter otherwise resets every few minutes, defeating the cap.
func (s *Service) loadDailyCap() {
	path := s.dailyCapPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var st dailyCapState
	if json.Unmarshal(data, &st) != nil {
		return
	}
	s.mu.Lock()
	s.dailyCount = st.Count
	s.dailyCountDate = st.Date
	s.mu.Unlock()
}

// saveDailyCapLocked persists the daily-cap counter. Caller must hold s.mu.
// Atomic tmp+rename: this file exists precisely to survive restarts, and a
// restart mid-write used to corrupt it, silently resetting the cap to zero.
func (s *Service) saveDailyCapLocked() {
	path := s.dailyCapPath()
	if path == "" {
		return
	}
	data, err := json.Marshal(dailyCapState{Date: s.dailyCountDate, Count: s.dailyCount})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.logger.Warn("genesis: daily-cap dir create failed", "error", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		s.logger.Warn("genesis: daily-cap write failed; cap may reset on restart", "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		s.logger.Warn("genesis: daily-cap rename failed; cap may reset on restart", "error", err)
	}
}

// Stop is a no-op (genesis is RPC-triggered, not event-driven).
// Session events lack AgentResult data (Turns, ToolActivities), so
// auto-genesis via EventBus is not viable. Use skills.genesis RPC
// or the dream-to-skill periodic task instead.
func (s *Service) Stop() {
	if s.unsub != nil {
		s.unsub()
		s.unsub = nil
	}
}

// Evaluate checks whether a session context is skill-worthy.
func (s *Service) Evaluate(sctx SessionContext) bool {
	// Need enough tool usage to indicate a non-trivial workflow.
	if len(sctx.ToolActivities) < s.cfg.MinToolCalls {
		return false
	}
	if sctx.Turns < s.cfg.MinTurns {
		return false
	}

	// Check daily cap.
	s.mu.Lock()
	defer s.mu.Unlock()
	today := time.Now().Format("2006-01-02")
	if s.dailyCountDate != today {
		s.dailyCount = 0
		s.dailyCountDate = today
		s.saveDailyCapLocked()
	}
	if s.dailyCount >= s.cfg.MaxSkillsPerDay {
		s.logger.Debug("genesis: daily cap reached", "count", s.dailyCount)
		return false
	}

	// Require diverse tool usage — at least 2 distinct tools.
	toolSet := make(map[string]struct{})
	for _, ta := range sctx.ToolActivities {
		toolSet[ta.Name] = struct{}{}
	}
	return len(toolSet) >= 2
}

// EvaluateReview checks whether a session has enough signal to run the
// background skill-review fork. This is deliberately broader than Evaluate:
// Evaluate gates immediate genesis/persist decisions, while review only records
// a routed proposal or no-op and can catch single-tool corrections, repeated
// verification misses, and near-miss evolution opportunities for later
// accumulation.
func (s *Service) EvaluateReview(sctx SessionContext) bool {
	minTools := s.cfg.MinToolCalls
	if minTools <= 0 || minTools > 2 {
		minTools = 2
	}
	if len(sctx.ToolActivities) < minTools {
		return false
	}
	minTurns := s.cfg.MinTurns
	if minTurns <= 0 || minTurns > 1 {
		minTurns = 1
	}
	return sctx.Turns >= minTurns
}

// Generate calls the LLM to synthesize a skill from the session context.
// Returns nil if the LLM determines no skill is worth creating.
func (s *Service) Generate(ctx context.Context, sctx SessionContext) (*GeneratedSkill, error) {
	// Build tool activity summary.
	toolSummary := buildToolSummary(sctx.ToolActivities)

	// Truncate transcript for token budget.
	transcript := sctx.AllText
	if len([]rune(transcript)) > 8000 {
		runes := []rune(transcript)
		transcript = string(runes[:8000]) + "\n...(truncated)"
	}

	// Build existing skill names for dedup.
	existingSkills := s.listExistingSkillNames()

	userPrompt := fmt.Sprintf(`## 완료된 세션 정보
- 세션 키: %s
- 라벨: %s
- 도구 사용 요약: %s
- 에이전트 턴 수: %d

## 기존 스킬 목록 (중복 방지)
%s

## 대화 내용 (요약)
%s`, sctx.Key, sctx.Label, toolSummary, sctx.Turns,
		strings.Join(existingSkills, ", "),
		transcript)

	events, err := s.llmClient.StreamChat(ctx, llm.ChatRequest{
		Model:          s.resolveModel(sctx.Model),
		Thinking:       s.thinkingFor(s.resolveModel(sctx.Model)),
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(s.metaLoad(MetaGenesisSystemPrompt, genesisSystemPrompt)),
		MaxTokens:      generationMaxTokens,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, fmt.Errorf("genesis LLM call: %w", err)
	}
	if events == nil {
		return nil, fmt.Errorf("genesis LLM: nil event channel")
	}

	gen, perr := parseGenesisResponse(llm.DrainStreamText(events))
	return s.gateGenerated(ctx, gen, perr)
}

// GenerateFromDream creates a skill from an Aurora dreaming summary.
// This is the dream-to-skill pipeline entry point.
func (s *Service) GenerateFromDream(ctx context.Context, summaryContent string) (*GeneratedSkill, error) {
	existingSkills := s.listExistingSkillNames()

	userPrompt := fmt.Sprintf(`## 드리밍 요약 (Aurora compaction summary)
%s

## 기존 스킬 목록 (중복 방지)
%s

위 요약에서 반복되는 워크플로우 패턴이나 재사용 가능한 절차를 스킬로 추출하세요.
단발성 작업이나 이미 기존 스킬이 커버하는 내용이면 skip을 반환하세요.`,
		genesiscommon.TruncateRunes(summaryContent, 8000),
		strings.Join(existingSkills, ", "))

	events, err := s.llmClient.StreamChat(ctx, llm.ChatRequest{
		Model:          s.cfg.Model,
		Thinking:       s.thinkingFor(s.cfg.Model),
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(s.metaLoad(MetaGenesisSystemPrompt, genesisSystemPrompt)),
		MaxTokens:      generationMaxTokens,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, fmt.Errorf("genesis-dream LLM call: %w", err)
	}
	if events == nil {
		return nil, fmt.Errorf("genesis-dream LLM: nil event channel")
	}

	gen, perr := parseGenesisResponse(llm.DrainStreamText(events))
	return s.gateGenerated(ctx, gen, perr)
}

// GenerateFromBacklog creates a skill from a recurring demand brief mined out
// of the Propus opportunity backlog (route=genesis signals that no session
// followed through on). Same contract as GenerateFromDream: the LLM only
// drafts; gateGenerated + Persist keep the deterministic accept/dedup gates.
func (s *Service) GenerateFromBacklog(ctx context.Context, brief string) (*GeneratedSkill, error) {
	existingSkills := s.listExistingSkillNames()

	userPrompt := fmt.Sprintf(`## 반복 수요 브리프 (스킬 기회 백로그 — 여러 세션에서 같은 필요가 관측됨)
%s

## 기존 스킬 목록 (중복 방지)
%s

위 브리프는 실제 사용 세션들이 스킬 신설 가치가 있다고 판단해 남긴 반복 신호입니다.
브리프가 요구하는 재사용 가능한 절차를 스킬로 작성하세요. 이미 기존 스킬이 커버하거나
내장 도구 한 번 호출로 끝나는 내용이면 skip을 반환하세요.`,
		genesiscommon.TruncateRunes(brief, 8000),
		strings.Join(existingSkills, ", "))

	events, err := s.llmClient.StreamChat(ctx, llm.ChatRequest{
		Model:          s.cfg.Model,
		Thinking:       s.thinkingFor(s.cfg.Model),
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(s.metaLoad(MetaGenesisSystemPrompt, genesisSystemPrompt)),
		MaxTokens:      generationMaxTokens,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, fmt.Errorf("genesis-backlog LLM call: %w", err)
	}
	if events == nil {
		return nil, fmt.Errorf("genesis-backlog LLM: nil event channel")
	}

	gen, perr := parseGenesisResponse(llm.DrainStreamText(events))
	return s.gateGenerated(ctx, gen, perr)
}

// gateGenerated applies a specificity gate to a freshly generated skill before
// it reaches Persist. Self-generated skills are, on average, net-harmful unless
// curated (SkillsBench: human-curated +16.2pp vs self-generated -1.3pp; SoK
// arXiv:2602.20867) and the dominant failure mode is vagueness — "correct but
// not actionable" (EvolveR arXiv:2510.16079, 50% of low-score principles). A
// rejected skill is treated like a skip (nil, nil), not an error: the session
// pattern can regenerate later, and we log the issues for gate tuning rather
// than spamming the operator with a non-failure.
func (s *Service) gateGenerated(ctx context.Context, skill *GeneratedSkill, err error) (*GeneratedSkill, error) {
	if err != nil || skill == nil {
		return skill, err
	}
	if issues := skillSpecificityIssues(skill); len(issues) > 0 {
		s.logger.Info("genesis: specificity gate rejected skill",
			"skill", skill.Name, "issues", strings.Join(issues, "; "))
		return nil, nil
	}
	if pass, reason := s.judgeGenerated(ctx, skill); !pass {
		s.logger.Info("genesis: quality judge rejected skill",
			"skill", skill.Name, "reason", reason)
		return nil, nil
	}
	return skill, nil
}

// judgeGenerated runs the LLM quality gate on a candidate that already passed
// the specificity heuristic: it rejects semantic duplicates of existing skills
// and vague/one-off/low-value skills the structural checks can't see. This is
// the genesis counterpart to the evolver's self-test judge. Fail-OPEN: with no
// judge wired or on any judge call/parse error, fall back to the heuristic gate
// alone (the prior behavior) rather than blocking all genesis on a model hiccup
// — the heuristic still guards against the dominant (vagueness) failure mode.
func (s *Service) judgeGenerated(ctx context.Context, skill *GeneratedSkill) (pass bool, reason string) {
	if s.judgeClient == nil || s.judgeModel == "" {
		return true, ""
	}
	userPrompt := fmt.Sprintf(`## 후보 스킬
이름: %s
설명: %s

본문:
%s

## 기존 스킬 (중복 판정용)
%s`, skill.Name, skill.Description, skill.Body, s.listExistingSkillDescriptions())

	events, err := s.judgeClient.StreamChat(ctx, llm.ChatRequest{
		Model:    s.judgeModel,
		Messages: []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:   llm.SystemString(s.metaLoad(MetaGenesisJudgeSystemPrompt, genesisJudgeSystemPrompt)),
		// 4096, not 1024: reasoning models that ignore thinking=disabled spend
		// the budget on chain-of-thought and return empty content, and this
		// judge FAILS OPEN ("accepting on heuristic") — a starved budget
		// silently weakens the acceptance gate (same class as the relevance
		// classifier starvation fixed in #4007).
		MaxTokens:      4096,
		Stream:         true,
		Thinking:       s.judgeThinking,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		s.logger.Warn("genesis: judge unavailable, accepting on heuristic", "skill", skill.Name, "error", err)
		return true, ""
	}
	extracted := jsonutil.ExtractObject(llm.DrainStreamText(events))
	if extracted == "" {
		s.logger.Warn("genesis: judge empty verdict, accepting on heuristic", "skill", skill.Name)
		return true, ""
	}
	var resp struct {
		Pass   bool   `json:"pass"`
		Reason string `json:"reason"`
	}
	if jerr := json.Unmarshal([]byte(extracted), &resp); jerr != nil {
		s.logger.Warn("genesis: judge parse failed, accepting on heuristic", "skill", skill.Name, "error", jerr)
		return true, ""
	}
	return resp.Pass, resp.Reason
}

// skillSpecificityIssues returns the reasons a generated skill is too vague to
// be worth persisting, or nil if it passes. The checks mirror the structure the
// genesis prompt asks for (When to Use / Procedure / Verification, description
// with a "Use when" trigger) and target the vagueness failure mode.
func skillSpecificityIssues(skill *GeneratedSkill) []string {
	var issues []string
	body := strings.TrimSpace(skill.Body)
	lower := strings.ToLower(body)

	if n := len([]rune(body)); n < 400 {
		issues = append(issues, fmt.Sprintf("본문이 너무 짧음(%d자<400)", n))
	}
	// The load-bearing contract says WHEN the skill applies, HOW to act, and
	// what observable state means DONE. Pitfalls remains optional.
	if !strings.Contains(lower, "when to use") {
		issues = append(issues, "When to Use 섹션 누락(트리거 불명)")
	}
	if !strings.Contains(lower, "procedure") {
		issues = append(issues, "Procedure 섹션 누락(절차 없음)")
	}
	if !strings.Contains(lower, "verification") {
		issues = append(issues, "Verification 섹션 누락(완료 기준 없음)")
	}
	if !hasActionableInstruction(sectionBody(body, "procedure")) {
		issues = append(issues, "구체적 실행 지시 부재(도구/명령/구조화된 판단 기준 없음)")
	}
	if !hasActionableInstruction(sectionBody(body, "verification")) {
		issues = append(issues, "관찰 가능한 완료 기준 부재")
	}
	desc := strings.ToLower(skill.Description)
	if !strings.Contains(desc, "use when") && !strings.Contains(skill.Description, "트리거") && !strings.Contains(desc, "when:") {
		issues = append(issues, "description에 트리거(Use when) 없음")
	}
	return issues
}

// hasActionableInstruction accepts exact commands, mandatory ordered steps, or
// two structured decision/boundary criteria. This keeps vague prose out without
// forcing every high-freedom task into an invented step-by-step recipe.
func hasActionableInstruction(body string) bool {
	if strings.Contains(body, "`") { // inline tool, command, field, or exact check
		return true
	}
	bullets := 0
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if len(t) >= 2 && t[0] >= '1' && t[0] <= '9' && (t[1] == '.' || t[1] == ')') {
			return true
		}
		if strings.HasPrefix(t, "- ") && len([]rune(strings.TrimSpace(t[2:]))) >= 12 {
			bullets++
		}
	}
	return bullets >= 2
}

// sectionBody returns the content below a level-two heading until the next
// level-two heading. Heading matching is case-insensitive.
func sectionBody(body, name string) string {
	want := strings.ToLower(strings.TrimSpace(name))
	var out strings.Builder
	inSection := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			heading := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			if inSection && heading != want {
				break
			}
			inSection = heading == want
			continue
		}
		if inSection {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// Persist writes a generated skill to disk and registers it in the catalog.
func (s *Service) Persist(skill *GeneratedSkill) error {
	if skill == nil || skill.Name == "" {
		return fmt.Errorf("genesis: nil or unnamed skill")
	}

	// Validate name: lowercase, hyphens only.
	name := genesiscommon.SanitizeSkillName(skill.Name)
	if name == "" {
		return fmt.Errorf("genesis: invalid skill name %q", skill.Name)
	}

	// Sanitize the description before it is written to frontmatter and
	// surfaced in the skill catalog index — which is cached in the
	// semi-static system prompt block. Collapsing to one clean line means a
	// generated description cannot inject newlines/control chars into the
	// frontmatter or the prompt-cached index. (A comparable agent wraps all
	// re-injected skill text as untrusted; Deneb keeps the index in the
	// system block for cache stability and sanitizes at the source instead.)
	skill.Description = sanitizeSkillDescription(skill.Description)

	// Reject near-duplicates of existing skills. The generation prompt is
	// already given the existing skill names, but the LLM can ignore that;
	// this is the code-level backstop against runaway near-identical skills.
	if s.isDuplicateSkill(name, skill.Description) {
		s.logger.Info("genesis: skill deduplicated", "skill", name)
		return ErrSkillDeduped
	}

	// Check cooldown.
	s.mu.Lock()
	if last, ok := s.recentSkills[name]; ok && time.Since(last) < s.cfg.CooldownPerSkill {
		s.mu.Unlock()
		return fmt.Errorf("genesis: skill %q on cooldown", name)
	}
	s.mu.Unlock()

	// Create directory structure.
	category := skill.Category
	if category == "" {
		category = "genesis"
	}
	skillDir := filepath.Join(s.cfg.OutputDir, category, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("genesis: mkdir %s: %w", skillDir, err)
	}

	// Build SKILL.md content.
	content := buildSkillMD(name, skill)

	skillPath := filepath.Join(skillDir, "SKILL.md")
	// Atomic write: a crash mid-write must not leave a half-written SKILL.md
	// that the catalog/loader would then parse. Perm 0o644 keeps the file
	// world-readable as before.
	if err := atomicfile.WriteFile(skillPath, []byte(content), &atomicfile.Options{Perm: 0o644}); err != nil {
		return fmt.Errorf("genesis: write %s: %w", skillPath, err)
	}

	// Register in catalog.
	if s.catalog != nil {
		entry := skills.SkillEntry{
			Skill: skills.Skill{
				Name:        name,
				Description: skill.Description,
				Dir:         skillDir,
				FilePath:    skillPath,
				Category:    category,
				Version:     "0.1.0",
				Source:      skills.SourceManaged,
			},
			Frontmatter: skills.ParsedFrontmatter{
				"name":        name,
				"version":     "0.1.0",
				"category":    category,
				"description": skill.Description,
			},
		}
		s.catalog.Register(entry)
	}

	// Update rate limiting state.
	s.mu.Lock()
	s.recentSkills[name] = time.Now()
	s.dailyCount++
	s.saveDailyCapLocked()
	s.mu.Unlock()

	return nil
}

// listExistingSkillNames returns all registered skill names for dedup.
func (s *Service) listExistingSkillNames() []string {
	if s.catalog == nil {
		return nil
	}
	entries := s.catalog.List()
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Skill.Name)
	}
	return names
}

// listExistingSkillDescriptions returns the existing skills as "- name: desc"
// lines for the genesis judge's redundancy check (descriptions capture "what /
// when" far better than names alone, which token-Jaccard dedup can't compare).
func (s *Service) listExistingSkillDescriptions() string {
	if s.catalog == nil {
		return "(없음)"
	}
	entries := s.catalog.List()
	if len(entries) == 0 {
		return "(없음)"
	}
	var b strings.Builder
	for _, e := range entries {
		desc := e.Skill.Description
		if r := []rune(desc); len(r) > 200 {
			desc = string(r[:200]) + "…"
		}
		fmt.Fprintf(&b, "- %s: %s\n", e.Skill.Name, desc)
	}
	return b.String()
}

func (s *Service) resolveModel(sessionModel string) string {
	if s.cfg.Model != "" {
		return s.cfg.Model
	}
	if sessionModel != "" {
		return sessionModel
	}
	return "gemini-2.5-flash"
}

// buildToolSummary creates a compact tool usage string.
func buildToolSummary(activities []ToolActivity) string {
	counts := make(map[string]int)
	errors := make(map[string]int)
	for _, a := range activities {
		counts[a.Name]++
		if a.IsError {
			errors[a.Name]++
		}
	}
	var parts []string
	for name, count := range counts {
		s := fmt.Sprintf("%s(%d)", name, count)
		if e := errors[name]; e > 0 {
			s += fmt.Sprintf("[err:%d]", e)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// parseGenesisResponse parses the LLM JSON output.
func parseGenesisResponse(text string) (*GeneratedSkill, error) {
	extracted := jsonutil.ExtractObject(text)
	if extracted == "" {
		extracted = strings.TrimSpace(text)
	}

	var resp struct {
		Skip   bool            `json:"skip"`
		Reason string          `json:"reason,omitempty"`
		Skill  *GeneratedSkill `json:"skill,omitempty"`
	}
	if err := json.Unmarshal([]byte(extracted), &resp); err != nil {
		// Try direct skill parse.
		var skill GeneratedSkill
		if err2 := json.Unmarshal([]byte(extracted), &skill); err2 != nil {
			return nil, fmt.Errorf("genesis: parse response: %w (raw: %s)", err, genesiscommon.TruncateRunes(text, 200))
		}
		if skill.Name == "" {
			return nil, nil
		}
		skill.Description = normalizeSkillDescription(skill.Description)
		return &skill, nil
	}
	if resp.Skip || resp.Skill == nil {
		return nil, nil
	}
	resp.Skill.Description = normalizeSkillDescription(resp.Skill.Description)
	return resp.Skill, nil
}

// buildSkillMD generates the SKILL.md content from a GeneratedSkill.
func buildSkillMD(name string, skill *GeneratedSkill) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "name: %s\n", name)
	sb.WriteString("version: \"0.1.0\"\n")
	fmt.Fprintf(&sb, "category: %s\n", skill.Category)
	fmt.Fprintf(&sb, "description: %q\n", skill.Description)

	// Build metadata block.
	meta := map[string]any{}
	deneb := map[string]any{}
	if skill.Emoji != "" {
		deneb["emoji"] = skill.Emoji
	}
	if len(skill.Tags) > 0 {
		deneb["tags"] = skill.Tags
	}
	deneb["origin"] = "genesis"
	meta["deneb"] = deneb
	if metaJSON, err := json.Marshal(meta); err == nil {
		fmt.Fprintf(&sb, "metadata: %s\n", string(metaJSON))
	}
	sb.WriteString("---\n\n")
	sb.WriteString(skill.Body)
	sb.WriteString("\n")
	return sb.String()
}

// maxSkillDescriptionRunes bounds an auto-generated skill description. The
// description lands in the skills compact-index that the system prompt loads
// every turn (semi-static cache), so a runaway or multi-line description bloats
// that budget and reads poorly in the picker. Hermes' house standard is ≤60
// chars; Deneb keeps a more generous bound because Korean "Use when" triggers
// run longer, and clamps at a word boundary rather than rejecting — a clean
// one-liner beats forcing a regeneration.
const maxSkillDescriptionRunes = 120

// normalizeSkillDescription flattens a generated description to a single line
// (collapsing any newlines / runs of whitespace the model emitted) and clamps
// it to maxSkillDescriptionRunes, backing up to a word boundary when possible.
// The "Use when" trigger sits at the front, so clamping the tail preserves it.
func normalizeSkillDescription(desc string) string {
	desc = strings.Join(strings.Fields(desc), " ")
	r := []rune(desc)
	if len(r) <= maxSkillDescriptionRunes {
		return desc
	}
	cut := r[:maxSkillDescriptionRunes]
	lastSpace := -1
	for i, c := range cut {
		if c == ' ' {
			lastSpace = i
		}
	}
	if lastSpace > maxSkillDescriptionRunes/2 {
		cut = cut[:lastSpace]
	}
	return strings.TrimRight(string(cut), " ,;·-") + "…"
}

// isDuplicateSkill reports whether a skill with this name+description is a
// near-duplicate of an existing catalog entry. An exact name collision is
// always a duplicate; otherwise it compares name+description token sets with
// Jaccard similarity. Owner-agnostic — Deneb is single-user.
func (s *Service) isDuplicateSkill(name, description string) bool {
	if s.catalog == nil {
		return false
	}
	cand := genesiscommon.SkillDedupTokens(name, description)
	if len(cand) == 0 {
		return false
	}
	for _, e := range s.catalog.List() {
		if e.Skill.Name == name {
			return true
		}
		if genesiscommon.JaccardSimilarity(cand, genesiscommon.SkillDedupTokens(e.Skill.Name, e.Skill.Description)) >= skillDedupThreshold {
			return true
		}
	}
	return false
}

// sanitizeSkillDescription collapses a generated description to a single clean
// line: newlines/tabs/control chars become spaces, whitespace runs collapse,
// and the result is rune-length-capped. This protects both the SKILL.md
// frontmatter and the prompt-cached skill index from structure-breaking or
// injected text.
func sanitizeSkillDescription(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	const maxDescRunes = 300
	if r := []rune(s); len(r) > maxDescRunes {
		s = string(r[:maxDescRunes]) + "…"
	}
	return s
}
