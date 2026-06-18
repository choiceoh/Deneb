package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// initGenesisServices creates the genesis service, tracker, and evolver.
// Called after chatHandler is created but BEFORE registerLateMethods, so the
// RPC methods can be registered in method_registry.go (Rule 1 compliance).
func (s *Server) initGenesisServices() {
	if s.chatHandler == nil || s.modelRegistry == nil {
		s.logger.Debug("genesis: skipped (chat handler or model registry unavailable)")
		return
	}

	lwClient := s.modelRegistry.Client(modelrole.RoleLightweight)
	lwModel := s.modelRegistry.Model(modelrole.RoleLightweight)
	if lwClient == nil || lwModel == "" {
		s.logger.Debug("genesis: skipped (lightweight model not configured)")
		return
	}
	cfg := genesis.DefaultConfigFromEnv()
	cfg.Model = lwModel

	// Shared catalog so genesis can register generated skills and evolver can look them up.
	s.skillCatalog = skills.NewCatalog(s.logger)
	s.seedSkillCatalog()

	s.genesisSvc = genesis.NewService(cfg, lwClient, s.skillCatalog, s.logger)

	tracker, err := genesis.NewTracker(s.logger)
	if err != nil {
		s.logger.Warn("genesis: tracker unavailable", "error", err)
	} else {
		s.genesisTracker = tracker
	}

	// Reconcile orphan curator entries against the freshly-discovered catalog:
	// a skill removed or consolidated away otherwise leaves a lifecycle record
	// that lingers forever and skews the agent-skill value metric. Race-free at
	// startup; the reconcile itself guards against a discovery failure wiping
	// history (see ReconcileCuratorAgainstCatalog).
	if s.genesisTracker != nil && s.skillCatalog != nil {
		known := map[string]bool{}
		for _, e := range s.skillCatalog.List() {
			known[e.Skill.Name] = true
		}
		if pruned, rerr := s.genesisTracker.ReconcileCuratorAgainstCatalog(known); rerr != nil {
			s.logger.Warn("genesis: curator reconcile failed", "error", rerr)
		} else if len(pruned) > 0 {
			s.logger.Info("genesis: pruned orphan curator entries", "skills", pruned)
		}
	}

	s.genesisEvolver = genesis.NewEvolver(lwClient, s.skillCatalog, s.genesisTracker, lwModel, s.logger)
	evolverRole, evolverModel := s.configureGenesisEvolverModels(s.genesisEvolver)
	thinkingKwargs := s.genesisThinkingKwargs()

	// Quality-gate generated skills with the stronger main model (judge !=
	// producer): rejects semantic duplicates + vague/one-off skills the
	// specificity heuristic can't catch. Self-generated skills are net-harmful
	// unless curated (SoK SkillsBench -1.3pp), so this is the genesis counterpart
	// to the evolver's self-test. Thinking off (same dsv4 toggle as the evolver).
	mainClient := s.modelRegistry.Client(modelrole.RoleMain)
	mainModel := s.modelRegistry.Model(modelrole.RoleMain)
	if mainClient != nil && mainModel != "" {
		s.genesisSvc.SetJudge(mainClient, mainModel, &llm.ThinkingConfig{
			Type:          "disabled",
			TemplateKwarg: thinkingKwargs[mainModel],
		})
	}

	// Iteration-based nudger (Hermes-style): fires a mid-session skill
	// review every N tool calls. Env var DENEB_SKILL_NUDGE_INTERVAL
	// overrides genesis.DefaultNudgeInterval; 0 disables.
	// The review fork dispatches through chat.SendSync, which re-resolves the model string into a
	// provider via resolveModel — so it needs the FULL "provider/model" id. Model() returns the
	// bare name (e.g. "step3p7"), which has no provider and fails client resolution
	// ("no LLM client available, provider=\"\""), silently killing every nudger review and leaving
	// the whole skill self-evolution loop dead. Generate() uses lwClient directly, so the bare name
	// is fine there; only this SendSync path needs the prefix.
	reviewModel := s.modelRegistry.FullModelID(modelrole.RoleLightweight)
	reviewFork := newSkillReviewFork(s.chatHandler, s.genesisTranscripts, s.genesisTracker, reviewModel, s.logger)
	s.genesisNudger = genesis.NewNudgerFromEnvWithTrackerAndReviewer(
		s.genesisSvc,
		s.genesisTracker,
		reviewFork,
		s.logger,
	)

	// Install an adapter so the chat handler can invoke the nudger
	// without importing the genesis package (dependency inversion).
	if s.chatHandler != nil && s.genesisNudger.Enabled() {
		s.chatHandler.SetSkillNudger(newChatNudgerAdapter(s.genesisNudger))
	}
	// Usage attribution is independent of the nudger: even with the nudger
	// disabled, recording which skills are used (and whether their turns
	// succeed) gives the Evolver the success-rate signal its
	// SkillsNeedingEvolution gate reads — without it the loop runs blind.
	if s.chatHandler != nil && s.genesisTracker != nil {
		s.chatHandler.SetSkillUsageRecorder(newChatUsageRecorderAdapter(s.genesisTracker, s.genesisTranscripts, s.logger))
	}
	s.registerSkillLifecycleTool()

	s.logger.Info("genesis: services initialized",
		"model", lwModel, "evolverRole", evolverRole, "evolverModel", evolverModel, "outputDir", cfg.OutputDir,
		"nudgeInterval", s.genesisNudger.Interval(),
		"minToolCalls", cfg.MinToolCalls,
		"minTurns", cfg.MinTurns,
		"maxSkillsPerDay", cfg.MaxSkillsPerDay)
}

func (s *Server) refreshCodingModelConsumers() {
	if s.modelRegistry == nil {
		return
	}
	codingModel := s.modelRegistry.FullModelID(modelrole.RoleCoding)
	if s.toolDeps != nil {
		s.toolDeps.Sessions.CodingDefaultModel = codingModel
	}
	if s.genesisEvolver != nil {
		role, model := s.configureGenesisEvolverModels(s.genesisEvolver)
		s.logger.Info("genesis: evolver model refreshed",
			"codingModel", codingModel, "evolverRole", role, "evolverModel", model)
	}
}

func (s *Server) configureGenesisEvolverModels(evolver *genesis.Evolver) (modelrole.Role, string) {
	if evolver == nil || s.modelRegistry == nil {
		return "", ""
	}
	evolverRole := modelrole.RoleLightweight
	evolverClient := s.modelRegistry.Client(modelrole.RoleLightweight)
	evolverModel := s.modelRegistry.Model(modelrole.RoleLightweight)
	if codingModel := s.modelRegistry.Model(modelrole.RoleCoding); codingModel != "" {
		if codingClient := s.modelRegistry.Client(modelrole.RoleCoding); codingClient != nil {
			evolverRole = modelrole.RoleCoding
			evolverClient = codingClient
			evolverModel = codingModel
		}
	}
	evolver.SetPrimary(evolverClient, evolverModel)

	// Teacher-escalation: wire the stronger main model so a rewrite that fails
	// the default lightweight self-test gets one escalated attempt (#4). When a
	// dedicated coding model is configured, it owns the patch-generation path;
	// keep main out of the rewrite loop so code/skill edits are made by the
	// coding role the operator selected.
	mainClient := s.modelRegistry.Client(modelrole.RoleMain)
	mainModel := s.modelRegistry.Model(modelrole.RoleMain)
	if evolverRole != modelrole.RoleCoding && mainClient != nil && mainModel != "" && mainModel != evolverModel {
		evolver.SetTeacher(mainClient, mainModel)
	} else {
		evolver.SetTeacher(nil, "")
	}
	evolver.SetThinkingKwargs(s.genesisThinkingKwargs())
	return evolverRole, evolverModel
}

func (s *Server) genesisThinkingKwargs() map[string]string {
	if s.modelRegistry == nil {
		return nil
	}
	// Resolve per-model thinking toggles so the evolver's judge/teacher/rewrite
	// calls truly disable reasoning on dual-mode vLLM models (dsv4) instead of
	// burning their whole output budget on chain-of-thought and returning
	// truncated JSON ("judge error"). Keyed by bare model name to match the names
	// the evolver passes to thinkingOff.
	thinkingKwargs := map[string]string{}
	for _, role := range []modelrole.Role{modelrole.RoleLightweight, modelrole.RoleCoding, modelrole.RoleMain} {
		mc := s.modelRegistry.Config(role)
		if mc.Model == "" {
			continue
		}
		if k := s.modelRegistry.CapabilityForModel(mc.ProviderID, mc.Model).ThinkingToggleKwarg; k != "" {
			thinkingKwargs[mc.Model] = k
		}
	}
	return thinkingKwargs
}

func (s *Server) seedSkillCatalog() {
	if s.skillCatalog == nil {
		return
	}
	workspaceDir := ""
	if s.toolDeps != nil {
		workspaceDir = s.toolDeps.WorkspaceDir
	}
	if workspaceDir == "" {
		workspaceDir = resolveWorkspaceDir()
	}
	entries := skills.DiscoverWorkspaceSkills(skills.DiscoverConfig{
		WorkspaceDir: workspaceDir,
		Logger:       s.logger,
	})
	for _, entry := range entries {
		s.skillCatalog.Register(entry)
	}
	if len(entries) > 0 {
		s.logger.Info("genesis: seeded skill catalog", "skills", len(entries), "workspace", workspaceDir)
	}
}

func (s *Server) registerSkillLifecycleTool() {
	if s.chatHandler == nil || s.genesisSvc == nil {
		return
	}
	backend := &skillLifecycleBackend{
		genesis:     s.genesisSvc,
		evolver:     s.genesisEvolver,
		tracker:     s.genesisTracker,
		transcripts: s.genesisTranscripts,
		logger:      s.logger,
	}
	s.chatHandler.RegisterTool(toolctx.ToolDef{
		Name: "skill_lifecycle",
		Description: "Closed-loop skill self-evolution: propose (record/route reusable workflow decisions), " +
			"genesis (generate a skill from sessionKey or dreamSummary), evolve (improve an existing skill), " +
			"status (inspect recent lifecycle logs, usage stats, and curator state), " +
			"validation_case (record held-out replay assertions for future candidate selection), " +
			"validation_case_from_session (extract held-out replay assertions from a real session trace), " +
			"pin/unpin/archive/restore (manual state for agent-created skills). " +
			"Use through the evolution-proposal skill after meaningful workflows.",
		InputSchema: chattools.SkillLifecycleToolSchema(),
		Fn:          chattools.ToolSkillLifecycle(backend),
		Deferred:    true,
	})
}

// chatNudgerAdapter adapts *genesis.Nudger to chat.SkillNudger. It lives
// in the server package (the only place that knows about both types) so
// neither chat nor genesis needs to import the other.
type chatNudgerAdapter struct {
	inner *genesis.Nudger
}

func newChatNudgerAdapter(n *genesis.Nudger) chat.SkillNudger {
	return &chatNudgerAdapter{inner: n}
}

func (a *chatNudgerAdapter) Enabled() bool { return a.inner.Enabled() }

func (a *chatNudgerAdapter) OnToolCalls(ctx context.Context, sessionKey string, delta int, snap chat.SkillNudgeSnapshot) {
	activities := make([]genesis.ToolActivity, 0, len(snap.ToolActivities))
	for _, t := range snap.ToolActivities {
		activities = append(activities, genesis.ToolActivity{
			Name: t.Name, IsError: t.IsError,
		})
	}
	a.inner.OnToolCalls(ctx, sessionKey, delta, genesis.SessionContext{
		Key:            sessionKey,
		Label:          snap.Label,
		Model:          snap.Model,
		Turns:          snap.Turns,
		ToolActivities: activities,
		AllText:        snap.AllText,
	})
}

func (a *chatNudgerAdapter) Reset(sessionKey string) { a.inner.Reset(sessionKey) }

// chatUsageRecorderAdapter adapts *genesis.Tracker to chat.SkillUsageRecorder,
// translating per-turn skill-consult outcomes from the chat run loop into
// genesis usage records. Lives in the server package (the only place that knows
// both types) so neither chat nor genesis imports the other.
type chatUsageRecorderAdapter struct {
	inner       *genesis.Tracker
	transcripts toolctx.TranscriptStore
	logger      *slog.Logger
}

func newChatUsageRecorderAdapter(t *genesis.Tracker, transcripts toolctx.TranscriptStore, logger *slog.Logger) chat.SkillUsageRecorder {
	return &chatUsageRecorderAdapter{inner: t, transcripts: transcripts, logger: logger}
}

func (a *chatUsageRecorderAdapter) RecordSkillUse(sessionKey, skillName string, success bool, errMsg string) {
	if a == nil || a.inner == nil {
		return
	}
	if err := a.inner.RecordUsage(genesis.UsageRecord{
		SkillName:  skillName,
		SessionKey: sessionKey,
		Success:    success,
		ErrorMsg:   errMsg,
		Source:     genesis.UsageSourceReal,
	}); err != nil && a.logger != nil {
		// Usage telemetry is best-effort — a write failure must never affect the
		// chat turn, but log it so a persistently failing tracker is visible.
		a.logger.Warn("genesis: skill usage record failed", "skill", skillName, "error", err)
	}
	if !success {
		safego.GoWithSlog(a.logger, "skill-failed-use-validation-case", func() {
			a.recordValidationCaseFromFailedUse(sessionKey, skillName, errMsg)
		})
	}
}

func (a *chatUsageRecorderAdapter) recordValidationCaseFromFailedUse(sessionKey, skillName, errMsg string) {
	sessionKey = strings.TrimSpace(sessionKey)
	skillName = strings.TrimSpace(skillName)
	if sessionKey == "" || skillName == "" || a.transcripts == nil {
		return
	}
	sctx, err := buildSkillLifecycleSessionContext(a.transcripts, sessionKey)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("genesis: auto validation case transcript load failed",
				"skill", skillName, "session", sessionKey, "error", err)
		}
		return
	}
	description := "Failed skill use in session " + sessionKey
	if msg := strings.TrimSpace(errMsg); msg != "" {
		description += ": " + truncateRunes(msg, 180)
	}
	record := buildSkillValidationCaseFromSession(chattools.SkillValidationCaseFromSessionRequest{
		SkillName:   skillName,
		SessionKey:  sessionKey,
		Description: description,
		Source:      "auto-failed-skill-use",
	}, sctx)
	record.Replay = failedUseValidationReplay(record.Replay)
	if a.validationCaseAlreadyRecorded(skillName, record.ID) {
		return
	}
	if err := a.inner.RecordSkillValidationCase(record); err != nil {
		if errors.Is(err, genesis.ErrWeakAutomaticValidationCase) {
			if a.logger != nil {
				a.logger.Debug("genesis: auto validation case skipped weak failed-use trace",
					"skill", skillName, "session", sessionKey)
			}
			return
		}
		if a.logger != nil {
			a.logger.Warn("genesis: auto validation case record failed",
				"skill", skillName, "session", sessionKey, "error", err)
		}
	}
}

func (a *chatUsageRecorderAdapter) validationCaseAlreadyRecorded(skillName, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	cases, err := a.inner.RecentSkillValidationCases(skillName, 50)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("genesis: auto validation case duplicate check failed",
				"skill", skillName, "id", id, "error", err)
		}
		return false
	}
	for _, tc := range cases {
		if strings.TrimSpace(tc.ID) == id {
			return true
		}
	}
	return false
}

func failedUseValidationReplay(replay genesis.SkillReplayCaseRecord) genesis.SkillReplayCaseRecord {
	forbidden := make([]genesis.SkillReplayToolCallRecord, 0, len(replay.ExpectedToolCalls))
	for _, call := range replay.ExpectedToolCalls {
		if !call.FixtureError || len(call.InputIncludes)+len(call.InputExcludes) == 0 {
			continue
		}
		forbidden = append(forbidden, genesis.SkillReplayToolCallRecord{
			Name:          call.Name,
			InputIncludes: append([]string(nil), call.InputIncludes...),
			InputExcludes: append([]string(nil), call.InputExcludes...),
		})
	}
	replay.RequiredTools = nil
	replay.ExpectedToolCalls = nil
	replay.ForbiddenToolCalls = append(replay.ForbiddenToolCalls, forbidden...)
	replay.RequireOrder = false
	return replay
}

// registerGenesisAutonomousTasks registers periodic background tasks for genesis.
// Called during registerWorkflowSideEffects (non-RPC phase).
func (s *Server) registerGenesisAutonomousTasks(_ *rpcutil.GatewayHub) {
	if s.genesisSvc == nil || s.autonomousSvc == nil {
		return
	}

	if s.genesisTracker != nil {
		evolveTask := &genesis.EvolutionTask{
			Evolver: s.genesisEvolver,
			Logger:  s.logger,
		}
		s.autonomousSvc.RegisterTask(evolveTask)
		s.autonomousSvc.RegisterTask(&genesis.SkillCuratorTask{
			Tracker: s.genesisTracker,
			Logger:  s.logger,
			Config:  genesis.SkillCuratorConfigFromEnv(),
		})

		// Event-driven evolve: after N new skills accumulate, run a cycle in
		// the background instead of waiting for the 6h periodic task. The
		// periodic task remains a backstop; EvolveUnderperformers is TryLock-
		// serialized so the two paths never overlap, and minGap suppresses a
		// re-fire too soon after a cycle.
		s.genesisTracker.SetEvolveTrigger(func() {
			ctx, cancel := context.WithTimeout(s.ShutdownCtx(), 10*time.Minute)
			defer cancel()
			_ = evolveTask.Run(ctx)
		}, genesis.DefaultEvolveEventThreshold, 30*time.Minute)

		// Post-evolve rollback: revert an evolution that regresses (N consecutive
		// post-evolve failures restore the skill from its backup). Closes the
		// evolve loop — generate -> gate -> cross-model judge -> watch -> revert.
		s.genesisTracker.SetRollback(s.genesisEvolver.RollbackSkill, genesis.DefaultRollbackThreshold)
	}

}
