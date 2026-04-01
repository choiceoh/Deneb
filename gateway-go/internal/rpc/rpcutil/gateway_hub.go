// GatewayHub is the central service registry for the gateway server.
//
// Tesla analogy: instead of each handler (ECU) having its own wiring harness
// via bespoke Deps structs, they all connect to the hub (zone controller).
// The hub is built once in server.New() and passed to method registration.
// Individual handler Methods() still accept their own Deps structs for
// testability; the registry assembles Deps inline from hub fields.
package rpcutil

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
// Satisfies BroadcastFunc signature for direct use in handler Deps.
func (h *GatewayHub) Broadcast(event string, payload any) (int, []error) {
	return h.Broadcaster.Broadcast(event, payload)
}
