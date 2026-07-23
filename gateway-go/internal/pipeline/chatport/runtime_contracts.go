package chatport

import (
	"context"
	"time"
)

// InteractiveTurnDeadline is the transport-level backstop shared by native
// streaming, blocking fallback/capture turns, and graceful restart draining.
// The chat pipeline's own 5-minute budget should normally fire first.
const InteractiveTurnDeadline = 6 * time.Minute

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

// DeliveryContext carries channel routing for a chat run without exposing
// channel implementations.
type DeliveryContext struct {
	Channel    string `json:"channel,omitempty"`
	To         string `json:"to,omitempty"`
	AccountID  string `json:"accountId,omitempty"`
	ThreadID   string `json:"threadId,omitempty"`
	MessageID  string `json:"messageId,omitempty"`
	DraftMsgID string `json:"draftMsgId,omitempty"`
}

// ToolStreamEvent is one tool lifecycle transition emitted by a streaming run.
type ToolStreamEvent struct {
	State     string
	Tool      string
	ToolUseID string
	Detail    string
	IsError   bool
}

// SyncRequest is the runtime-safe subset of synchronous chat options. Rich API
// callers that need prebuilt LLM messages or provider-specific sampling keep
// using chat.SyncOptions at the composition boundary.
type SyncRequest struct {
	SessionKey string
	Message    string
	Model      string

	MaxTokens           *int
	MaxTurns            *int
	MaxToolCallAttempts *int
	SystemPrompt        string
	Thinking            string
	ToolPreset          string
	MaxHistoryTokens    int
	Delivery            *DeliveryContext

	EphemeralUser       bool
	EphemeralAssistant  bool
	AutoDeliveredOutput bool
	SkipRecall          bool
	FeedContext         string
	GateUntrustedTools  bool

	BeforeToolCall func(name, toolCallID string, input []byte) (block bool, blockReason string)
	OnToolResult   func(name, toolUseID, result string, isErr bool)
	OnToolEvent    func(ToolStreamEvent)
	OnThinking     func(preview string)
	OnReasoning    func(full string)
}

// SyncResult is a transport-neutral snapshot of a completed chat run.
type SyncResult struct {
	Text            string
	AllText         string
	DeliverableText string
	BestText        string
	Model           string
	ProviderModel   string
	FellBack        bool
	InputTokens     int
	OutputTokens    int
	Turns           int
	StopReason      string
	// Thinking is the accumulated reasoning/chain-of-thought for the turn (empty
	// when the model produced none). Surfaced to clients as the expandable
	// reasoning block; never re-fed into the model context.
	Thinking string
}

// SyncRunner is the stable runtime-facing chat execution boundary.
type SyncRunner interface {
	// ChatReady is nil-safe on the concrete handler and prevents a typed nil
	// pointer hidden in this interface from being treated as usable.
	ChatReady() bool
	RunSync(context.Context, SyncRequest) (*SyncResult, error)
}

// SyncStreamRunner is the narrower streaming boundary used by native SSE.
// Keeping it separate means non-streaming consumers and test doubles do not
// need to implement a method they never call.
type SyncStreamRunner interface {
	ChatReady() bool
	RunSyncStream(context.Context, SyncRequest, func(string)) (*SyncResult, error)
}

// ModelController is the narrow live-model control surface used by the picker.
type ModelController interface {
	ChatReady() bool
	DefaultModel() string
	SetDefaultModel(string)
	SetProviderConfigs(map[string]ProviderConfig)
}
