package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/secret"
	"github.com/choiceoh/deneb/gateway-go/internal/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/talk"
	"github.com/choiceoh/deneb/gateway-go/internal/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/wizard"
)

// WorkflowSubsystem groups agent execution, approval, skill, and workflow
// domain stores. All fields are eagerly initialized and flow into GatewayHub
// for RPC handler wiring.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type WorkflowSubsystem struct {
	approvals    *approval.Store
	agents       *agent.Store
	skills       *skill.Manager
	wizardEng    *wizard.Engine
	secrets      *secret.Resolver
	talkState    *talk.State
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
		wizardEng:    wizard.NewEngine(),
		secrets:      secret.NewResolver(),
		talkState:    talk.NewState(),
		jobTracker:   agent.NewJobTracker(logger),
		usageTracker: usage.New(),
	}
}
