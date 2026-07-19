package embedindex

import "context"

// TextEmbedder is the smallest embedding surface needed by the role helpers.
// Domain packages keep their own health-aware interfaces and can pass them
// here without importing the concrete AI client.
type TextEmbedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// RoleAwareEmbedder is implemented by asymmetric embedding clients that need
// distinct query and passage preprocessing (for example, Nemotron's query:
// and passage: prefixes).
type RoleAwareEmbedder interface {
	EmbedKind(ctx context.Context, kind string, texts []string) ([][]float32, error)
}

// EmbedQueries embeds retrieval queries with the query role when supported.
// Symmetric embedders and existing test fakes retain the plain Embed behavior.
func EmbedQueries(ctx context.Context, embedder TextEmbedder, texts []string) ([][]float32, error) {
	if roleAware, ok := embedder.(RoleAwareEmbedder); ok {
		return roleAware.EmbedKind(ctx, "query", texts)
	}
	return embedder.Embed(ctx, texts)
}
