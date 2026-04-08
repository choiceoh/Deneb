package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
)

// AutonomousSubsystem groups background/periodic services: the autonomous
// execution service, wiki dreamer, and Gmail polling service. All fields
// are late-bound during registerSessionRPCMethods() and
// registerWorkflowSideEffects().
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type AutonomousSubsystem struct {
	autonomousSvc *autonomous.Service
	wikiDreamer   *wiki.WikiDreamer // set during initMemorySubsystem()
	gmailPollSvc  *gmailpoll.Service
}
