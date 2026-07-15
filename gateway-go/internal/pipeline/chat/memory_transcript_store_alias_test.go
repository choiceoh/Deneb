package chat

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/transcript"

// MemoryTranscriptStore and NewMemoryTranscriptStore moved to the focused
// transcript package in #3679. These test-only aliases keep package chat's
// existing tests referencing the in-memory transcript store unqualified.
type MemoryTranscriptStore = transcript.MemoryTranscriptStore

func NewMemoryTranscriptStore() *transcript.MemoryTranscriptStore {
	return transcript.NewMemoryTranscriptStore()
}
