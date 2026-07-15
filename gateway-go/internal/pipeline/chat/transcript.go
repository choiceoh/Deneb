package chat

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"

// Transcript contracts remain available from chat while persistence
// implementations live in the focused transcript package.
type (
	SearchResult    = toolport.SearchResult
	MatchedMsg      = toolport.MatchedMsg
	TranscriptStore = toolport.TranscriptStore
)
