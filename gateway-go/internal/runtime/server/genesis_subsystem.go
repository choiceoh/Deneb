package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// GenesisSubsystem groups skill genesis services: the genesis service
// (auto-creation from sessions), usage tracker, skill evolver, and the
// iteration-based Nudger that fires mid-session skill reviews.
// Late-bound during registerWorkflowSideEffects() after the chat handler
// and LLM clients are available.
// Embedded in Server so fields are promoted.
type GenesisSubsystem struct {
	genesisSvc         *genesis.Service
	genesisTracker     *genesis.Tracker
	genesisEvolver     *genesis.Evolver
	genesisNudger      *genesis.Nudger
	skillCatalog       *skills.Catalog
	genesisTranscripts toolctx.TranscriptStore
}
