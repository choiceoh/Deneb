package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/usage"
)

// WorkflowSubsystem groups agent execution, approval, skill, and workflow
// domain stores. All fields are eagerly initialized and flow into GatewayHub
// for RPC handler wiring.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type WorkflowSubsystem struct {
	approvals    *approval.Store
	skills       *skills.Registry
	jobTracker   *agent.JobTracker
	usageTracker *usage.Tracker
}

// NewWorkflowSubsystem creates all workflow domain stores.
// Every field is initialized eagerly; none require late-binding.
func NewWorkflowSubsystem(logger *slog.Logger) *WorkflowSubsystem {
	return &WorkflowSubsystem{
		approvals:    approval.NewStore(),
		skills:       skills.NewRegistry(),
		jobTracker:   agent.NewJobTracker(logger),
		usageTracker: usage.New(),
	}
}
