package chat

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	transcriptstore "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/transcript"
)

// Transcript contracts remain available from chat while their persistence
// implementations live in the focused transcript package.
type (
	SearchResult          = toolport.SearchResult
	MatchedMsg            = toolport.MatchedMsg
	TranscriptStore       = toolport.TranscriptStore
	FileTranscriptStore   = transcriptstore.FileTranscriptStore
	MemoryTranscriptStore = transcriptstore.MemoryTranscriptStore
	CachedTranscriptStore = transcriptstore.CachedTranscriptStore
)

// NewFileTranscriptStore creates a JSONL-backed transcript store.
func NewFileTranscriptStore(baseDir string) *FileTranscriptStore {
	return transcriptstore.NewFileTranscriptStore(baseDir)
}

// NewMemoryTranscriptStore creates an in-memory transcript store.
func NewMemoryTranscriptStore() *MemoryTranscriptStore {
	return transcriptstore.NewMemoryTranscriptStore()
}

// NewCachedTranscriptStore adds a TTL cache around a transcript store.
func NewCachedTranscriptStore(inner TranscriptStore, ttl time.Duration) *CachedTranscriptStore {
	return transcriptstore.NewCachedTranscriptStore(inner, ttl)
}
