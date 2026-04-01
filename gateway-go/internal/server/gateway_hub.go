package server

import "github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"

// buildHub assembles the GatewayHub from the Server's already-initialized fields.
// Called once during server.New() after all subsystems are created.
func (s *Server) buildHub() *rpcutil.GatewayHub {
	return rpcutil.NewGatewayHub(rpcutil.HubConfig{
		Broadcaster:    s.broadcaster,
		GatewaySubs:    s.gatewaySubs,
		Sessions:       s.sessions,
		Processes:      s.processes,
		Hooks:          s.hooks,
		InternalHooks:  s.internalHooks,
		Agents:         s.agents,
		JobTracker:     s.jobTracker,
		Cron:           s.cron,
		CronService:    s.cronService,
		CronPersistLog: s.cronRunLog,
		Tasks:          s.taskRegistry,
		Approvals:      s.approvals,
		Skills:         s.skills,
		Wizard:         s.wizardEng,
		Talk:           s.talkState,
		Logger:         s.logger,
		Version:        s.version,
	})
}
