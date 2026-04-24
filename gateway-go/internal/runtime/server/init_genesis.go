package server

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
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

	cfg := genesis.DefaultConfig()
	cfg.Model = lwModel

	// Shared catalog so genesis can register generated skills and evolver can look them up.
	s.skillCatalog = skills.NewCatalog(s.logger)

	s.genesisSvc = genesis.NewService(cfg, lwClient, s.skillCatalog, s.logger)

	tracker, err := genesis.NewTracker(s.logger)
	if err != nil {
		s.logger.Warn("genesis: tracker unavailable", "error", err)
	} else {
		s.genesisTracker = tracker
	}

	s.genesisEvolver = genesis.NewEvolver(lwClient, s.skillCatalog, s.genesisTracker, lwModel, s.logger)

	// Iteration-based nudger (Hermes-style): fires a mid-session skill
	// review every N tool calls. Env var DENEB_SKILL_NUDGE_INTERVAL
	// overrides the default (10); 0 disables.
	s.genesisNudger = genesis.NewNudgerFromEnv(s.genesisSvc, s.logger)

	// Install an adapter so the chat handler can invoke the nudger
	// without importing the genesis package (dependency inversion).
	if s.chatHandler != nil && s.genesisNudger.Enabled() {
		s.chatHandler.SetSkillNudger(newChatNudgerAdapter(s.genesisNudger))
	}

	s.logger.Info("genesis: services initialized",
		"model", lwModel, "outputDir", cfg.OutputDir,
		"nudgeInterval", s.genesisNudger.Interval())
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

// registerGenesisAutonomousTasks registers periodic background tasks for genesis.
// Called during registerWorkflowSideEffects (non-RPC phase).
func (s *Server) registerGenesisAutonomousTasks(_ *rpcutil.GatewayHub) {
	if s.genesisSvc == nil || s.autonomousSvc == nil {
		return
	}

	if s.genesisTracker != nil {
		s.autonomousSvc.RegisterTask(&genesis.EvolutionTask{
			Evolver: s.genesisEvolver,
			Logger:  s.logger,
		})
	}

}
