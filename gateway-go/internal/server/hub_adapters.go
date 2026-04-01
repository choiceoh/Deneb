// Hub-to-Deps adapters: thin wiring layer that converts GatewayHub fields
// into handler-specific Deps structs. Keeps existing handler Methods() signatures
// intact (for testability) while eliminating inline Deps assembly in register* methods.
package server

import (
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/agent"
	handlerchannel "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/channel"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/chat"
	handlernode "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/node"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/process"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/skill"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/system"
)

// --- Group A: Hub fields only (Broadcaster → hub.Broadcast) ---

func agentCRUDDepsFromHub(hub *GatewayHub) handleragent.AgentsDeps {
	return handleragent.AgentsDeps{
		Agents:      hub.Agents,
		Broadcaster: hub.Broadcast,
	}
}

func skillDepsFromHub(hub *GatewayHub) handlerskill.Deps {
	return handlerskill.Deps{
		Skills:      hub.Skills,
		Broadcaster: hub.Broadcast,
	}
}

func deviceDepsFromHub(hub *GatewayHub) handlernode.DeviceDeps {
	return handlernode.DeviceDeps{
		Devices:     hub.Devices,
		Broadcaster: hub.Broadcast,
	}
}

func configAdvancedDepsFromHub(hub *GatewayHub) handlersystem.ConfigAdvancedDeps {
	return handlersystem.ConfigAdvancedDeps{
		Broadcaster: hub.Broadcast,
	}
}

func toolDepsFromHub(hub *GatewayHub) handlerskill.ToolDeps {
	return handlerskill.ToolDeps{
		Processes: hub.Processes,
	}
}

func messagingDepsFromHub(hub *GatewayHub) handlerchannel.MessagingDeps {
	return handlerchannel.MessagingDeps{
		TelegramPlugin: hub.Telegram,
	}
}

func channelEventsDepsFromHub(hub *GatewayHub) handlerchannel.EventsDeps {
	return handlerchannel.EventsDeps{
		Broadcaster: hub.Broadcaster,
		Logger:      hub.Logger,
	}
}

// --- Group B: Hub + small local fields ---

func agentExtendedDepsFromHub(hub *GatewayHub) handleragent.ExtendedDeps {
	return handleragent.ExtendedDeps{
		Sessions:       hub.Sessions,
		TelegramPlugin: hub.Telegram,
		GatewaySubs:    hub.GatewaySubs,
		Processes:      hub.Processes,
		Cron:           hub.Cron,
		Hooks:          hub.Hooks,
		Broadcaster:    hub.Broadcaster,
	}
}

func channelLifecycleDepsFromHub(hub *GatewayHub) handlerchannel.LifecycleDeps {
	return handlerchannel.LifecycleDeps{
		TelegramPlugin: hub.Telegram,
		Hooks:          hub.Hooks,
		Broadcaster:    hub.Broadcaster,
	}
}

func cronAdvancedDepsFromHub(hub *GatewayHub) handlerprocess.CronAdvancedDeps {
	return handlerprocess.CronAdvancedDeps{
		Cron:        hub.Cron,
		RunLog:      hub.CronRunLog,
		Broadcaster: hub.Broadcast,
	}
}

func nodeDepsFromHub(hub *GatewayHub, canvasHost string) handlernode.Deps {
	return handlernode.Deps{
		Nodes:       hub.Nodes,
		Broadcaster: hub.Broadcast,
		CanvasHost:  canvasHost,
	}
}

func btwDepsFromHub(hub *GatewayHub) handlerchat.BtwDeps {
	return handlerchat.BtwDeps{
		Chat:        hub.Chat,
		Broadcaster: hub.Broadcast,
	}
}

func execDepsFromHub(hub *GatewayHub) handlersession.ExecDeps {
	return handlersession.ExecDeps{
		Chat:       hub.Chat,
		Agents:     hub.Agents,
		JobTracker: hub.JobTracker,
	}
}

func approvalDepsFromHub(hub *GatewayHub) handlerprocess.ApprovalDeps {
	return handlerprocess.ApprovalDeps{
		Store:       hub.Approvals,
		Broadcaster: hub.Broadcast,
	}
}
