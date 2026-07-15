package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
)

// InfraSubsystem groups infrastructure services with independent lifecycles
// (currently the maintenance runner).
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type InfraSubsystem struct {
	maintRunner *domainbind.Runner

	// marketCache backs both the miniapp 오늘 dashboard card AND the agent's
	// market tool with one keyless Yahoo fetch (10m TTL) — set during
	// registerEarlyMethods(), before chat init consumes it.
	marketCache *domainbind.Cache
}

// NewInfraSubsystem creates infrastructure services that can be eagerly initialized.
func NewInfraSubsystem(logger *slog.Logger, denebDir string) *InfraSubsystem {
	return &InfraSubsystem{
		maintRunner: domainbind.NewRunner(denebDir),
	}
}
