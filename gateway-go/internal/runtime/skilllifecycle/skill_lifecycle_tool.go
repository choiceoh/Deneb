package skilllifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
)

const (
	skillLifecycleTimeout                       = 90 * time.Second
	skillLifecycleMaxStatusLogEntries           = 50
	skillLifecycleMaxProposalResultBytes        = 12288
	skillLifecycleMaxValidationBackfillSessions = 50
)

type skillLifecycleBackend struct {
	genesis         *generation.Service
	evolver         *genesis.Evolver
	tracker         *genesis.Tracker
	transcripts     toolport.TranscriptStore
	logger          *slog.Logger
	relevanceClient *llm.Client
	relevanceModel  string
	statusCacheMu   sync.Mutex
	statusCache     map[skillLifecycleStatusCacheKey]skillLifecycleStatusCacheEntry

	// Heartbeat shadow-replay deps (P1, heartbeat_shadow_replay.go). Nil/empty
	// on backends that never serve the tool (e.g. the backfill task).
	shadowReplay func(ctx context.Context, candidate string, limit int) (chattools.HeartbeatShadowReplayResult, error)
}

var _ chattools.SkillLifecycleBackend = (*skillLifecycleBackend)(nil)

// Backend implements the chat skill-lifecycle control-plane boundary.
type Backend = skillLifecycleBackend

// BackendConfig contains the Propus services and optional replay boundary used
// by a lifecycle backend.
type BackendConfig struct {
	Genesis     *generation.Service
	Evolver     *genesis.Evolver
	Tracker     *genesis.Tracker
	Transcripts toolport.TranscriptStore
	Logger      *slog.Logger
	// RelevanceClient/RelevanceModel (lightweight text role) gate retro-backfilled
	// validation cases by whether the session actually exercised the skill —
	// keeping consulted-but-off-topic sessions out of a skill's held-out corpus.
	// Optional; nil records everything (prior behavior).
	RelevanceClient *llm.Client
	RelevanceModel  string
	ShadowReplay    func(ctx context.Context, candidate string, limit int) (chattools.HeartbeatShadowReplayResult, error)
}

// NewBackend constructs a lifecycle backend from explicit dependencies.
func NewBackend(cfg BackendConfig) *Backend {
	return &skillLifecycleBackend{
		genesis:         cfg.Genesis,
		evolver:         cfg.Evolver,
		tracker:         cfg.Tracker,
		transcripts:     cfg.Transcripts,
		logger:          cfg.Logger,
		relevanceClient: cfg.RelevanceClient,
		relevanceModel:  strings.TrimSpace(cfg.RelevanceModel),
		shadowReplay:    cfg.ShadowReplay,
	}
}

// HeartbeatShadowReplay dry-runs a candidate HEARTBEAT.md body over the
// harvested fixture corpus. Report only — nothing is applied (the surface
// registry keeps heartbeat-instructions propose-only).
func (b *skillLifecycleBackend) HeartbeatShadowReplay(ctx context.Context, req chattools.HeartbeatShadowReplayRequest) (chattools.HeartbeatShadowReplayResult, error) {
	if b.shadowReplay == nil {
		return chattools.HeartbeatShadowReplayResult{}, fmt.Errorf("heartbeat shadow replay is not configured on this backend")
	}
	return b.shadowReplay(ctx, req.Candidate, req.Limit)
}

// ProposeSkillEvolution creates a skill-evolution proposal through the lifecycle backend.
func (b *skillLifecycleBackend) ProposeSkillEvolution(ctx context.Context, req chattools.SkillEvolutionProposalRequest) (chattools.SkillEvolutionProposalResult, error) {
	if req.SessionKey == "" {
		req.SessionKey = toolport.SessionKeyFromContext(ctx)
	}
	route := normalizeSkillLifecycleRoute(req.Route)
	if route == "" {
		return chattools.SkillEvolutionProposalResult{}, fmt.Errorf("route must be one of no-op, genesis, create, evolve")
	}
	// A no-op proposal records "no skill-worthy pattern, nothing to do" — there
	// is no reusable candidate by definition. Only executable routes (genesis/
	// create/evolve) require one. Forcing candidate on no-op made the reviewer
	// agent fail repeatedly with "candidate is required for propose".
	if route != "no-op" && strings.TrimSpace(req.Candidate) == "" {
		return chattools.SkillEvolutionProposalResult{}, fmt.Errorf("candidate is required for propose with route=%q", route)
	}

	result := chattools.SkillEvolutionProposalResult{
		OK:        true,
		Candidate: req.Candidate,
		Route:     route,
	}
	if req.Reason != "" {
		result.Reason = req.Reason
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
	// Gate-first: a route=evolve proposal naming a skill the evolver would refuse
	// anyway (thrash cooldown, rejection backoff, recency gate) is answered with
	// the gate reason instead of executed. Executing it only produced an
	// evolve_rejected row that fed the rejection backoff — in 2026-08 the
	// reviewer nominated morning-letter 17 times in 21 days and the recency gate
	// stopped every attempt after the fact. The proposal is still recorded, with
	// the suppression, so L1 status counts the waste; the review verdict usage
	// still lands because the reviewer's judgment about the skill stands.
	if route == "evolve" && b.evolver != nil && strings.TrimSpace(req.SkillName) != "" {
		if gated, why := b.evolver.EvolutionSuppressed(req.SkillName); gated {
			result.Suppressed = why
			result.NextAction = "이 스킬은 지금 진화 게이트에 걸려 있어 실행하지 않았습니다: " + why +
				". 이 세션이 이 스킬을 실제로 consult하지 않았다면 route=no-op로 다시 제안하고, 스킬이 쓰이게 만드는 게 목적이면 그 경로(크론 지시·진입 힌트)의 수정을 제안하세요."
		}
	}

	b.recordReviewUsage(req, route)
	b.logProposal(req, route, result)

	if req.Execute && result.Suppressed == "" {
		switch route {
		case "genesis":
			execResult, execErr := b.RunSkillGenesis(ctx, chattools.SkillGenesisRequest{
				SessionKey:   req.SessionKey,
				DreamSummary: req.DreamSummary,
			})
			if execErr != nil {
				result.OK = false
				result.Error = execErr.Error()
			} else {
				result.Executed = true
				result.Result = &chattools.SkillEvolutionProposalExecutionResult{Genesis: &execResult}
			}
		case "evolve":
			// Pass the review's reasoning + evidence as the improvement finding
			// so the evolver can act on the LLM's verdict without usage stats.
			finding := strings.TrimSpace(req.Reason + "\n" + req.Evidence)
			execResult, execErr := b.RunSkillEvolution(ctx, chattools.SkillEvolutionRequest{
				SkillName: req.SkillName,
				Finding:   finding,
			})
			if execErr != nil {
				result.OK = false
				result.Error = execErr.Error()
			} else {
				result.Executed = true
				result.Result = &chattools.SkillEvolutionProposalExecutionResult{Evolution: &execResult}
			}
		case "create":
			result.NextAction = "load skill-factory, then use skills action=create"
		case "no-op":
			// Nothing to execute; the proposal record is the result.
		}
	}

	return result, nil
}

// RunSkillGenesis runs a requested skill-genesis operation.
func (b *skillLifecycleBackend) RunSkillGenesis(ctx context.Context, req chattools.SkillGenesisRequest) (chattools.SkillGenesisResult, error) {
	if b.genesis == nil {
		return chattools.SkillGenesisResult{}, fmt.Errorf("skill genesis is not configured")
	}
	if req.SessionKey == "" {
		req.SessionKey = toolport.SessionKeyFromContext(ctx)
	}
	if strings.TrimSpace(req.SessionKey) == "" && strings.TrimSpace(req.DreamSummary) == "" {
		return chattools.SkillGenesisResult{}, fmt.Errorf("sessionKey or dreamSummary is required")
	}

	ctx, cancel := context.WithTimeout(ctx, skillLifecycleTimeout)
	defer cancel()

	source := "session"
	var sessionKey string
	var skill *generation.GeneratedSkill
	var err error
	if strings.TrimSpace(req.DreamSummary) != "" {
		source = "dream"
		skill, err = b.genesis.GenerateFromDream(ctx, req.DreamSummary)
	} else {
		sessionKey = req.SessionKey
		sctx, buildErr := buildSkillLifecycleSessionContext(b.transcripts, req.SessionKey)
		if buildErr != nil {
			return chattools.SkillGenesisResult{}, fmt.Errorf("load session: %w", buildErr)
		}
		skill, err = b.genesis.Generate(ctx, sctx)
	}
	if err != nil {
		return chattools.SkillGenesisResult{}, err
	}
	if skill == nil {
		return chattools.SkillGenesisResult{
			OK:     true,
			Skip:   true,
			Reason: "no skill-worthy pattern detected",
			Source: source,
		}, nil
	}
	if err := b.genesis.Persist(skill); err != nil {
		if errors.Is(err, generation.ErrSkillDeduped) {
			return chattools.SkillGenesisResult{
				OK:     true,
				Skip:   true,
				Reason: "existing skill already covers this (deduplicated)",
				Source: source,
			}, nil
		}
		return chattools.SkillGenesisResult{}, err
	}
	if b.tracker != nil {
		_ = b.tracker.LogGenesis(skill.Name, source, sessionKey, skill.Category, skill.Description)
		b.clearSkillLifecycleStatusCache()
	}
	return chattools.SkillGenesisResult{
		OK:     true,
		Source: source,
		Skill:  skill,
	}, nil
}

// RunSkillEvolution runs a requested skill-evolution operation.
func (b *skillLifecycleBackend) RunSkillEvolution(ctx context.Context, req chattools.SkillEvolutionRequest) (chattools.SkillEvolutionResult, error) {
	if b.evolver == nil {
		return chattools.SkillEvolutionResult{}, fmt.Errorf("skill evolver is not configured")
	}
	if strings.TrimSpace(req.SkillName) == "" {
		return chattools.SkillEvolutionResult{}, fmt.Errorf("skillName is required")
	}
	ctx, cancel := context.WithTimeout(ctx, skillLifecycleTimeout)
	defer cancel()
	result, err := b.evolver.EvolveSkill(ctx, req.SkillName, req.Finding)
	if err != nil {
		return chattools.SkillEvolutionResult{}, err
	}
	if b.tracker != nil && result != nil && result.Evolved {
		_ = b.tracker.MarkSkillPatched(req.SkillName)
	}
	b.clearSkillLifecycleStatusCache()
	return chattools.SkillEvolutionResult{
		OK:     true,
		Result: result,
	}, nil
}

// RunSkillCuratorAction runs a requested curator action.
func (b *skillLifecycleBackend) RunSkillCuratorAction(_ context.Context, req chattools.SkillCuratorActionRequest) (chattools.SkillCuratorActionResult, error) {
	if b.tracker == nil {
		return chattools.SkillCuratorActionResult{
			Reason: "skill tracker is not configured",
		}, nil
	}
	skillName := strings.TrimSpace(req.SkillName)
	if skillName == "" {
		return chattools.SkillCuratorActionResult{}, fmt.Errorf("skillName is required")
	}
	switch req.Action {
	case "pin":
		if err := b.tracker.SetSkillPinned(skillName, true); err != nil {
			return chattools.SkillCuratorActionResult{}, err
		}
		b.clearSkillLifecycleStatusCache()
	case "unpin":
		if err := b.tracker.SetSkillPinned(skillName, false); err != nil {
			return chattools.SkillCuratorActionResult{}, err
		}
		b.clearSkillLifecycleStatusCache()
	case "archive":
		rec, err := b.tracker.SetSkillCuratorState(skillName, genesis.SkillCuratorStateArchived)
		if err != nil {
			return chattools.SkillCuratorActionResult{}, err
		}
		b.clearSkillLifecycleStatusCache()
		return chattools.SkillCuratorActionResult{
			OK:        true,
			Action:    lifecycleValue(req.Action),
			SkillName: lifecycleValue(skillName),
			Curator:   &chattools.SkillCuratorResult{Record: &rec},
		}, nil
	case "restore":
		rec, err := b.tracker.SetSkillCuratorState(skillName, genesis.SkillCuratorStateActive)
		if err != nil {
			return chattools.SkillCuratorActionResult{}, err
		}
		b.clearSkillLifecycleStatusCache()
		return chattools.SkillCuratorActionResult{
			OK:        true,
			Action:    lifecycleValue(req.Action),
			SkillName: lifecycleValue(skillName),
			Curator:   &chattools.SkillCuratorResult{Record: &rec},
		}, nil
	default:
		return chattools.SkillCuratorActionResult{}, fmt.Errorf("unsupported curator action %q", req.Action)
	}
	curator, err := b.tracker.SkillCuratorReport(skillName)
	if err != nil {
		return chattools.SkillCuratorActionResult{}, err
	}
	return chattools.SkillCuratorActionResult{
		OK:        true,
		Action:    lifecycleValue(req.Action),
		SkillName: lifecycleValue(skillName),
		Curator:   &chattools.SkillCuratorResult{Records: lifecycleValue(curator)},
	}, nil
}

func (b *skillLifecycleBackend) logProposal(req chattools.SkillEvolutionProposalRequest, route string, result chattools.SkillEvolutionProposalResult) {
	if b.tracker == nil {
		return
	}
	resultText := ""
	if data, err := json.Marshal(result); err == nil {
		resultText = truncateSkillLifecycleProposalResult(string(data))
	}
	if err := b.tracker.LogEvolutionProposal(genesis.EvolutionProposalRecord{
		Candidate:  req.Candidate,
		Route:      route,
		SessionKey: req.SessionKey,
		SkillName:  req.SkillName,
		Evidence:   req.Evidence,
		Reason:     req.Reason,
		Executed:   result.Executed,
		Result:     resultText,
		Suppressed: result.Suppressed,
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
		Executed:   result.Executed,
		Source:     "skill_lifecycle",
	}); err != nil && b.logger != nil {
		b.logger.Warn("skill lifecycle: opportunity log failed", "error", err)
	}
	b.clearSkillLifecycleStatusCache()
}

func lifecycleValue[T any](value T) *T {
	return &value
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
		return 8
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
	seen := make(map[string]struct{}, len(base))
	out := make([]string, 0, len(base))
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
