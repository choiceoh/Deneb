package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

const skillLifecycleTimeout = 90 * time.Second
const skillLifecycleMaxStatusLogEntries = 50
const skillLifecycleMaxProposalResultBytes = 4096

type skillLifecycleBackend struct {
	genesis     *genesis.Service
	evolver     *genesis.Evolver
	tracker     *genesis.Tracker
	transcripts toolctx.TranscriptStore
	logger      *slog.Logger
}

func (b *skillLifecycleBackend) ProposeSkillEvolution(ctx context.Context, req chattools.SkillEvolutionProposalRequest) (any, error) {
	if req.SessionKey == "" {
		req.SessionKey = toolctx.SessionKeyFromContext(ctx)
	}
	route := normalizeSkillLifecycleRoute(req.Route)
	if route == "" {
		return nil, fmt.Errorf("route must be one of no-op, genesis, create, evolve")
	}
	if strings.TrimSpace(req.Candidate) == "" {
		return nil, fmt.Errorf("candidate is required for propose")
	}

	result := map[string]any{
		"ok":        true,
		"candidate": req.Candidate,
		"route":     route,
		"executed":  false,
	}
	if req.Reason != "" {
		result["reason"] = req.Reason
	}

	var execResult any
	var execErr error
	if req.Execute {
		switch route {
		case "genesis":
			execResult, execErr = b.RunSkillGenesis(ctx, chattools.SkillGenesisRequest{
				SessionKey:   req.SessionKey,
				DreamSummary: req.DreamSummary,
			})
		case "evolve":
			execResult, execErr = b.RunSkillEvolution(ctx, chattools.SkillEvolutionRequest{
				SkillName: req.SkillName,
			})
		case "create":
			result["nextAction"] = "load skill-factory, then use skills action=create"
		case "no-op":
			// Nothing to execute; the proposal record is the result.
		}
		if execErr != nil {
			result["ok"] = false
			result["error"] = execErr.Error()
		} else if execResult != nil {
			result["executed"] = true
			result["result"] = execResult
		}
	}

	b.logProposal(req, route, result)
	return result, nil
}

func (b *skillLifecycleBackend) RunSkillGenesis(ctx context.Context, req chattools.SkillGenesisRequest) (any, error) {
	if b.genesis == nil {
		return nil, fmt.Errorf("skill genesis is not configured")
	}
	if req.SessionKey == "" {
		req.SessionKey = toolctx.SessionKeyFromContext(ctx)
	}
	if strings.TrimSpace(req.SessionKey) == "" && strings.TrimSpace(req.DreamSummary) == "" {
		return nil, fmt.Errorf("sessionKey or dreamSummary is required")
	}

	ctx, cancel := context.WithTimeout(ctx, skillLifecycleTimeout)
	defer cancel()

	source := "session"
	var sessionKey string
	var skill *genesis.GeneratedSkill
	var err error
	if strings.TrimSpace(req.DreamSummary) != "" {
		source = "dream"
		skill, err = b.genesis.GenerateFromDream(ctx, req.DreamSummary)
	} else {
		sessionKey = req.SessionKey
		sctx, buildErr := buildSkillLifecycleSessionContext(b.transcripts, req.SessionKey)
		if buildErr != nil {
			return nil, fmt.Errorf("load session: %w", buildErr)
		}
		skill, err = b.genesis.Generate(ctx, sctx)
	}
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return map[string]any{
			"ok":     true,
			"skip":   true,
			"reason": "no skill-worthy pattern detected",
			"source": source,
		}, nil
	}
	if err := b.genesis.Persist(skill); err != nil {
		return nil, err
	}
	if b.tracker != nil {
		_ = b.tracker.LogGenesis(skill.Name, source, sessionKey, skill.Category, skill.Description)
	}
	return map[string]any{
		"ok":     true,
		"source": source,
		"skill":  skill,
	}, nil
}

func (b *skillLifecycleBackend) RunSkillEvolution(ctx context.Context, req chattools.SkillEvolutionRequest) (any, error) {
	if b.evolver == nil {
		return nil, fmt.Errorf("skill evolver is not configured")
	}
	if strings.TrimSpace(req.SkillName) == "" {
		return nil, fmt.Errorf("skillName is required")
	}
	ctx, cancel := context.WithTimeout(ctx, skillLifecycleTimeout)
	defer cancel()
	result, err := b.evolver.EvolveSkill(ctx, req.SkillName)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":     true,
		"result": result,
	}, nil
}

func (b *skillLifecycleBackend) SkillLifecycleStatus(_ context.Context, req chattools.SkillLifecycleStatusRequest) (any, error) {
	if b.tracker == nil {
		return map[string]any{
			"ok":     false,
			"reason": "skill tracker is not configured",
		}, nil
	}

	limit := normalizeSkillLifecycleStatusLimit(req.Limit)
	recent, err := b.tracker.RecentLifecycleLog(limit)
	if err != nil {
		return nil, err
	}
	skillName := strings.TrimSpace(req.SkillName)
	if skillName != "" {
		recent = filterSkillLifecycleLog(recent, skillName)
		stats, err := b.tracker.Stats(skillName)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"ok":        true,
			"skillName": skillName,
			"limit":     limit,
			"recent":    recent,
			"stats":     stats,
		}, nil
	}

	stats, err := b.tracker.ListAllStats()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":     true,
		"limit":  limit,
		"recent": recent,
		"stats":  stats,
	}, nil
}

func (b *skillLifecycleBackend) logProposal(req chattools.SkillEvolutionProposalRequest, route string, result map[string]any) {
	if b.tracker == nil {
		return
	}
	resultText := ""
	if data, err := json.Marshal(result); err == nil {
		resultText = truncateSkillLifecycleProposalResult(string(data))
	}
	executed, _ := result["executed"].(bool)
	if err := b.tracker.LogEvolutionProposal(genesis.EvolutionProposalRecord{
		Candidate:  req.Candidate,
		Route:      route,
		SessionKey: req.SessionKey,
		SkillName:  req.SkillName,
		Evidence:   req.Evidence,
		Reason:     req.Reason,
		Executed:   executed,
		Result:     resultText,
	}); err != nil && b.logger != nil {
		b.logger.Warn("skill lifecycle: proposal log failed", "error", err)
	}
}

func normalizeSkillLifecycleStatusLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > skillLifecycleMaxStatusLogEntries {
		return skillLifecycleMaxStatusLogEntries
	}
	return limit
}

func filterSkillLifecycleLog(entries []genesis.LifecycleLogEntry, skillName string) []genesis.LifecycleLogEntry {
	filtered := make([]genesis.LifecycleLogEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.SkillName == skillName {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func truncateSkillLifecycleProposalResult(result string) string {
	if len(result) <= skillLifecycleMaxProposalResultBytes {
		return result
	}
	return result[:skillLifecycleMaxProposalResultBytes] + "...[truncated]"
}

func normalizeSkillLifecycleRoute(route string) string {
	switch strings.ToLower(strings.TrimSpace(route)) {
	case "noop", "no_op", "no-op", "skip", "none":
		return "no-op"
	case "genesis", "create", "evolve":
		return strings.ToLower(strings.TrimSpace(route))
	default:
		return ""
	}
}

func buildSkillLifecycleSessionContext(store toolctx.TranscriptStore, sessionKey string) (genesis.SessionContext, error) {
	sctx := genesis.SessionContext{Key: sessionKey}
	if store == nil {
		return sctx, nil
	}

	msgs, _, err := store.Load(sessionKey, 200)
	if err != nil {
		return sctx, err
	}

	var textParts []string
	toolSet := make(map[string]struct{})
	for _, msg := range msgs {
		if msg.Role == "assistant" {
			sctx.Turns++
		}
		if text := msg.TextContent(); text != "" {
			textParts = append(textParts, msg.Role+": "+text)
		}
		for _, name := range extractSkillLifecycleToolNames(msg.Content) {
			toolSet[name] = struct{}{}
		}
	}
	sctx.AllText = strings.Join(textParts, "\n")
	for name := range toolSet {
		sctx.ToolActivities = append(sctx.ToolActivities, genesis.ToolActivity{Name: name})
	}
	return sctx, nil
}

func extractSkillLifecycleToolNames(content json.RawMessage) []string {
	if len(content) == 0 || content[0] != '[' {
		return nil
	}
	var blocks []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return nil
	}
	var names []string
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name != "" {
			names = append(names, b.Name)
		}
	}
	return names
}
