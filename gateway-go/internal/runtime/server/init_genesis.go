package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
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

	s.genesisEvolver = genesis.NewEvolver(lwClient, s.skillCatalog, s.genesisTracker, lwModel, s.logger)

	// Teacher-escalation: wire the stronger main model so a rewrite that fails
	// the lightweight self-test gets one escalated attempt (#4). The evolver
	// calls the client directly (not SendSync), so it takes the BARE model name
	// (Model(), not FullModelID() — the latter is only for the provider-
	// re-resolving SendSync path, see the review-fork note above). Skipped when
	// main is unconfigured or identical to lightweight (escalation would be a
	// no-op).
	mainClient := s.modelRegistry.Client(modelrole.RoleMain)
	mainModel := s.modelRegistry.Model(modelrole.RoleMain)
	if mainClient != nil && mainModel != "" && mainModel != lwModel {
		s.genesisEvolver.SetTeacher(mainClient, mainModel)
	}

	// Iteration-based nudger (Hermes-style): fires a mid-session skill
	// review every N tool calls. Env var DENEB_SKILL_NUDGE_INTERVAL
	// overrides the default (10); 0 disables.
	// The review fork dispatches through chat.SendSync, which re-resolves the model string into a
	// provider via resolveModel — so it needs the FULL "provider/model" id. Model() returns the
	// bare name (e.g. "step3p7"), which has no provider and fails client resolution
	// ("no LLM client available, provider=\"\""), silently killing every nudger review and leaving
	// the whole skill self-evolution loop dead. Generate() uses lwClient directly, so the bare name
	// is fine there; only this SendSync path needs the prefix.
	reviewModel := s.modelRegistry.FullModelID(modelrole.RoleLightweight)
	reviewFork := newSkillReviewFork(s.chatHandler, s.genesisTranscripts, reviewModel, s.logger)
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
		s.chatHandler.SetSkillUsageRecorder(newChatUsageRecorderAdapter(s.genesisTracker, s.logger))
	}
	s.registerSkillLifecycleTool()

	s.logger.Info("genesis: services initialized",
		"model", lwModel, "outputDir", cfg.OutputDir,
		"nudgeInterval", s.genesisNudger.Interval(),
		"minToolCalls", cfg.MinToolCalls,
		"minTurns", cfg.MinTurns,
		"maxSkillsPerDay", cfg.MaxSkillsPerDay)
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
	inner  *genesis.Tracker
	logger *slog.Logger
}

func newChatUsageRecorderAdapter(t *genesis.Tracker, logger *slog.Logger) chat.SkillUsageRecorder {
	return &chatUsageRecorderAdapter{inner: t, logger: logger}
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
	}); err != nil && a.logger != nil {
		// Usage telemetry is best-effort — a write failure must never affect the
		// chat turn, but log it so a persistently failing tracker is visible.
		a.logger.Warn("genesis: skill usage record failed", "skill", skillName, "error", err)
	}
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
	}

}
