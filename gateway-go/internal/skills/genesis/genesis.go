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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Config configures the genesis service.
type Config struct {
	// MinToolCalls is the minimum number of tool calls in a session to
	// consider it for skill extraction. Default: 5.
	MinToolCalls int
	// MinTurns is the minimum number of agent turns. Default: 3.
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
	MaxSkillsPerDay int
}

// DefaultConfig returns production defaults.
func DefaultConfig() Config {
	outputDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		outputDir = filepath.Join(home, ".deneb", "skills", "genesis")
	}
	return Config{
		MinToolCalls:     5,
		MinTurns:         3,
		OutputDir:        outputDir,
		CooldownPerSkill: 24 * time.Hour,
		MaxSkillsPerDay:  3,
	}
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
		cfg.MinToolCalls = 5
	}
	if cfg.MinTurns == 0 {
		cfg.MinTurns = 3
	}
	if cfg.MaxSkillsPerDay == 0 {
		cfg.MaxSkillsPerDay = 3
	}
	if cfg.CooldownPerSkill == 0 {
		cfg.CooldownPerSkill = 24 * time.Hour
	}
	return &Service{
		cfg:          cfg,
		llmClient:    llmClient,
		catalog:      catalog,
		logger:       logger,
		recentSkills: make(map[string]time.Time),
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
	}
	if s.dailyCount >= s.cfg.MaxSkillsPerDay {
		s.logger.Debug("genesis: daily cap reached", "count", s.dailyCount)
		return false
	}

	// Require diverse tool usage — at least 2 distinct tools.
	toolSet := make(map[string]bool)
	for _, ta := range sctx.ToolActivities {
		toolSet[ta.Name] = true
	}
	if len(toolSet) < 2 {
		return false
	}

	return true
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
		ExtraBody: map[string]any{
			"chat_template_kwargs": map[string]any{"enable_thinking": false},
		},
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

	return parseGenesisResponse(sb.String())
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
		ExtraBody: map[string]any{
			"chat_template_kwargs": map[string]any{"enable_thinking": false},
		},
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

	return parseGenesisResponse(sb.String())
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
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
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
	sb.WriteString(fmt.Sprintf("name: %s\n", name))
	sb.WriteString("version: \"0.1.0\"\n")
	sb.WriteString(fmt.Sprintf("category: %s\n", skill.Category))
	sb.WriteString(fmt.Sprintf("description: \"%s\"\n", strings.ReplaceAll(skill.Description, `"`, `\"`)))

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
		sb.WriteString(fmt.Sprintf("metadata: %s\n", string(metaJSON)))
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

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

func ptrInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
