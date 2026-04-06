package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/rl"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
)

// initRLService creates the RL self-learning service if enabled via config
// or environment variables. Called before buildHub() so hub.RLService() is
// available during early method registration.
//
// The service orchestrates external Python processes (sglang + Tinker +
// Atropos) for task-specific RL — it does not run training itself.
func (s *Server) initRLService() {
	cfg := rl.ConfigFromEnv()
	if !cfg.Enabled {
		return
	}

	s.rlService = rl.NewService(cfg, s.processes, s.logger)
	s.logger.Info("rl: service initialized",
		"model", cfg.BaseModelPath,
		"adapterDir", cfg.AdapterDir,
		"environments", len(cfg.Environments),
	)
}

// registerRLSideEffects wires the RL collector as the local AI hub observer
// and (optionally) the session hook for trajectory collection.
// Called during registerWorkflowSideEffects (non-RPC phase).
func (s *Server) registerRLSideEffects(_ *rpcutil.GatewayHub) {
	if s.rlService == nil {
		return
	}

	// Wire hub observer for task-specific trajectory collection.
	if s.localAIHub != nil {
		s.localAIHub.SetObserver(s.rlService.Collector().Observe)
		s.logger.Info("rl: hub observer registered")
	}

	// Wire session hook for session-level trajectory collection (fallback path).
	cfg := rl.ConfigFromEnv()
	s.rlHook = rl.NewSessionHook(
		s.rlService.Store(),
		s.sessions,
		cfg.Collection,
		s.logger,
	)
	s.logger.Info("rl: session hook subscribed")
}
