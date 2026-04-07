package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/secret"
)

// WorkflowSubsystem groups agent execution, approval, skill, and workflow
// domain stores. All fields are eagerly initialized and flow into GatewayHub
// for RPC handler wiring.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type WorkflowSubsystem struct {
	approvals    *approval.Store
	agents       *agent.Store
	skills       *skill.Manager
	secrets      *secret.Resolver
	jobTracker   *agent.JobTracker
	usageTracker *usage.Tracker
}

// NewWorkflowSubsystem creates all workflow domain stores.
// Every field is initialized eagerly; none require late-binding.
func NewWorkflowSubsystem(logger *slog.Logger) *WorkflowSubsystem {
	return &WorkflowSubsystem{
		approvals:    approval.NewStore(),
		agents:       agent.NewStore(),
		skills:       skill.NewManager(),
		secrets:      secret.NewResolver(),
		jobTracker:   agent.NewJobTracker(logger),
		usageTracker: usage.New(),
	}
}
