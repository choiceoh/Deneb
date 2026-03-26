package plugin

import (
	"sync"
)

// ProviderModel describes an LLM model offered by a provider.
type ProviderModel struct {
	ID               string   `json:"id"`
	Label            string   `json:"label,omitempty"`
	Aliases          []string `json:"aliases,omitempty"`
	ContextTokens    int      `json:"contextTokens,omitempty"`
	MaxOutputTokens  int      `json:"maxOutputTokens,omitempty"`
	ReasoningCapable bool     `json:"reasoningCapable,omitempty"`
	Deprecated       bool     `json:"deprecated,omitempty"`
}

// ProviderAuth holds authentication credentials for a provider.
type ProviderAuth struct {
	Type      string `json:"type"` // "api_key", "oauth", "token"
	APIKey    string `json:"apiKey,omitempty"`
	Token     string `json:"token,omitempty"`
	BaseURL   string `json:"baseUrl,omitempty"`
	OrgID     string `json:"orgId,omitempty"`
	ProfileID string `json:"profileId,omitempty"`
}

// ProviderCapabilities describes what a provider supports.
type ProviderCapabilities struct {
	Chat            bool `json:"chat"`
	Completion      bool `json:"completion,omitempty"`
	Embedding       bool `json:"embedding,omitempty"`
	ImageGeneration bool `json:"imageGeneration,omitempty"`
	TTS             bool `json:"tts,omitempty"`
	STT             bool `json:"stt,omitempty"`
	WebSearch       bool `json:"webSearch,omitempty"`
	Streaming       bool `json:"streaming,omitempty"`
	ToolUse         bool `json:"toolUse,omitempty"`
	Vision          bool `json:"vision,omitempty"`
	Thinking        bool `json:"thinking,omitempty"`
}

// ProviderRuntime holds the resolved runtime configuration for a provider.
type ProviderRuntime struct {
	Config       ProviderConfig
	Auth         ProviderAuth
	Models       []ProviderModel
	Capabilities ProviderCapabilities
}

// ProviderResolver manages runtime provider resolution with caching.
type ProviderResolver struct {
	mu       sync.RWMutex
	catalog  *ProviderCatalog
	runtimes map[string]*ProviderRuntime
}

// NewProviderResolver creates a new provider resolver.
func NewProviderResolver(catalog *ProviderCatalog) *ProviderResolver {
	return &ProviderResolver{
		catalog:  catalog,
		runtimes: make(map[string]*ProviderRuntime),
	}
}

// Resolve returns the runtime configuration for a provider.
func (r *ProviderResolver) Resolve(id string) *ProviderRuntime {
	r.mu.RLock()
	runtime, ok := r.runtimes[id]
	r.mu.RUnlock()
	if ok {
		return runtime
	}

	cfg := r.catalog.Get(id)
	if cfg == nil {
		return nil
	}

	runtime = &ProviderRuntime{
		Config: *cfg,
		Auth: ProviderAuth{
			Type:    "api_key",
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
		},
	}

	r.mu.Lock()
	r.runtimes[id] = runtime
	r.mu.Unlock()
	return runtime
}

// Invalidate clears the cached runtime for a provider.
func (r *ProviderResolver) Invalidate(id string) {
	r.mu.Lock()
	delete(r.runtimes, id)
	r.mu.Unlock()
}

// InvalidateAll clears all cached runtimes.
func (r *ProviderResolver) InvalidateAll() {
	r.mu.Lock()
	r.runtimes = make(map[string]*ProviderRuntime)
	r.mu.Unlock()
}

// ResolveDefaultModel returns the default model for a provider.
func (r *ProviderResolver) ResolveDefaultModel(providerID string) *ProviderModel {
	runtime := r.Resolve(providerID)
	if runtime == nil || len(runtime.Models) == 0 {
		return nil
	}
	return &runtime.Models[0]
}

// FindModel finds a model by ID across all providers.
func (r *ProviderResolver) FindModel(providerID, modelID string) *ProviderModel {
	runtime := r.Resolve(providerID)
	if runtime == nil {
		return nil
	}
	for _, m := range runtime.Models {
		if m.ID == modelID {
			return &m
		}
	}
	return nil
}
