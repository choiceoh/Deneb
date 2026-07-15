package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/modelmaintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rolehealth"
)

// AutonomousSubsystem groups background/periodic services: the autonomous
// execution service, wiki dreamer, and Gmail polling service. All fields
// are late-bound during registerSessionRPCMethods() and
// registerWorkflowSideEffects().
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type AutonomousSubsystem struct {
	autonomousSvc    *autonomous.Service
	wikiDreamer      *wiki.WikiDreamer // set during initMemorySubsystem()
	gmailPollSvc     *mailanalysis.Service
	roleHealth       *rolehealth.Watch // set during registerWorkflowSideEffects()
	modelMaintenance *modelmaintenance.Suite

	// agentLogWriter is the shared behavioral event log (the same instance the
	// chat pipeline uses). Promoted to Server so registerWorkflowSideEffects can
	// hand it to the autonomous task loop for background.job events. Set during
	// registerSessionRPCMethods.
	agentLogWriter *agentlog.Writer
}
