package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

const skillLifecycleTimeout = 90 * time.Second
const skillLifecycleMaxStatusLogEntries = 50
const skillLifecycleMaxProposalResultBytes = 4096
const skillLifecycleMaxValidationBackfillSessions = 50

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

	b.recordReviewUsage(req, route)
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
		curator, err := b.tracker.SkillCuratorReport(skillName)
		if err != nil {
			return nil, err
		}
		rejected, rejectedErr := b.recentRejectedSkillEdits(skillName, limit)
		usageQuality, usageQualityErr := b.usageQualitySummary(skillName)
		optimizerMemory, optimizerMemoryErr := b.optimizerMemory(skillName)
		validationCases, validationCasesErr := b.recentSkillValidationCases(skillName, limit)
		validationSummary, validationSummaryErr := b.validationCaseSummary(skillName)
		opportunities, opportunitiesErr := b.recentSkillOpportunities(skillName, limit)
		selfCorrections, selfCorrectionsErr := b.recentSelfCorrectionCandidates(skillName, limit)
		status := map[string]any{
			"ok":                       true,
			"skillName":                skillName,
			"limit":                    limit,
			"recent":                   recent,
			"stats":                    stats,
			"curator":                  curator,
			"rejectedEdits":            rejected,
			"usageQuality":             usageQuality,
			"optimizerMemory":          optimizerMemory,
			"validationCases":          validationCases,
			"validationCaseSummary":    validationSummary,
			"opportunities":            opportunities,
			"selfCorrectionCandidates": selfCorrections,
		}
		if rejectedErr != "" {
			status["rejectedEditsError"] = rejectedErr
		}
		if usageQualityErr != "" {
			status["usageQualityError"] = usageQualityErr
		}
		if optimizerMemoryErr != "" {
			status["optimizerMemoryError"] = optimizerMemoryErr
		}
		if validationCasesErr != "" {
			status["validationCasesError"] = validationCasesErr
		}
		if validationSummaryErr != "" {
			status["validationCaseSummaryError"] = validationSummaryErr
		}
		if opportunitiesErr != "" {
			status["opportunitiesError"] = opportunitiesErr
		}
		if selfCorrectionsErr != "" {
			status["selfCorrectionCandidatesError"] = selfCorrectionsErr
		}
		return status, nil
	}

	stats, err := b.tracker.ListAllStats()
	if err != nil {
		return nil, err
	}
	curator, err := b.tracker.SkillCuratorReport("")
	if err != nil {
		return nil, err
	}
	rejected, rejectedErr := b.recentRejectedSkillEdits("", limit)
	usageQuality, usageQualityErr := b.usageQualitySummary("")
	validationCases, validationCasesErr := b.recentSkillValidationCases("", limit)
	validationSummary, validationSummaryErr := b.validationCaseSummary("")
	opportunities, opportunitiesErr := b.recentSkillOpportunities("", limit)
	selfCorrections, selfCorrectionsErr := b.recentSelfCorrectionCandidates("", limit)
	status := map[string]any{
		"ok":                       true,
		"limit":                    limit,
		"recent":                   recent,
		"stats":                    stats,
		"curator":                  curator,
		"rejectedEdits":            rejected,
		"usageQuality":             usageQuality,
		"validationCases":          validationCases,
		"validationCaseSummary":    validationSummary,
		"opportunities":            opportunities,
		"selfCorrectionCandidates": selfCorrections,
	}
	if rejectedErr != "" {
		status["rejectedEditsError"] = rejectedErr
	}
	if usageQualityErr != "" {
		status["usageQualityError"] = usageQualityErr
	}
	if validationCasesErr != "" {
		status["validationCasesError"] = validationCasesErr
	}
	if validationSummaryErr != "" {
		status["validationCaseSummaryError"] = validationSummaryErr
	}
	if opportunitiesErr != "" {
		status["opportunitiesError"] = opportunitiesErr
	}
	if selfCorrectionsErr != "" {
		status["selfCorrectionCandidatesError"] = selfCorrectionsErr
	}
	return status, nil
}

func (b *skillLifecycleBackend) recentRejectedSkillEdits(skillName string, limit int) ([]genesis.RejectedSkillEditRecord, string) {
	rejected, err := b.tracker.RecentRejectedSkillEdits(skillName, limit)
	if err == nil {
		return rejected, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: rejected edits unavailable",
			"skill", skillName, "error", err)
	}
	return []genesis.RejectedSkillEditRecord{}, err.Error()
}

func (b *skillLifecycleBackend) usageQualitySummary(skillName string) (genesis.UsageQualitySummary, string) {
	quality, err := b.tracker.UsageQualitySummary(skillName)
	if err == nil {
		return quality, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: usage quality unavailable",
			"skill", skillName, "error", err)
	}
	return quality, err.Error()
}

func (b *skillLifecycleBackend) optimizerMemory(skillName string) (genesis.SkillOptimizerMemoryEntry, string) {
	memory, err := b.tracker.OptimizerMemory(skillName)
	if err == nil {
		return memory, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: optimizer memory unavailable",
			"skill", skillName, "error", err)
	}
	return memory, err.Error()
}

func (b *skillLifecycleBackend) recentSkillValidationCases(skillName string, limit int) ([]genesis.SkillValidationCaseRecord, string) {
	cases, err := b.tracker.RecentSkillValidationCases(skillName, limit)
	if err == nil {
		return cases, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: validation cases unavailable",
			"skill", skillName, "error", err)
	}
	return []genesis.SkillValidationCaseRecord{}, err.Error()
}

func (b *skillLifecycleBackend) validationCaseSummary(skillName string) (genesis.SkillValidationCaseSummary, string) {
	summary, err := b.tracker.ValidationCaseSummary(skillName)
	if err == nil {
		return summary, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: validation case summary unavailable",
			"skill", skillName, "error", err)
	}
	return genesis.SkillValidationCaseSummary{}, err.Error()
}

func (b *skillLifecycleBackend) recentSkillOpportunities(skillName string, limit int) ([]genesis.SkillOpportunityRecord, string) {
	opportunities, err := b.tracker.RecentSkillOpportunities(skillName, limit)
	if err == nil {
		return opportunities, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: opportunities unavailable",
			"skill", skillName, "error", err)
	}
	return []genesis.SkillOpportunityRecord{}, err.Error()
}

func (b *skillLifecycleBackend) recentSelfCorrectionCandidates(skillName string, limit int) ([]genesis.SelfCorrectionCandidateRecord, string) {
	candidates, err := b.tracker.RecentSelfCorrectionCandidates(skillName, genesis.SelfCorrectionStatusProposed, limit)
	if err == nil {
		return candidates, ""
	}
	if b.logger != nil {
		b.logger.Warn("skill lifecycle: self-correction candidates unavailable",
			"skill", skillName, "error", err)
	}
	return []genesis.SelfCorrectionCandidateRecord{}, err.Error()
}

func (b *skillLifecycleBackend) RecordSelfCorrectionCandidate(ctx context.Context, req chattools.SkillSelfCorrectionCandidateRequest) (any, error) {
	if b.tracker == nil {
		return map[string]any{
			"ok":     false,
			"reason": "skill tracker is not configured",
		}, nil
	}
	if req.SessionKey == "" {
		req.SessionKey = toolctx.SessionKeyFromContext(ctx)
	}
	rec, err := b.tracker.RecordSelfCorrectionCandidate(genesis.SelfCorrectionCandidateRecord{
		ID:             req.ID,
		Scope:          req.Scope,
		SkillName:      req.SkillName,
		SessionKey:     req.SessionKey,
		Title:          req.Title,
		Candidate:      req.Candidate,
		Evidence:       req.Evidence,
		Reason:         req.Reason,
		TargetFiles:    req.TargetFiles,
		ProposedChange: req.ProposedChange,
		Risk:           req.Risk,
		Source:         req.Source,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":        true,
		"candidate": rec,
	}, nil
}

func (b *skillLifecycleBackend) ReviewSelfCorrectionCandidate(_ context.Context, req chattools.SkillSelfCorrectionReviewRequest) (any, error) {
	if b.tracker == nil {
		return map[string]any{
			"ok":     false,
			"reason": "skill tracker is not configured",
		}, nil
	}
	rec, err := b.tracker.RecordSelfCorrectionReview(genesis.SelfCorrectionCandidateRecord{
		ID:         req.ID,
		Status:     req.Status,
		Reviewer:   req.Reviewer,
		ReviewNote: req.ReviewNote,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":     true,
		"review": rec,
	}, nil
}

func (b *skillLifecycleBackend) RecordSkillValidationCase(_ context.Context, req chattools.SkillValidationCaseRequest) (any, error) {
	if b.tracker == nil {
		return map[string]any{
			"ok":     false,
			"reason": "skill tracker is not configured",
		}, nil
	}
	record := genesis.SkillValidationCaseRecord{
		SkillName:           req.SkillName,
		ID:                  req.ID,
		Description:         req.Description,
		RequiredSubstrings:  req.RequiredSubstrings,
		ForbiddenSubstrings: req.ForbiddenSubstrings,
		RequiredHeadings:    req.RequiredHeadings,
		Replay: genesis.SkillReplayCaseRecord{
			Input:                 req.Replay.Input,
			Context:               req.Replay.Context,
			RequiredActions:       req.Replay.RequiredActions,
			ForbiddenActions:      req.Replay.ForbiddenActions,
			RequiredObservations:  req.Replay.RequiredObservations,
			ForbiddenObservations: req.Replay.ForbiddenObservations,
			RequiredTools:         req.Replay.RequiredTools,
			ForbiddenTools:        req.Replay.ForbiddenTools,
			ExpectedToolCalls:     skillReplayToolCallsFromRequest(req.Replay.ExpectedToolCalls),
			ForbiddenToolCalls:    skillReplayToolCallsFromRequest(req.Replay.ForbiddenToolCalls),
			RequireOrder:          req.Replay.RequireOrder,
		},
		Source: req.Source,
	}
	if err := b.tracker.RecordSkillValidationCase(record); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":        true,
		"skillName": strings.TrimSpace(req.SkillName),
		"id":        strings.TrimSpace(req.ID),
	}, nil
}

func (b *skillLifecycleBackend) RecordSkillValidationCaseFromSession(ctx context.Context, req chattools.SkillValidationCaseFromSessionRequest) (any, error) {
	if b.tracker == nil {
		return map[string]any{
			"ok":     false,
			"reason": "skill tracker is not configured",
		}, nil
	}
	if req.SessionKey == "" {
		req.SessionKey = toolctx.SessionKeyFromContext(ctx)
	}
	if strings.TrimSpace(req.SessionKey) == "" {
		return nil, fmt.Errorf("sessionKey is required")
	}
	sctx, err := buildSkillLifecycleSessionContext(b.transcripts, req.SessionKey)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	record := buildSkillValidationCaseFromSession(req, sctx)
	if err := b.tracker.RecordSkillValidationCase(record); err != nil {
		if errors.Is(err, genesis.ErrWeakAutomaticValidationCase) {
			return map[string]any{
				"ok":         true,
				"skip":       true,
				"reason":     err.Error(),
				"skillName":  strings.TrimSpace(req.SkillName),
				"sessionKey": strings.TrimSpace(req.SessionKey),
			}, nil
		}
		return nil, err
	}
	return map[string]any{
		"ok":                 true,
		"skillName":          strings.TrimSpace(req.SkillName),
		"id":                 record.ID,
		"sessionKey":         strings.TrimSpace(req.SessionKey),
		"expectedToolCalls":  len(record.Replay.ExpectedToolCalls),
		"forbiddenToolCalls": len(record.Replay.ForbiddenToolCalls),
		"requiredTools":      len(record.Replay.RequiredTools),
	}, nil
}

func (b *skillLifecycleBackend) BackfillSkillValidationCases(ctx context.Context, req chattools.SkillValidationBackfillRequest) (any, error) {
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

	limit := normalizeSkillValidationBackfillLimit(req.Limit)
	sessionKey := strings.TrimSpace(req.SessionKey)
	if sessionKey != "" {
		return b.backfillSkillValidationCasesFromKeys(ctx, req, []string{sessionKey}, 1)
	}
	if b.transcripts == nil {
		return nil, fmt.Errorf("transcript store is not configured")
	}
	keys, err := b.transcripts.ListKeys()
	if err != nil {
		return nil, fmt.Errorf("list transcripts: %w", err)
	}
	sort.Strings(keys)
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	return b.backfillSkillValidationCasesFromKeys(ctx, req, keys, limit)
}

func (b *skillLifecycleBackend) backfillSkillValidationCasesFromKeys(ctx context.Context, req chattools.SkillValidationBackfillRequest, keys []string, limit int) (map[string]any, error) {
	result := map[string]any{
		"ok":        true,
		"skillName": strings.TrimSpace(req.SkillName),
		"limit":     limit,
		"scanned":   0,
		"recorded":  0,
		"skipped":   0,
		"errors":    []string{},
		"details":   []map[string]any{},
	}
	var (
		scanned  int
		recorded int
		skipped  int
		errs     []string
		details  []map[string]any
	)
	for _, key := range keys {
		if scanned >= limit {
			break
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		scanned++
		gotAny, err := b.RecordSkillValidationCaseFromSession(ctx, skillValidationBackfillCaseRequest(req, key))
		if err != nil {
			errText := key + ": " + err.Error()
			errs = append(errs, errText)
			if len(details) < 20 {
				details = append(details, map[string]any{
					"sessionKey": key,
					"ok":         false,
					"error":      err.Error(),
				})
			}
			continue
		}
		got, _ := gotAny.(map[string]any)
		if skip, _ := got["skip"].(bool); skip {
			skipped++
			if len(details) < 20 {
				details = append(details, map[string]any{
					"sessionKey": key,
					"ok":         true,
					"skip":       true,
					"reason":     got["reason"],
				})
			}
			continue
		}
		recorded++
		if len(details) < 20 {
			details = append(details, map[string]any{
				"sessionKey":         key,
				"ok":                 true,
				"id":                 got["id"],
				"expectedToolCalls":  got["expectedToolCalls"],
				"forbiddenToolCalls": got["forbiddenToolCalls"],
				"requiredTools":      got["requiredTools"],
			})
		}
	}
	result["scanned"] = scanned
	result["recorded"] = recorded
	result["skipped"] = skipped
	result["errors"] = errs
	result["details"] = details
	if summary, summaryErr := b.validationCaseSummary(strings.TrimSpace(req.SkillName)); summaryErr == "" {
		result["validationCaseSummary"] = summary
	} else {
		result["validationCaseSummaryError"] = summaryErr
	}
	return result, nil
}

func skillValidationBackfillCaseRequest(req chattools.SkillValidationBackfillRequest, sessionKey string) chattools.SkillValidationCaseFromSessionRequest {
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "session-backfill"
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = "Backfilled replay trace from session " + strings.TrimSpace(sessionKey)
	}
	return chattools.SkillValidationCaseFromSessionRequest{
		SkillName:   req.SkillName,
		SessionKey:  sessionKey,
		Description: description,
		Replay:      req.Replay,
		Source:      source,
	}
}

func skillReplayToolCallsFromRequest(calls []chattools.SkillReplayToolCallRequest) []genesis.SkillReplayToolCallRecord {
	out := make([]genesis.SkillReplayToolCallRecord, 0, len(calls))
	for _, call := range calls {
		out = append(out, genesis.SkillReplayToolCallRecord{
			Name:          call.Name,
			InputIncludes: call.InputIncludes,
			InputExcludes: call.InputExcludes,
			FixtureOutput: call.FixtureOutput,
			FixtureError:  call.FixtureError,
		})
	}
	return out
}

func buildSkillValidationCaseFromSession(req chattools.SkillValidationCaseFromSessionRequest, sctx genesis.SessionContext) genesis.SkillValidationCaseRecord {
	replay := genesis.SkillReplayCaseRecord{
		Input:                 firstNonBlank(req.Replay.Input, skillReplayInputFromTranscript(sctx.AllText)),
		Context:               append([]string(nil), req.Replay.Context...),
		RequiredActions:       append([]string(nil), req.Replay.RequiredActions...),
		ForbiddenActions:      append([]string(nil), req.Replay.ForbiddenActions...),
		RequiredObservations:  append([]string(nil), req.Replay.RequiredObservations...),
		ForbiddenObservations: append([]string(nil), req.Replay.ForbiddenObservations...),
		RequiredTools:         append([]string(nil), req.Replay.RequiredTools...),
		ForbiddenTools:        append([]string(nil), req.Replay.ForbiddenTools...),
		ExpectedToolCalls:     skillReplayToolCallsFromRequest(req.Replay.ExpectedToolCalls),
		ForbiddenToolCalls:    skillReplayToolCallsFromRequest(req.Replay.ForbiddenToolCalls),
		RequireOrder:          req.Replay.RequireOrder,
	}
	replay.Context = append(replay.Context, skillReplayContextFromTranscript(sctx.AllText)...)

	autoExpectedCalls, autoForbiddenCalls := skillReplayToolCallsFromActivities(sctx.ToolActivities)
	if len(autoExpectedCalls) > 0 {
		replay.ExpectedToolCalls = append(autoExpectedCalls, replay.ExpectedToolCalls...)
		replay.RequiredTools = appendUniqueStrings(replay.RequiredTools, skillReplayToolNames(autoExpectedCalls)...)
		if len(autoExpectedCalls) > 1 {
			replay.RequireOrder = true
		}
	}
	replay.ForbiddenToolCalls = append(autoForbiddenCalls, replay.ForbiddenToolCalls...)

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "review-session"
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = "Replay trace extracted from session " + strings.TrimSpace(req.SessionKey)
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = skillValidationCaseIDFromSession(req.SessionKey)
	}
	return genesis.SkillValidationCaseRecord{
		SkillName:           req.SkillName,
		ID:                  id,
		Description:         description,
		RequiredSubstrings:  req.RequiredSubstrings,
		ForbiddenSubstrings: req.ForbiddenSubstrings,
		RequiredHeadings:    req.RequiredHeadings,
		Replay:              replay,
		Source:              source,
	}
}

func skillReplayToolCallsFromActivities(activities []genesis.ToolActivity) ([]genesis.SkillReplayToolCallRecord, []genesis.SkillReplayToolCallRecord) {
	const maxExtractedReplayToolCalls = 12
	expected := make([]genesis.SkillReplayToolCallRecord, 0, min(len(activities), maxExtractedReplayToolCalls))
	forbidden := make([]genesis.SkillReplayToolCallRecord, 0, min(len(activities), maxExtractedReplayToolCalls))
	for _, activity := range activities {
		name := strings.TrimSpace(activity.Name)
		if name == "" {
			continue
		}
		call := genesis.SkillReplayToolCallRecord{
			Name:          name,
			InputIncludes: skillReplayInputIncludes(activity.Input),
			FixtureOutput: truncateRunes(strings.TrimSpace(activity.Output), 1000),
			FixtureError:  activity.IsError,
		}
		if activity.IsError {
			if len(call.InputIncludes)+len(call.InputExcludes) > 0 {
				forbidden = append(forbidden, genesis.SkillReplayToolCallRecord{
					Name:          call.Name,
					InputIncludes: append([]string(nil), call.InputIncludes...),
					InputExcludes: append([]string(nil), call.InputExcludes...),
				})
			}
		} else {
			expected = append(expected, call)
		}
		if len(expected)+len(forbidden) >= maxExtractedReplayToolCalls {
			break
		}
	}
	return expected, forbidden
}

func skillReplayToolNames(calls []genesis.SkillReplayToolCallRecord) []string {
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		if name := strings.TrimSpace(call.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

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

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

func skillReplayInputFromTranscript(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "user: ") {
			return truncateRunes(strings.TrimSpace(strings.TrimPrefix(line, "user: ")), 500)
		}
	}
	return truncateRunes(strings.TrimSpace(text), 500)
}

func skillReplayContextFromTranscript(text string) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, 4)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "user: ") || strings.HasPrefix(line, "assistant: ") {
			out = append(out, truncateRunes(line, 300))
		}
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func skillValidationCaseIDFromSession(sessionKey string) string {
	var b strings.Builder
	b.WriteString("session-")
	for _, r := range strings.ToLower(strings.TrimSpace(sessionKey)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ':':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
		if b.Len() >= 100 {
			break
		}
	}
	if b.String() == "session-" {
		return "session-replay"
	}
	return b.String()
}

func skillReplayInputIncludes(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	var decoded any
	var out []string
	if err := json.Unmarshal([]byte(input), &decoded); err == nil {
		collectReplayInputFragments(decoded, "", &out)
	}
	if len(out) == 0 && len([]rune(input)) <= 160 && !trivialReplayInput(input) {
		out = append(out, input)
	}
	return appendUniqueStrings(nil, out...)
}

func trivialReplayInput(input string) bool {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "", "{}", "[]", "null":
		return true
	default:
		return false
	}
}

func collectReplayInputFragments(value any, key string, out *[]string) {
	if len(*out) >= 3 || replaySecretKey(key) {
		return
	}
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := v[k]
			collectReplayInputFragments(child, k, out)
			if len(*out) >= 3 {
				return
			}
		}
	case []any:
		for _, child := range v {
			collectReplayInputFragments(child, key, out)
			if len(*out) >= 3 {
				return
			}
		}
	case string:
		if replayInterestingKey(key) {
			appendReplayInputFragment(out, key, v)
		}
	case float64, bool:
		if replayInterestingKey(key) {
			appendReplayInputFragment(out, key, fmt.Sprint(v))
		}
	}
}

func replayInterestingKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "action", "cmd", "command", "path", "query", "q", "url", "sessionkey", "skillname", "ref_id", "id", "filename":
		return true
	default:
		return false
	}
}

func replaySecretKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"token", "secret", "password", "apikey", "api_key", "authorization", "cookie", "initdata"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func appendReplayInputFragment(out *[]string, key, value string) {
	for _, fragment := range replayInputFragmentsForKey(key, value) {
		fragment = strings.TrimSpace(fragment)
		if fragment == "" || looksOpaqueReplayFragment(fragment) {
			continue
		}
		*out = append(*out, truncateRunes(fragment, 180))
		if len(*out) >= 3 {
			return
		}
	}
}

func replayInputFragmentsForKey(key, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "cmd", "command":
		return replayCommandIntentFragments(value)
	default:
		return []string{value}
	}
}

func replayCommandIntentFragments(command string) []string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil
	}
	var out []string
	appendUnique := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range out {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		out = append(out, value)
	}
	for i := 0; i < len(fields); i++ {
		token := cleanReplayCommandToken(fields[i])
		switch token {
		case "ssh":
			appendUnique(replaySSHIntent("ssh", fields, i+1))
		case "tailscale":
			if i+1 < len(fields) && cleanReplayCommandToken(fields[i+1]) == "ssh" {
				appendUnique(replaySSHIntent("tailscale ssh", fields, i+2))
			}
		case "systemctl":
			appendUnique(systemctlReplayIntent(fields[i:]))
		}
		if len(out) >= 3 {
			return out
		}
	}
	if len(out) == 0 {
		appendUnique(genericCommandIntent(fields))
	}
	return out
}

func replaySSHIntent(prefix string, fields []string, start int) string {
	for i := start; i < len(fields); i++ {
		token := cleanReplayCommandToken(fields[i])
		if token == "" {
			continue
		}
		if strings.HasPrefix(token, "-") {
			if !strings.Contains(token, "=") && i+1 < len(fields) {
				next := cleanReplayCommandToken(fields[i+1])
				if next != "" && !strings.HasPrefix(next, "-") {
					i++
				}
			}
			continue
		}
		return prefix + " " + token
	}
	return ""
}

func cleanReplayCommandToken(token string) string {
	return strings.Trim(strings.TrimSpace(token), `"'`)
}

func systemctlReplayIntent(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	parts := []string{cleanReplayCommandToken(fields[0])}
	hasUser := false
	subcommand := ""
	for _, raw := range fields[1:] {
		token := cleanReplayCommandToken(raw)
		if token == "" {
			continue
		}
		if token == "--user" {
			hasUser = true
			continue
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		if subcommand == "" {
			subcommand = token
			break
		}
	}
	if hasUser {
		parts = append(parts, "--user")
	}
	if subcommand != "" {
		parts = append(parts, subcommand)
	}
	return strings.Join(parts, " ")
}

func genericCommandIntent(fields []string) string {
	command := cleanReplayCommandToken(fields[0])
	if command == "" {
		return ""
	}
	for _, raw := range fields[1:] {
		token := cleanReplayCommandToken(raw)
		if token == "" || strings.HasPrefix(token, "-") {
			continue
		}
		return command + " " + token
	}
	return command
}

func looksOpaqueReplayFragment(value string) bool {
	if len([]rune(value)) < 48 {
		return false
	}
	if strings.ContainsAny(value, " /:-_.") {
		return false
	}
	return true
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
	pendingToolResults := make(map[string]int)
	for _, msg := range msgs {
		if msg.Role == "assistant" {
			sctx.Turns++
		}
		if text := msg.TextContent(); text != "" {
			textParts = append(textParts, msg.Role+": "+text)
		}
		extractSkillLifecycleToolActivities(msg.Content, pendingToolResults, &sctx.ToolActivities)
	}
	sctx.AllText = strings.Join(textParts, "\n")
	return sctx, nil
}

func extractSkillLifecycleToolActivities(content json.RawMessage, pending map[string]int, activities *[]genesis.ToolActivity) {
	if len(content) == 0 || content[0] != '[' {
		return
	}
	var blocks []llm.ContentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		switch b.Type {
		case "tool_use":
			if strings.TrimSpace(b.Name) == "" {
				continue
			}
			*activities = append(*activities, genesis.ToolActivity{
				Name:  b.Name,
				Input: compactJSONForReplay(b.Input),
			})
			if b.ID != "" && pending != nil {
				pending[b.ID] = len(*activities) - 1
			}
		case "tool_result":
			if pending == nil || b.ToolUseID == "" {
				continue
			}
			idx, ok := pending[b.ToolUseID]
			if !ok || idx < 0 || idx >= len(*activities) {
				continue
			}
			(*activities)[idx].IsError = b.IsError
			(*activities)[idx].Output = truncateRunes(strings.TrimSpace(b.Content), 2000)
		}
	}
}

func compactJSONForReplay(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err == nil {
		return truncateRunes(buf.String(), 1000)
	}
	return truncateRunes(strings.TrimSpace(string(raw)), 1000)
}
