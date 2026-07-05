package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/maintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
)

// InfraSubsystem groups infrastructure services with independent lifecycles
// (currently the maintenance runner).
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type InfraSubsystem struct {
	maintRunner *maintenance.Runner

	// marketCache backs both the miniapp 오늘 dashboard card AND the agent's
	// market tool with one keyless Yahoo fetch (10m TTL) — set during
	// registerEarlyMethods(), before chat init consumes it.
	marketCache *market.Cache
}

// NewInfraSubsystem creates infrastructure services that can be eagerly initialized.
func NewInfraSubsystem(logger *slog.Logger, denebDir string) *InfraSubsystem {
	return &InfraSubsystem{
		maintRunner: maintenance.NewRunner(denebDir),
	}
}
