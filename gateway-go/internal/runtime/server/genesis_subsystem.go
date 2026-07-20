package server

import (
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle"
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
	genesisSvc         *skilllifecycle.GenesisService
	genesisMeta        *skilllifecycle.MetaArtifacts // RSI P1 prompt artifacts (read wiring in initGenesisServices; prod-gated materialize in registerGenesisAutonomousTasks)
	genesisTracker     *skilllifecycle.Tracker
	genesisEvolver     *skilllifecycle.Evolver
	genesisNudger      *skilllifecycle.Nudger
	skillCatalog       *skilllifecycle.Catalog
	genesisTranscripts toolport.TranscriptStore
	// retryMiner mines failed→successful tool retry pairs from transcripts
	// into tool_retry evidence clusters (lazy: built on first sweep evidence
	// read in selfCodingFailureEvidence).
	retryMiner     *skilllifecycle.RetryCorrectionMiner
	retryMinerOnce sync.Once
}
