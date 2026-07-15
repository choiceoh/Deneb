package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/usage"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/aibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
)

// WorkflowSubsystem groups agent execution, approval, skill, and workflow
// domain stores. All fields are eagerly initialized and flow into GatewayHub
// for RPC handler wiring.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type WorkflowSubsystem struct {
	approvals    *domainbind.ApprovalStore
	skills       *domainbind.Registry
	jobTracker   *aibind.JobTracker
	usageTracker *usage.Tracker
}

// NewWorkflowSubsystem creates all workflow domain stores.
// Every field is initialized eagerly; none require late-binding.
func NewWorkflowSubsystem(logger *slog.Logger) *WorkflowSubsystem {
	return &WorkflowSubsystem{
		approvals:    domainbind.NewApprovalStore(),
		skills:       domainbind.NewRegistry(),
		jobTracker:   aibind.NewJobTracker(logger),
		usageTracker: usage.New(),
	}
}
