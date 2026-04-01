// GatewayHub is the central service registry for the gateway server.
//
// It replaces the scattered point-to-point wiring of 28+ Deps structs with a
// single struct that RPC handlers can reference. This is the "zone controller"
// in the Tesla analogy — instead of each handler (ECU) having its own wiring
// harness, they all connect to the hub.
//
// Usage: built once in server.New(), passed to registerAllMethods() and
// BuildChatPipeline(). Individual handler Methods() functions accept the hub
// (or a subset) instead of bespoke Deps structs.
package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/device"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/node"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/talk"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/wizard"
)

// GatewayHub is the central dependency registry that replaces per-handler Deps
// structs. All commonly shared services live here; domain-specific fields that
// only one handler needs remain as local deps passed alongside the hub.
type GatewayHub struct {
	// Event infrastructure.
	Broadcaster *events.Broadcaster
	GatewaySubs *events.GatewayEventSubscriptions

	// Session and process management.
	Sessions  *session.Manager
	Processes *process.Manager

	// Channel plugins.
	Telegram *telegram.Plugin
	Hooks    *hooks.Registry

	// Agent pipeline.
	Chat       *chat.Handler
	Agents     *agent.Store
	JobTracker *agent.JobTracker

	// Scheduling.
	Cron       *cron.Scheduler
	CronSvc    *cron.Service
	CronRunLog *cron.PersistentRunLog

	// Workflow subsystems.
	Approvals *approval.Store
	Nodes     *node.Manager
	Devices   *device.Manager
	Skills    *skill.Manager
	Wizard    *wizard.Engine
	Talk      *talk.State

	// Metadata.
	Logger  *slog.Logger
	Version string
	Config  *config.GatewayRuntimeConfig
}

// Broadcast sends an event to all connected WebSocket clients.
// Replaces the 15+ broadcastFn closures that were individually created and
// passed to each handler Deps struct.
func (h *GatewayHub) Broadcast(event string, payload any) (int, []error) {
	return h.Broadcaster.Broadcast(event, payload)
}

// buildHub assembles the GatewayHub from the Server's already-initialized fields.
// Called once during server.New() after all subsystems are created.
func (s *Server) buildHub() *GatewayHub {
	return &GatewayHub{
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
