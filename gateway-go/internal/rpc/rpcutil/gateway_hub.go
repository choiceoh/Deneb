// GatewayHub is the central service container for the gateway server.
//
// It holds references to all shared services and stores. No business logic
// lives here — only Broadcast() (fan-out helper) and Validate() (startup check).
// Built once in server.New() via buildHub(), passed to method registration.
// Handler packages never import this type; they receive Deps structs instead.
package rpcutil

import (
	"fmt"
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

type GatewayHub struct {
	// Event infrastructure.
	Broadcaster *events.Broadcaster
	GatewaySubs *events.GatewayEventSubscriptions

	// Session and process management.
	Sessions  *session.Manager
	Processes *process.Manager

	// Channel plugins.
	Telegram      *telegram.Plugin        // nil until registerEarlyMethods creates it from config.
	Hooks         *hooks.Registry
	InternalHooks *hooks.InternalRegistry // programmatic hook handlers (nil-safe)

	// Agent pipeline.
	Chat       *chat.Handler    // nil until registerSessionRPCMethods (late-phase only).
	Agents     *agent.Store
	JobTracker *agent.JobTracker

	// Scheduling.
	Cron          *cron.Scheduler
	CronService   *cron.Service
	CronPersistLog *cron.PersistentRunLog

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

// Validate checks that all required hub fields are non-nil.
// Called once at startup before method registration begins.
func (h *GatewayHub) Validate() error {
	var missing []string
	if h.Broadcaster == nil {
		missing = append(missing, "Broadcaster")
	}
	if h.GatewaySubs == nil {
		missing = append(missing, "GatewaySubs")
	}
	if h.Sessions == nil {
		missing = append(missing, "Sessions")
	}
	if h.Processes == nil {
		missing = append(missing, "Processes")
	}
	if h.Hooks == nil {
		missing = append(missing, "Hooks")
	}
	if h.Agents == nil {
		missing = append(missing, "Agents")
	}
	if h.Cron == nil {
		missing = append(missing, "Cron")
	}
	if h.Approvals == nil {
		missing = append(missing, "Approvals")
	}
	if h.Nodes == nil {
		missing = append(missing, "Nodes")
	}
	if h.Devices == nil {
		missing = append(missing, "Devices")
	}
	if h.Skills == nil {
		missing = append(missing, "Skills")
	}
	if h.Logger == nil {
		missing = append(missing, "Logger")
	}
	if len(missing) > 0 {
		return fmt.Errorf("GatewayHub missing required fields: %v", missing)
	}
	return nil
}
