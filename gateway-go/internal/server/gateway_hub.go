package server

import "github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"

// buildHub assembles the GatewayHub from the Server's already-initialized fields.
// Called once during server.New() after all subsystems are created.
func (s *Server) buildHub() *rpcutil.GatewayHub {
	return &rpcutil.GatewayHub{
		Broadcaster: s.broadcaster,
		GatewaySubs: s.gatewaySubs,
		Sessions:    s.sessions,
		Processes:   s.processes,
		Telegram:    s.telegramPlug,
		Hooks:       s.hooks,
		Chat:        s.chatHandler,
		Agents:      s.agents,
		JobTracker:  s.jobTracker,
		Cron:        s.cron,
		CronSvc:     s.cronService,
		CronRunLog:  s.cronRunLog,
		Approvals:   s.approvals,
		Nodes:       s.nodes,
		Devices:     s.devices,
		Skills:      s.skills,
		Wizard:      s.wizardEng,
		Talk:        s.talkState,
		Logger:      s.logger,
		Version:     s.version,
		Config:      s.runtimeCfg,
	}
}
