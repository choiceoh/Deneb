// Package channel adapter type definitions.
//
// These optional interfaces extend the base Plugin interface using Go's
// interface composition pattern. Channel implementations opt in by
// implementing additional interfaces, checked via type assertion at runtime.
//
// This mirrors the TypeScript ChannelPlugin adapter pattern from
// src/channels/plugins/types.plugin.ts.
package channel

import "context"

// --- Adapter types ---

// OutboundMessage represents a message to be sent through a channel.
type OutboundMessage struct {
	To      string `json:"to"`
	Text    string `json:"text"`
	ReplyTo string `json:"replyTo,omitempty"`
	// Media is a list of media attachment URLs or paths.
	Media []string `json:"media,omitempty"`
}

// ConfigSchema describes a channel's configuration schema.
type ConfigSchema struct {
	Schema  map[string]any          `json:"schema"`
	UIHints map[string]ConfigUIHint `json:"uiHints,omitempty"`
}

// ConfigUIHint provides display hints for config fields.
type ConfigUIHint struct {
	Label       string `json:"label,omitempty"`
	Help        string `json:"help,omitempty"`
	Advanced    bool   `json:"advanced,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

// SetupWizard describes the interactive setup flow for a channel.
type SetupWizard struct {
	Steps []SetupStep `json:"steps"`
}

// SetupStep is a single step in a setup wizard.
type SetupStep struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`
}

// StreamSession represents an active streaming session.
type StreamSession struct {
	SessionKey string `json:"sessionKey"`
	Channel    string `json:"channel"`
	To         string `json:"to"`
}

// --- Optional adapter interfaces ---
// Channel plugins implement these via type assertion: if p, ok := plugin.(ConfigAdapter); ok { ... }

// ConfigAdapter provides channel configuration capabilities.
type ConfigAdapter interface {
	ConfigSchema() ConfigSchema
	ResolveAccount(ctx context.Context) (any, error)
}

// SetupAdapter provides interactive channel setup.
type SetupAdapter interface {
	SetupWizard() SetupWizard
	IsConfigured(ctx context.Context) bool
}

// PairingAdapter provides account pairing for channels that require linking.
type PairingAdapter interface {
	PairAccount(ctx context.Context, token string) error
	UnpairAccount(ctx context.Context) error
	IsPaired(ctx context.Context) bool
}

// SecurityAdapter validates inbound messages from external sources.
type SecurityAdapter interface {
	ValidateInbound(ctx context.Context, payload []byte) bool
}

// MessagingAdapter provides core message send/receive capabilities.
type MessagingAdapter interface {
	SendMessage(ctx context.Context, msg OutboundMessage) error
}

// StreamingAdapter provides streaming message support.
type StreamingAdapter interface {
	SendStream(ctx context.Context, stream StreamSession) error
	SupportsStreaming() bool
}

// LifecycleAdapter provides lifecycle hooks for channel start/stop/reload.
type LifecycleAdapter interface {
	OnStart(ctx context.Context) error
	OnStop(ctx context.Context) error
	OnReload(ctx context.Context, changed []string) error
}

// StatusAdapter provides detailed status reporting.
type StatusAdapter interface {
	DetailedStatus(ctx context.Context) (map[string]any, error)
	Probe(ctx context.Context) error
}

// AllowlistAdapter provides message filtering by sender.
type AllowlistAdapter interface {
	IsAllowed(ctx context.Context, sender string) bool
	AllowList(ctx context.Context) []string
}
