// Package genesis provides automatic skill creation from session experience.
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
package genesis

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
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// skillDedupThreshold is the Jaccard similarity (over name+description token
// sets) at or above which a generated skill is treated as a duplicate of an
// existing catalog entry and dropped. Conservative on purpose: only near-
// identical skills are rejected.
const skillDedupThreshold = 0.82

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
	if home, err := os.UserHomeDir(); err == nil {
		outputDir = filepath.Join(home, ".deneb", "skills", "genesis")
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
	cfg.MaxSkillsPerDay = envInt("DENEB_SKILL_GENESIS_MAX_PER_DAY", cfg.MaxSkillsPerDay)
	cfg.MinToolCalls = envInt("DENEB_SKILL_GENESIS_MIN_TOOL_CALLS", cfg.MinToolCalls)
	cfg.MinTurns = envInt("DENEB_SKILL_GENESIS_MIN_TURNS", cfg.MinTurns)
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

	mu             sync.Mutex
	recentSkills   map[string]time.Time // skill name → last generated
	dailyCount     int
	dailyCountDate string // YYYY-MM-DD
	unsub          func()
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
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(genesisSystemPrompt),
		MaxTokens:      2048,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, fmt.Errorf("genesis LLM call: %w", err)
	}
	if events == nil {
		return nil, fmt.Errorf("genesis LLM: nil event channel")
	}

	var sb strings.Builder
	for ev := range events {
		if ev.Type == "content_block_delta" {
			var delta struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
				sb.WriteString(delta.Delta.Text)
			}
		}
	}

	gen, perr := parseGenesisResponse(sb.String())
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
		truncateRunes(summaryContent, 8000),
		strings.Join(existingSkills, ", "))

	events, err := s.llmClient.StreamChat(ctx, llm.ChatRequest{
		Model:          s.cfg.Model,
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(genesisSystemPrompt),
		MaxTokens:      2048,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, fmt.Errorf("genesis-dream LLM call: %w", err)
	}
	if events == nil {
		return nil, fmt.Errorf("genesis-dream LLM: nil event channel")
	}

	var sb strings.Builder
	for ev := range events {
		if ev.Type == "content_block_delta" {
			var delta struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
				sb.WriteString(delta.Delta.Text)
			}
		}
	}

	gen, perr := parseGenesisResponse(sb.String())
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
		Model:          s.judgeModel,
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(genesisJudgeSystemPrompt),
		MaxTokens:      1024,
		Stream:         true,
		Thinking:       s.judgeThinking,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		s.logger.Warn("genesis: judge unavailable, accepting on heuristic", "skill", skill.Name, "error", err)
		return true, ""
	}
	extracted := jsonutil.ExtractObject(drainStreamText(events))
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
// genesis prompt asks for (When to Use / Procedure / Pitfalls / Verification,
// description with a "Use when" trigger) and target the vagueness failure mode.
func skillSpecificityIssues(skill *GeneratedSkill) []string {
	var issues []string
	body := strings.TrimSpace(skill.Body)
	lower := strings.ToLower(body)

	if n := len([]rune(body)); n < 400 {
		issues = append(issues, fmt.Sprintf("본문이 너무 짧음(%d자<400)", n))
	}
	// The two load-bearing sections: a skill must say WHEN it applies and HOW
	// to run it. Pitfalls/Verification are recommended but not hard-required to
	// avoid false rejects of otherwise-actionable skills.
	if !strings.Contains(lower, "when to use") {
		issues = append(issues, "When to Use 섹션 누락(트리거 불명)")
	}
	if !strings.Contains(lower, "procedure") {
		issues = append(issues, "Procedure 섹션 누락(절차 없음)")
	}
	if !hasActionableStep(body) {
		issues = append(issues, "구체적 단계 부재(번호 절차/도구 호출/명령어 없음)")
	}
	desc := strings.ToLower(skill.Description)
	if !strings.Contains(desc, "use when") && !strings.Contains(skill.Description, "트리거") && !strings.Contains(desc, "when:") {
		issues = append(issues, "description에 트리거(Use when) 없음")
	}
	return issues
}

// hasActionableStep reports whether a skill body contains a concrete step — a
// numbered list item, or an inline code/command reference — as opposed to pure
// vague prose ("read the context carefully").
func hasActionableStep(body string) bool {
	if strings.Contains(body, "`") { // inline code or command
		return true
	}
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if len(t) >= 2 && t[0] >= '1' && t[0] <= '9' && (t[1] == '.' || t[1] == ')') {
			return true
		}
	}
	return false
}

// Persist writes a generated skill to disk and registers it in the catalog.
func (s *Service) Persist(skill *GeneratedSkill) error {
	if skill == nil || skill.Name == "" {
		return fmt.Errorf("genesis: nil or unnamed skill")
	}

	// Validate name: lowercase, hyphens only.
	name := sanitizeSkillName(skill.Name)
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
			return nil, fmt.Errorf("genesis: parse response: %w (raw: %s)", err, truncateRunes(text, 200))
		}
		if skill.Name == "" {
			return nil, nil
		}
		return &skill, nil
	}
	if resp.Skip || resp.Skill == nil {
		return nil, nil
	}
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

// sanitizeSkillName normalizes a skill name to lowercase with hyphens.
func sanitizeSkillName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	// Remove non-alphanumeric chars except hyphens.
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			sb.WriteRune(r)
		}
	}
	result := sb.String()
	// Trim leading/trailing hyphens and collapse consecutive hyphens.
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	if len(result) < 2 {
		return ""
	}
	return result
}

func truncateRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// isDuplicateSkill reports whether a skill with this name+description is a
// near-duplicate of an existing catalog entry. An exact name collision is
// always a duplicate; otherwise it compares name+description token sets with
// Jaccard similarity. Owner-agnostic — Deneb is single-user.
func (s *Service) isDuplicateSkill(name, description string) bool {
	if s.catalog == nil {
		return false
	}
	cand := skillDedupTokens(name, description)
	if len(cand) == 0 {
		return false
	}
	for _, e := range s.catalog.List() {
		if e.Skill.Name == name {
			return true
		}
		if jaccardSimilarity(cand, skillDedupTokens(e.Skill.Name, e.Skill.Description)) >= skillDedupThreshold {
			return true
		}
	}
	return false
}

// skillDedupTokens builds a lowercase token set from a skill's name and
// description for similarity comparison. Tokens shorter than 2 runes are
// dropped as noise. unicode.IsLetter covers CJK, so Korean words tokenize on
// whitespace/punctuation.
func skillDedupTokens(name, description string) map[string]struct{} {
	set := make(map[string]struct{})
	addDedupTokens(set, name)
	addDedupTokens(set, description)
	return set
}

func addDedupTokens(set map[string]struct{}, text string) {
	for _, tok := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(tok)) >= 2 {
			set[tok] = struct{}{}
		}
	}
}

// jaccardSimilarity returns |A∩B| / |A∪B| for two token sets (0 when either
// is empty).
func jaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for tok := range a {
		if _, ok := b[tok]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
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

// errString returns "" for nil so liveness callers can pass an error directly.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
