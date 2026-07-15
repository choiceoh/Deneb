package embedindex

// Identity describes the embedding contract that produced a vector cache.
// Fingerprint should change whenever the model or its output semantics change;
// Dimensions is retained separately so operators can diagnose shape drift.
type Identity struct {
	Fingerprint string
	Dimensions  int
}

// identityProvider is optional so existing test embedders and degraded
// deployments remain valid. Production embedding.Client implements it.
type identityProvider interface {
	EmbeddingFingerprint() string
	EmbeddingDimensions() int
}

// IdentityOf returns the active embedder identity when it is known. A zero
// identity means the provider has not completed its health/model probe yet;
// callers should keep the cache provisionally and retry validation later.
func IdentityOf(embedder any) Identity {
	provider, ok := embedder.(identityProvider)
	if !ok || provider == nil {
		return Identity{}
	}
	return Identity{
		Fingerprint: provider.EmbeddingFingerprint(),
		Dimensions:  provider.EmbeddingDimensions(),
	}
}
