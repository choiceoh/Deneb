package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/autoresearch"
	"github.com/choiceoh/deneb/gateway-go/internal/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
)

// AutonomousSubsystem groups background/periodic services: the autonomous
// execution service, wiki dreamer, autoresearch runner, and Gmail
// polling service. All fields are late-bound during registerSessionRPCMethods()
// and registerWorkflowSideEffects().
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type AutonomousSubsystem struct {
	autonomousSvc      *autonomous.Service
	wikiDreamer        *wiki.WikiDreamer // set during initMemorySubsystem()
	autoresearchRunner *autoresearch.Runner
	gmailPollSvc       *gmailpoll.Service
}
