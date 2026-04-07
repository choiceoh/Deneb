package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
)

// GenesisSubsystem groups skill genesis services: the genesis service
// (auto-creation from sessions), usage tracker, and skill evolver.
// Late-bound during registerWorkflowSideEffects() after the chat handler
// and LLM clients are available.
// Embedded in Server so fields are promoted.
type GenesisSubsystem struct {
	genesisSvc     *genesis.Service
	genesisTracker *genesis.Tracker
	genesisEvolver *genesis.Evolver
}
