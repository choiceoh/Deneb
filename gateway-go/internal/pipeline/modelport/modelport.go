// Package modelport defines stable model configuration and live-control
// contracts shared by runtime model selection and chat execution.
package modelport

// ProviderConfig is the stable provider configuration consumed outside the
// chat implementation. The concrete chat handler aliases this type so runtime
// model management does not need to import the full pipeline package.
type ProviderConfig struct {
	APIKey        string            `json:"apiKey"`
	APIKeyRef     string            `json:"apiKeyRef,omitempty"`
	BaseURL       string            `json:"baseUrl"`
	API           string            `json:"api"`
	Headers       map[string]string `json:"headers,omitempty"`
	ContextWindow int               `json:"contextWindow,omitempty"`
	Reasoning     *bool             `json:"reasoning,omitempty"`
	Vision        *bool             `json:"vision,omitempty"`
	PromptCache   *bool             `json:"promptCache,omitempty"`
	Temperature   *float64          `json:"temperature,omitempty"`
	TopP          *float64          `json:"topP,omitempty"`
	TopK          *int              `json:"topK,omitempty"`
	Routing       *RoutingConfig    `json:"routing,omitempty"`
}

// RoutingConfig is the per-model effort-router override block.
type RoutingConfig struct {
	Enabled           *bool   `json:"enabled,omitempty"`
	ToggleKwarg       *string `json:"toggleKwarg,omitempty"`
	MaxSimpleRunes    *int    `json:"maxSimpleRunes,omitempty"`
	StepCeilingTurn   *int    `json:"stepCeilingTurn,omitempty"`
	ObservationRunes  *int    `json:"observationRunes,omitempty"`
	CumulativeRunes   *int    `json:"cumulativeRunes,omitempty"`
	HeavyHistoryRunes *int    `json:"heavyHistoryRunes,omitempty"`
}

// ModelController is the narrow live-model control surface used by the picker.
type ModelController interface {
	ChatReady() bool
	DefaultModel() string
	SetDefaultModel(string)
	SetProviderConfigs(map[string]ProviderConfig)
}
