package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/rl"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
)

// initRLService creates the RL self-learning service if enabled in config.
// Called before buildHub() so hub.RLService() is available during early
// method registration. The service orchestrates external Python processes
// (sglang + Tinker + Atropos) — it does not run training itself.
func (s *Server) initRLService() {
	cfg := rl.DefaultConfig()
	if !cfg.Enable {
		return
	}

	s.rlService = rl.NewService(cfg, s.processes, s.logger)
	s.logger.Info("rl: service initialized",
		"model", cfg.BaseModelPath,
		"adapterDir", cfg.AdapterDir,
	)
}

// registerRLSideEffects wires the RL SessionHook to collect training
// trajectories from completed sessions. Called during
// registerWorkflowSideEffects (non-RPC phase).
func (s *Server) registerRLSideEffects(_ *rpcutil.GatewayHub) {
	if s.rlService == nil {
		return
	}

	cfg := rl.DefaultConfig()
	s.rlHook = rl.NewSessionHook(
		s.rlService.Store(),
		s.sessions,
		cfg.Collection,
		s.logger,
	)
	s.logger.Info("rl: session hook subscribed")
}
