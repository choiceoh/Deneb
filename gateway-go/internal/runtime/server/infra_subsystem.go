package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/maintenance"
)

// InfraSubsystem groups infrastructure services with independent lifecycles
// (currently the maintenance runner).
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type InfraSubsystem struct {
	maintRunner *maintenance.Runner
}

// NewInfraSubsystem creates infrastructure services that can be eagerly initialized.
func NewInfraSubsystem(logger *slog.Logger, denebDir string) *InfraSubsystem {
	return &InfraSubsystem{
		maintRunner: maintenance.NewRunner(denebDir),
	}
}
