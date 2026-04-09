package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
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

	s.logger.Info("genesis: services initialized", "model", lwModel, "outputDir", cfg.OutputDir)
}

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
