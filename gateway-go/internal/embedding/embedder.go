package embedding

import "context"

// Embedder generates L2-normalized embedding vectors from text.
// Implemented by GeminiEmbedder (API) and LocalEmbedder (GGUF FFI).
type Embedder interface {
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}
