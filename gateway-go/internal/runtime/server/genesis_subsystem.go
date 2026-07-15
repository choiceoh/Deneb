package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	skillcore "github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle/core"
)

// GenesisSubsystem groups skill genesis services: the genesis service
// (auto-creation from sessions), usage tracker, skill evolver, and the
// iteration-based Nudger that fires mid-session skill reviews.
// Late-bound during registerWorkflowSideEffects() after the chat handler
// and LLM clients are available.
// Embedded in Server so fields are promoted.
//
// Concrete leaf types (generation/review) are reached through skilllifecycle/core
// aliases so this composition-root package does not import those leaves.
type GenesisSubsystem struct {
	genesisSvc         *skillcore.GenesisService
	genesisMeta        *skillcore.MetaArtifacts // RSI P1 prompt artifacts (read wiring in initGenesisServices; prod-gated materialize in registerGenesisAutonomousTasks)
	genesisTracker     *skillcore.Tracker
	genesisEvolver     *skillcore.Evolver
	genesisNudger      *skillcore.Nudger
	skillCatalog       *skillcore.Catalog
	genesisTranscripts toolport.TranscriptStore
}
