package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/infrabind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/platbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcops"
)

// AutonomousSubsystem groups background/periodic services: the autonomous
// execution service, wiki dreamer, and Gmail polling service. All fields
// are late-bound during registerSessionRPCMethods() and
// registerWorkflowSideEffects().
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type AutonomousSubsystem struct {
	autonomousSvc    *domainbind.Service
	wikiDreamer      *domainbind.WikiDreamer // set during initMemorySubsystem()
	gmailPollSvc     *platbind.MailAnalysisService
	roleHealth       *svcbind.Watch // set during registerWorkflowSideEffects()
	modelMaintenance *svcops.Suite

	// agentLogWriter is the shared behavioral event log (the same instance the
	// chat pipeline uses). Promoted to Server so registerWorkflowSideEffects can
	// hand it to the autonomous task loop for background.job events. Set during
	// registerSessionRPCMethods.
	agentLogWriter *infrabind.Writer
}
