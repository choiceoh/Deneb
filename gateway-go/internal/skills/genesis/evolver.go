package genesis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// EvolveResult describes the outcome of an evolution attempt.
type EvolveResult struct {
	SkillName   string `json:"skillName"`
	Evolved     bool   `json:"evolved"`
	NewVersion  string `json:"newVersion,omitempty"`
	Description string `json:"description,omitempty"`
	Reason      string `json:"reason,omitempty"` // when skipped
}

// Evolver auto-improves skills based on usage data.
type Evolver struct {
	llmClient *llm.Client
	catalog   *skills.Catalog
	tracker   *Tracker
	model     string
	logger    *slog.Logger
}

// NewEvolver creates a skill evolver.
func NewEvolver(llmClient *llm.Client, catalog *skills.Catalog, tracker *Tracker, model string, logger *slog.Logger) *Evolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &Evolver{
		llmClient: llmClient,
		catalog:   catalog,
		tracker:   tracker,
		model:     model,
		logger:    logger,
	}
}

// EvolveSkill attempts to improve a single skill based on usage feedback.
func (e *Evolver) EvolveSkill(ctx context.Context, skillName string) (*EvolveResult, error) {
	if e.catalog == nil {
		return nil, fmt.Errorf("evolver: catalog not configured")
	}
	// Get current skill content.
	entry, ok := e.catalog.Get(skillName)
	if !ok {
		return nil, fmt.Errorf("evolver: skill %q not found in catalog", skillName)
	}

	currentContent, err := os.ReadFile(entry.Skill.FilePath)
	if err != nil {
		return nil, fmt.Errorf("evolver: read skill file: %w", err)
	}

	// Get usage stats.
	var stats *UsageStats
	if e.tracker != nil {
		stats, _ = e.tracker.GetStats(skillName)
	}
	if stats == nil {
		stats = &UsageStats{SkillName: skillName}
	}

	// Build prompt.
	userPrompt := fmt.Sprintf(`## 현재 SKILL.md
%s

## 사용 통계
- 총 사용: %d회
- 성공: %d회 (%.0f%%)
- 실패: %d회
- 최근 에러: %s`,
		string(currentContent),
		stats.TotalUses, stats.SuccessCount, stats.SuccessRate*100,
		stats.FailureCount,
		formatRecentErrors(stats.RecentErrors))

	events, err := e.llmClient.StreamChat(ctx, llm.ChatRequest{
		Model:          e.resolveModel(),
		Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
		System:         llm.SystemString(evolveSystemPrompt),
		MaxTokens:      2048,
		Stream:         true,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
		ExtraBody: map[string]any{
			"chat_template_kwargs": map[string]any{"enable_thinking": false},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("evolver LLM call: %w", err)
	}
	if events == nil {
		return nil, fmt.Errorf("evolver LLM: nil event channel")
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

	return e.parseAndApply(sb.String(), entry, string(currentContent))
}

// EvolveUnderperformers finds and evolves skills with poor success rates.
// Used as a periodic background task.
func (e *Evolver) EvolveUnderperformers(ctx context.Context) ([]EvolveResult, error) {
	if e.tracker == nil {
		return nil, nil
	}

	candidates, err := e.tracker.SkillsNeedingEvolution(3, 0.7)
	if err != nil {
		return nil, err
	}

	var results []EvolveResult
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			break
		}
		result, err := e.EvolveSkill(ctx, candidate.SkillName)
		if err != nil {
			e.logger.Warn("evolver: failed to evolve",
				"skill", candidate.SkillName, "error", err)
			results = append(results, EvolveResult{
				SkillName: candidate.SkillName,
				Evolved:   false,
				Reason:    err.Error(),
			})
			continue
		}
		if result != nil {
			results = append(results, *result)
		}
	}
	return results, nil
}

func (e *Evolver) parseAndApply(text string, entry *skills.SkillEntry, originalContent string) (*EvolveResult, error) {
	extracted := jsonutil.ExtractObject(text)
	if extracted == "" {
		extracted = strings.TrimSpace(text)
	}

	var resp struct {
		Skip    bool   `json:"skip"`
		Reason  string `json:"reason,omitempty"`
		Changes *struct {
			Description string `json:"description"`
			NewVersion  string `json:"new_version"`
			Body        string `json:"body"`
		} `json:"changes,omitempty"`
	}
	if err := json.Unmarshal([]byte(extracted), &resp); err != nil {
		return nil, fmt.Errorf("evolver: parse response: %w", err)
	}

	if resp.Skip || resp.Changes == nil {
		return &EvolveResult{
			SkillName: entry.Skill.Name,
			Evolved:   false,
			Reason:    resp.Reason,
		}, nil
	}

	// Reconstruct SKILL.md with updated body.
	header, bodyOffset := skills.ExtractFrontmatterBlock(originalContent)
	if bodyOffset == 0 || header == "" {
		return nil, fmt.Errorf("evolver: skill %q has no valid frontmatter", entry.Skill.Name)
	}

	// Update version in frontmatter.
	newVersion := resp.Changes.NewVersion
	if newVersion == "" {
		newVersion = bumpPatchVersion(entry.Skill.Version)
	}

	newHeader := strings.Replace(header, entry.Skill.Version, newVersion, 1)
	newContent := newHeader + "\n" + resp.Changes.Body + "\n"

	// Write back.
	if err := os.WriteFile(entry.Skill.FilePath, []byte(newContent), 0o644); err != nil {
		return nil, fmt.Errorf("evolver: write file: %w", err)
	}

	e.logger.Info("evolver: skill evolved",
		"skill", entry.Skill.Name,
		"version", newVersion,
		"description", resp.Changes.Description,
	)

	return &EvolveResult{
		SkillName:   entry.Skill.Name,
		Evolved:     true,
		NewVersion:  newVersion,
		Description: resp.Changes.Description,
	}, nil
}

func (e *Evolver) resolveModel() string {
	if e.model != "" {
		return e.model
	}
	return "gemini-2.5-flash"
}

// bumpPatchVersion increments the patch segment of a semver string.
func bumpPatchVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "0.1.1"
	}
	var patch int
	fmt.Sscanf(parts[2], "%d", &patch)
	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
}

func formatRecentErrors(errors []string) string {
	if len(errors) == 0 {
		return "(none)"
	}
	var parts []string
	for _, e := range errors {
		if len(e) > 100 {
			e = e[:100] + "..."
		}
		parts = append(parts, "- "+e)
	}
	return strings.Join(parts, "\n")
}

// EvolutionTask implements autonomous.PeriodicTask for background skill evolution.
type EvolutionTask struct {
	Evolver *Evolver
	Logger  *slog.Logger
}

// Name returns the task identifier.
func (t *EvolutionTask) Name() string { return "skill-evolution" }

// Interval returns how often to check for underperforming skills.
func (t *EvolutionTask) Interval() time.Duration { return 6 * time.Hour }

// Run executes one evolution cycle.
func (t *EvolutionTask) Run(ctx context.Context) error {
	results, err := t.Evolver.EvolveUnderperformers(ctx)
	if err != nil {
		return err
	}
	evolved := 0
	for _, r := range results {
		if r.Evolved {
			evolved++
		}
	}
	if evolved > 0 {
		t.Logger.Info("skill-evolution: cycle complete",
			"evolved", evolved, "total", len(results))
	}
	return nil
}
