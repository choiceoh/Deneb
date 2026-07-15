package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/pipebind"
)

// GenesisSubsystem groups skill genesis services: the genesis service
// (auto-creation from sessions), usage tracker, skill evolver, and the
// iteration-based Nudger that fires mid-session skill reviews.
// Late-bound during registerWorkflowSideEffects() after the chat handler
// and LLM clients are available.
// Embedded in Server so fields are promoted.
//
// Concrete leaf types (generation/review) are reached through skilllifecycle
// aliases so this composition-root package does not import those leaves.
type GenesisSubsystem struct {
	genesisSvc         *svcbind.GenesisService
	genesisMeta        *svcbind.MetaArtifacts // RSI P1 prompt artifacts (read wiring in initGenesisServices; prod-gated materialize in registerGenesisAutonomousTasks)
	genesisTracker     *svcbind.Tracker
	genesisEvolver     *svcbind.Evolver
	genesisNudger      *svcbind.Nudger
	skillCatalog       *svcbind.Catalog
	genesisTranscripts pipebind.TranscriptStore
}
