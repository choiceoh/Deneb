package rpcutil

import "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil/hub"

// Re-export the service-container types from hub/ so callers keep a stable
// rpcutil import path while the concrete domain fan-out lives in the hub
// subpackage.

type (
	GatewayHub = hub.GatewayHub
	HubConfig  = hub.HubConfig
)

const (
	PhaseInit    = hub.PhaseInit
	PhaseEarly   = hub.PhaseEarly
	PhaseSession = hub.PhaseSession
	PhaseLate    = hub.PhaseLate
)

var NewGatewayHub = hub.NewGatewayHub
