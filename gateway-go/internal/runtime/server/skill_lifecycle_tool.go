package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

const (
	skillLifecycleTimeout                       = 90 * time.Second
	skillLifecycleMaxStatusLogEntries           = 50
	skillLifecycleMaxProposalResultBytes        = 4096
	skillLifecycleMaxValidationBackfillSessions = 50
)

type skillLifecycleBackend struct {
	genesis     *genesis.Service
	evolver     *genesis.Evolver
	tracker     *genesis.Tracker
	transcripts toolctx.TranscriptStore
	logger      *slog.Logger

	// Heartbeat shadow-replay deps (P1, heartbeat_shadow_replay.go). Nil/empty
	// on backends that never serve the tool (e.g. the backfill task).
	fixturePath    string
	shadowComplete shadowCompleteFunc
}

// HeartbeatShadowReplay dry-runs a candidate HEARTBEAT.md body over the
// harvested fixture corpus. Report only — nothing is applied (the surface
// registry keeps heartbeat-instructions propose-only).
func (b *skillLifecycleBackend) HeartbeatShadowReplay(ctx context.Context, req chattools.HeartbeatShadowReplayRequest) (any, error) {
	if b.shadowComplete == nil || b.fixturePath == "" {
		return nil, fmt.Errorf("heartbeat shadow replay is not configured on this backend")
	}
	return runHeartbeatShadowReplay(ctx, b.fixturePath, req.Candidate, req.Limit, b.shadowComplete)
}

// ProposeSkillEvolution creates a skill-evolution proposal through the lifecycle backend.
func (b *skillLifecycleBackend) ProposeSkillEvolution(ctx context.Context, req chattools.SkillEvolutionProposalRequest) (any, error) {
	if req.SessionKey == "" {
		req.SessionKey = toolctx.SessionKeyFromContext(ctx)
	}
	route := normalizeSkillLifecycleRoute(req.Route)
	if route == "" {
		return nil, fmt.Errorf("route must be one of no-op, genesis, create, evolve")
	}
	// A no-op proposal records "no skill-worthy pattern, nothing to do" — there
	// is no reusable candidate by definition. Only executable routes (genesis/
	// create/evolve) require one. Forcing candidate on no-op made the reviewer
	// agent fail repeatedly with "candidate is required for propose".
	if route != "no-op" && strings.TrimSpace(req.Candidate) == "" {
		return nil, fmt.Errorf("candidate is required for propose with route=%q", route)
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

	// Persist the reviewer's decision (route + candidate + evidence) BEFORE the
	// slow, cancellable execution below. The nudge review fork rides the server
	// shutdown context, so an auto-deploy hot-swap that fires mid-review cancels
	// the fork — and when logging came only AFTER execution, an interrupted
	// held-out validation erased the proposal entirely (verified on prod
	// 2026-07-09: the lifecycle log frozen for days while reviews kept firing).
	// Logging up-front makes the proposal durable the instant it is made; the
	// applied OUTCOME (genesis/evolved/evolve_rejected) is recorded separately by
	// the exec path below, so this stays a single proposal record — no double
	// count. executed=false here reflects "not yet executed at log time".
	b.recordReviewUsage(req, route)
	b.logProposal(req, route, result)

	if req.Execute {
		var execResult any
		var execErr error
		switch route {
		case "genesis":
			execResult, execErr = b.RunSkillGenesis(ctx, chattools.SkillGenesisRequest{
				SessionKey:   req.SessionKey,
				DreamSummary: req.DreamSummary,
			})
		case "evolve":
			// Pass the review's reasoning + evidence as the improvement finding
			// so the evolver can act on the LLM's verdict without usage stats.
			finding := strings.TrimSpace(req.Reason + "\n" + req.Evidence)
			execResult, execErr = b.RunSkillEvolution(ctx, chattools.SkillEvolutionRequest{
				SkillName: req.SkillName,
				Finding:   finding,
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

	return result, nil
}

// RunSkillGenesis runs a requested skill-genesis operation.
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
		if errors.Is(err, genesis.ErrSkillDeduped) {
			return map[string]any{
				"ok":     true,
				"skip":   true,
				"reason": "existing skill already covers this (deduplicated)",
				"source": source,
			}, nil
		}
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

// RunSkillEvolution runs a requested skill-evolution operation.
func (b *skillLifecycleBackend) RunSkillEvolution(ctx context.Context, req chattools.SkillEvolutionRequest) (any, error) {
	if b.evolver == nil {
		return nil, fmt.Errorf("skill evolver is not configured")
	}
	if strings.TrimSpace(req.SkillName) == "" {
		return nil, fmt.Errorf("skillName is required")
	}
	ctx, cancel := context.WithTimeout(ctx, skillLifecycleTimeout)
	defer cancel()
	result, err := b.evolver.EvolveSkill(ctx, req.SkillName, req.Finding)
	if err != nil {
		return nil, err
	}
	if b.tracker != nil && result != nil && result.Evolved {
		_ = b.tracker.MarkSkillPatched(req.SkillName)
	}
	return map[string]any{
		"ok":     true,
		"result": result,
	}, nil
}

// RunSkillCuratorAction runs a requested curator action.
func (b *skillLifecycleBackend) RunSkillCuratorAction(_ context.Context, req chattools.SkillCuratorActionRequest) (any, error) {
	if b.tracker == nil {
		return map[string]any{
			"ok":     false,
			"reason": "skill tracker is not configured",
		}, nil
	}
	skillName := strings.TrimSpace(req.SkillName)
	if skillName == "" {
		return nil, fmt.Errorf("skillName is required")
	}
	switch req.Action {
	case "pin":
		if err := b.tracker.SetSkillPinned(skillName, true); err != nil {
			return nil, err
		}
	case "unpin":
		if err := b.tracker.SetSkillPinned(skillName, false); err != nil {
			return nil, err
		}
	case "archive":
		rec, err := b.tracker.SetSkillCuratorState(skillName, genesis.SkillCuratorStateArchived)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "action": req.Action, "skillName": skillName, "curator": rec}, nil
	case "restore":
		rec, err := b.tracker.SetSkillCuratorState(skillName, genesis.SkillCuratorStateActive)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "action": req.Action, "skillName": skillName, "curator": rec}, nil
	default:
		return nil, fmt.Errorf("unsupported curator action %q", req.Action)
	}
	curator, err := b.tracker.SkillCuratorReport(skillName)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":        true,
		"action":    req.Action,
		"skillName": skillName,
		"curator":   curator,
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
	if err := b.tracker.RecordSkillOpportunity(genesis.SkillOpportunityRecord{
		Candidate:  req.Candidate,
		Route:      route,
		SessionKey: req.SessionKey,
		SkillName:  req.SkillName,
		Evidence:   req.Evidence,
		Reason:     req.Reason,
		Executed:   executed,
		Source:     "skill_lifecycle",
	}); err != nil && b.logger != nil {
		b.logger.Warn("skill lifecycle: opportunity log failed", "error", err)
	}
}

// recordReviewUsage captures skill usage from a review verdict: a no-op means
// the skill worked as-is (success), an evolve means it needs improvement
// (failure). It complements the chat-loop consult recorder (#2151, real reads
// attributed by turn outcome) with a quality judgment signal; both feed the
// evolver's usage stats, EvolveUnderperformers candidate selection, and the
// curator's staleness signal.
func (b *skillLifecycleBackend) recordReviewUsage(req chattools.SkillEvolutionProposalRequest, route string) {
	if b.tracker == nil {
		return
	}
	name := strings.TrimSpace(req.SkillName)
	if name == "" {
		return
	}
	// Tagged review-verdict so it stays out of the evolver's real-use success
	// rate (it drives the curator's staleness/lastUsed signal, but a judgment is
	// not a real execution — conflating them is what thrashed email-analysis).
	switch route {
	case "no-op":
		_ = b.tracker.RecordUsage(genesis.UsageRecord{SkillName: name, SessionKey: req.SessionKey, Success: true, Source: genesis.UsageSourceReviewVerdict})
	case "evolve":
		_ = b.tracker.RecordUsage(genesis.UsageRecord{SkillName: name, SessionKey: req.SessionKey, Success: false, ErrorMsg: strings.TrimSpace(req.Reason), Source: genesis.UsageSourceReviewVerdict})
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

func normalizeSkillValidationBackfillLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > skillLifecycleMaxValidationBackfillSessions {
		return skillLifecycleMaxValidationBackfillSessions
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

func appendUniqueStrings(base []string, values ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(values))
	out := make([]string, 0, len(base)+len(values))
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	for _, value := range base {
		add(value)
	}
	for _, value := range values {
		add(value)
	}
	return out
}
