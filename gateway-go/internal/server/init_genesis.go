package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/skills/genesis"
)

// initGenesisSubsystem sets up skill genesis: auto-creation from sessions,
// dream-to-skill pipeline, usage tracking, and periodic evolution.
// Must be called after chatHandler is available (in registerWorkflowSideEffects).
func (s *Server) initGenesisSubsystem(hub *rpcutil.GatewayHub) {
	if s.chatHandler == nil || s.modelRegistry == nil {
		s.logger.Debug("genesis: skipped (chat handler or model registry unavailable)")
		return
	}

	// Get lightweight LLM client for genesis/evolution (same as dreaming).
	lwClient := s.modelRegistry.Client(modelrole.RoleLightweight)
	lwModel := s.modelRegistry.Model(modelrole.RoleLightweight)
	if lwClient == nil || lwModel == "" {
		s.logger.Debug("genesis: skipped (lightweight model not configured)")
		return
	}

	cfg := genesis.DefaultConfig()
	cfg.Model = lwModel

	// Create the genesis service. Catalog is nil — genesis will create skills
	// without dedup checking against the catalog. The filesystem-based output
	// dir and cooldown mechanism prevent duplicates.
	s.genesisSvc = genesis.NewService(cfg, lwClient, nil, s.logger)

	// Create usage tracker (optional — failure is non-fatal).
	tracker, err := genesis.NewTracker(s.logger)
	if err != nil {
		s.logger.Warn("genesis: tracker unavailable", "error", err)
	} else {
		s.genesisTracker = tracker
	}

	// Create evolver. Also nil catalog — evolution reads SKILL.md from disk directly.
	s.genesisEvolver = genesis.NewEvolver(lwClient, nil, s.genesisTracker, lwModel, s.logger)

	// Register periodic evolution task.
	if s.autonomousSvc != nil && s.genesisTracker != nil {
		s.autonomousSvc.RegisterTask(&genesis.EvolutionTask{
			Evolver: s.genesisEvolver,
			Logger:  s.logger,
		})
	}

	// Register dream-to-skill task if Aurora is available.
	auroraStore := s.chatHandler.AuroraStore()
	if s.autonomousSvc != nil && auroraStore != nil {
		s.autonomousSvc.RegisterTask(
			genesis.NewDreamToSkillTask(s.genesisSvc, auroraStore, s.logger),
		)
	}

	// Register genesis RPC methods.
	genesisMethods := handlerskill.GenesisMethods(handlerskill.GenesisDeps{
		Genesis: s.genesisSvc,
		Evolver: s.genesisEvolver,
		Tracker: s.genesisTracker,
	})
	if len(genesisMethods) > 0 {
		s.dispatcher.RegisterDomain(genesisMethods)
	}

	s.logger.Info("genesis: initialized",
		"model", lwModel,
		"outputDir", cfg.OutputDir,
		"maxSkillsPerDay", cfg.MaxSkillsPerDay,
	)
}
