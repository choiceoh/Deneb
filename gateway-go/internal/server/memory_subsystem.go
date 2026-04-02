package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
)

// MemorySubsystem groups the memory/embedding pipeline: the Gemini embedder
// for vector search, Jina API key for cross-encoder reranking, and the
// structured memory store.
// geminiEmbedder and jinaAPIKey are set via Options; memoryStore is late-bound
// during initMemorySubsystem() in the chat pipeline setup.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type MemorySubsystem struct {
	geminiEmbedder *embedding.GeminiEmbedder
	jinaAPIKey     string
	memoryStore    *memory.Store // set during initMemorySubsystem()
}
