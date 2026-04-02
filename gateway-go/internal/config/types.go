// Package config implements Deneb configuration loading, validation, and bootstrap.
//
// This ports the TypeScript config system (src/config/) to Go, enabling the
// Go gateway to read ~/.deneb/deneb.json and resolve gateway-specific settings.
package config

import "encoding/json"

// ── Typed enum constants ─────────────────────────────────────────────────────

// Gateway bind modes.
const (
	BindAuto     = "auto"
	BindLAN      = "lan"
	BindLoopback = "loopback"
	BindCustom   = "custom"
	BindTailnet  = "tailnet"
)

// Gateway auth modes.
const (
	AuthModeNone         = "none"
	AuthModeToken        = "token"
	AuthModePassword     = "password"
	AuthModeTrustedProxy = "trusted-proxy"
)

// Tailscale modes.
const (
	TailscaleOff   = "off"
	TailscaleServe = "serve"
	TailscaleFunnel = "funnel"
)

// Config reload modes.
const (
	ReloadOff     = "off"
	ReloadRestart = "restart"
	ReloadHot     = "hot"
	ReloadHybrid  = "hybrid"
)

// Remote transport modes.
const (
	TransportSSH    = "ssh"
	TransportDirect = "direct"
)

// Browser routing modes.
const (
	BrowserAuto   = "auto"
	BrowserManual = "manual"
	BrowserOff    = "off"
)

// Logging formats.
const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

// ── Default values ───────────────────────────────────────────────────────────

const (
	DefaultChannelHealthCheckMinutes   = 5
	DefaultChannelStaleThresholdMinutes = 30
	DefaultChannelMaxRestartsPerHour   = 10
	DefaultReloadDebounceMs            = 300
	DefaultReloadDeferralTimeoutMs     = 300_000
	DefaultAuthRateLimitMaxAttempts    = 10
	DefaultAuthRateLimitWindowMs       = 60_000
	DefaultAuthRateLimitLockoutMs      = 300_000
	DefaultSessionMainKey              = "main"
	DefaultAgentMaxConcurrent          = 8
	DefaultSubagentMaxConcurrent       = 2
	DefaultLogRedactSensitive          = "tools"
	DefaultMaxHookTimeoutMs            = 300_000 // 5 minutes
)

// DenebConfig is the top-level configuration object read from deneb.json.
// Only gateway-relevant sections are fully typed; other sections are preserved
// as raw JSON for forwarding to the Node.js Plugin Host bridge.
type DenebConfig struct {
	Meta       *MetaConfig       `json:"meta,omitempty"`
	Gateway    *GatewayConfig    `json:"gateway,omitempty"`
	Logging    *LoggingConfig    `json:"logging,omitempty"`
	Hooks      *HooksConfig      `json:"hooks,omitempty"`
	CanvasHost *CanvasHostConfig `json:"canvasHost,omitempty"`
	Media      *MediaConfig      `json:"media,omitempty"`
	Secrets    *SecretsConfig    `json:"secrets,omitempty"`
	Channels   *ChannelsConfig   `json:"channels,omitempty"`
	Session    *SessionConfig    `json:"session,omitempty"`
	Agents     *AgentsConfig     `json:"agents,omitempty"`
	GmailPoll  *GmailPollConfig  `json:"gmailPoll,omitempty"`
}

// MetaConfig tracks config version metadata.
type MetaConfig struct {
	LastTouchedVersion string `json:"lastTouchedVersion,omitempty"`
	LastTouchedAt      string `json:"lastTouchedAt,omitempty"`
}

// GatewayConfig holds all gateway server settings.
type GatewayConfig struct {
	Port                              *int                    `json:"port,omitempty"`
	Mode                              string                  `json:"mode,omitempty"` // "local" | "remote"
	Bind                              string                  `json:"bind,omitempty"` // GatewayBindMode
	CustomBindHost                    string                  `json:"customBindHost,omitempty"`
	ControlUI                         *GatewayControlUIConfig `json:"controlUi,omitempty"`
	Auth                              *GatewayAuthConfig      `json:"auth,omitempty"`
	Tailscale                         *GatewayTailscaleConfig `json:"tailscale,omitempty"`
	Remote                            *GatewayRemoteConfig    `json:"remote,omitempty"`
	Reload                            *GatewayReloadConfig    `json:"reload,omitempty"`
	TLS                               *GatewayTLSConfig       `json:"tls,omitempty"`
	HTTP                              *GatewayHTTPConfig      `json:"http,omitempty"`
	Push                              *GatewayPushConfig      `json:"push,omitempty"`
	Nodes                             *GatewayNodesConfig     `json:"nodes,omitempty"`
	TrustedProxies                    []string                `json:"trustedProxies,omitempty"`
	AllowRealIPFallback               *bool                   `json:"allowRealIpFallback,omitempty"`
	Tools                             *GatewayToolsConfig     `json:"tools,omitempty"`
	ChannelHealthCheckMinutes         *int                    `json:"channelHealthCheckMinutes,omitempty"`
	ChannelStaleEventThresholdMinutes *int                    `json:"channelStaleEventThresholdMinutes,omitempty"`
	ChannelMaxRestartsPerHour         *int                    `json:"channelMaxRestartsPerHour,omitempty"`
}

// GatewayControlUIConfig controls the Control UI serving.
type GatewayControlUIConfig struct {
	Enabled                                  *bool    `json:"enabled,omitempty"`
	BasePath                                 string   `json:"basePath,omitempty"`
	Root                                     string   `json:"root,omitempty"`
	AllowedOrigins                           []string `json:"allowedOrigins,omitempty"`
	DangerouslyAllowHostHeaderOriginFallback *bool    `json:"dangerouslyAllowHostHeaderOriginFallback,omitempty"`
	AllowInsecureAuth                        *bool    `json:"allowInsecureAuth,omitempty"`
	DangerouslyDisableDeviceAuth             *bool    `json:"dangerouslyDisableDeviceAuth,omitempty"`
}

// GatewayAuthConfig configures gateway authentication.
type GatewayAuthConfig struct {
	Mode           string                      `json:"mode,omitempty"` // "none" | "token" | "password" | "trusted-proxy"
	Token          string                      `json:"token,omitempty"`
	Password       string                      `json:"password,omitempty"`
	AllowTailscale *bool                       `json:"allowTailscale,omitempty"`
	RateLimit      *GatewayAuthRateLimitConfig `json:"rateLimit,omitempty"`
	TrustedProxy   *GatewayTrustedProxyConfig  `json:"trustedProxy,omitempty"`
}

// GatewayAuthRateLimitConfig configures auth rate limiting.
type GatewayAuthRateLimitConfig struct {
	MaxAttempts    *int  `json:"maxAttempts,omitempty"`
	WindowMs       *int  `json:"windowMs,omitempty"`
	LockoutMs      *int  `json:"lockoutMs,omitempty"`
	ExemptLoopback *bool `json:"exemptLoopback,omitempty"`
}

// GatewayTrustedProxyConfig for trusted-proxy auth mode.
type GatewayTrustedProxyConfig struct {
	UserHeader      string   `json:"userHeader,omitempty"`
	RequiredHeaders []string `json:"requiredHeaders,omitempty"`
	AllowUsers      []string `json:"allowUsers,omitempty"`
}

// GatewayTailscaleConfig for Tailscale serve/funnel mode.
type GatewayTailscaleConfig struct {
	Mode        string `json:"mode,omitempty"` // "off" | "serve" | "funnel"
	ResetOnExit *bool  `json:"resetOnExit,omitempty"`
}

// GatewayRemoteConfig for remote gateway connections.
type GatewayRemoteConfig struct {
	Enabled        *bool  `json:"enabled,omitempty"`
	URL            string `json:"url,omitempty"`
	Transport      string `json:"transport,omitempty"` // "ssh" | "direct"
	Token          string `json:"token,omitempty"`
	Password       string `json:"password,omitempty"`
	TLSFingerprint string `json:"tlsFingerprint,omitempty"`
	SSHTarget      string `json:"sshTarget,omitempty"`
	SSHIdentity    string `json:"sshIdentity,omitempty"`
}

// GatewayReloadConfig for config reload behavior.
type GatewayReloadConfig struct {
	Mode              string `json:"mode,omitempty"` // "off" | "restart" | "hot" | "hybrid"
	DebounceMs        *int   `json:"debounceMs,omitempty"`
	DeferralTimeoutMs *int   `json:"deferralTimeoutMs,omitempty"`
}

// GatewayTLSConfig for TLS termination.
type GatewayTLSConfig struct {
	Enabled      *bool  `json:"enabled,omitempty"`
	AutoGenerate *bool  `json:"autoGenerate,omitempty"`
	CertPath     string `json:"certPath,omitempty"`
	KeyPath      string `json:"keyPath,omitempty"`
	CAPath       string `json:"caPath,omitempty"`
}

// GatewayHTTPConfig for HTTP endpoint settings.
type GatewayHTTPConfig struct {
	Endpoints       *GatewayHTTPEndpointsConfig       `json:"endpoints,omitempty"`
	SecurityHeaders *GatewayHTTPSecurityHeadersConfig `json:"securityHeaders,omitempty"`
}

// GatewayHTTPEndpointsConfig for HTTP API endpoints.
type GatewayHTTPEndpointsConfig struct {
	ChatCompletions *GatewayHTTPChatCompletionsConfig `json:"chatCompletions,omitempty"`
	Responses       *GatewayHTTPResponsesConfig       `json:"responses,omitempty"`
}

// GatewayHTTPSecurityHeadersConfig for HTTP security headers.
type GatewayHTTPSecurityHeadersConfig struct {
	StrictTransportSecurity *string `json:"strictTransportSecurity,omitempty"`
}

// GatewayHTTPChatCompletionsConfig for /v1/chat/completions endpoint.
type GatewayHTTPChatCompletionsConfig struct {
	Enabled            *bool `json:"enabled,omitempty"`
	MaxBodyBytes       *int  `json:"maxBodyBytes,omitempty"`
	MaxImageParts      *int  `json:"maxImageParts,omitempty"`
	MaxTotalImageBytes *int  `json:"maxTotalImageBytes,omitempty"`
}

// GatewayHTTPResponsesConfig for /v1/responses endpoint.
type GatewayHTTPResponsesConfig struct {
	Enabled      *bool `json:"enabled,omitempty"`
	MaxBodyBytes *int  `json:"maxBodyBytes,omitempty"`
	MaxURLParts  *int  `json:"maxUrlParts,omitempty"`
}

// GatewayPushConfig for push notification settings.
type GatewayPushConfig struct {
	APNS *GatewayPushAPNSConfig `json:"apns,omitempty"`
}

// GatewayPushAPNSConfig for APNs push relay.
type GatewayPushAPNSConfig struct {
	Relay *GatewayPushAPNSRelayConfig `json:"relay,omitempty"`
}

// GatewayPushAPNSRelayConfig for APNs relay settings.
type GatewayPushAPNSRelayConfig struct {
	BaseURL   string `json:"baseUrl,omitempty"`
	TimeoutMs *int   `json:"timeoutMs,omitempty"`
}

// GatewayNodesConfig for node browser routing.
type GatewayNodesConfig struct {
	Browser       *GatewayNodesBrowserConfig `json:"browser,omitempty"`
	AllowCommands []string                   `json:"allowCommands,omitempty"`
	DenyCommands  []string                   `json:"denyCommands,omitempty"`
}

// GatewayNodesBrowserConfig for browser routing mode.
type GatewayNodesBrowserConfig struct {
	Mode string `json:"mode,omitempty"` // "auto" | "manual" | "off"
	Node string `json:"node,omitempty"`
}

// GatewayToolsConfig for HTTP /tools/invoke access control.
type GatewayToolsConfig struct {
	Deny  []string `json:"deny,omitempty"`
	Allow []string `json:"allow,omitempty"`
}

// LoggingConfig for structured logging.
type LoggingConfig struct {
	Level           string `json:"level,omitempty"`
	Format          string `json:"format,omitempty"` // "text" (default) or "json"
	File            string `json:"file,omitempty"`
	RedactSensitive string `json:"redactSensitive,omitempty"`
}

// HooksConfig for gateway hooks.
type HooksConfig struct {
	Token   string      `json:"token,omitempty"`
	Entries []HookEntry `json:"entries,omitempty"`
}

// HookEntry defines a single hook.
type HookEntry struct {
	ID        string `json:"id,omitempty"`
	Event     string `json:"event,omitempty"`
	Command   string `json:"command,omitempty"`
	TimeoutMs *int   `json:"timeoutMs,omitempty"`
	Blocking  *bool  `json:"blocking,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

// CanvasHostConfig for A2UI canvas hosting.
type CanvasHostConfig struct {
	Enabled    *bool  `json:"enabled,omitempty"`
	Root       string `json:"root,omitempty"`
	Port       *int   `json:"port,omitempty"`
	LiveReload *bool  `json:"liveReload,omitempty"`
}

// MediaConfig for media handling.
type MediaConfig struct {
	PreserveFilenames *bool `json:"preserveFilenames,omitempty"`
	TTLHours          *int  `json:"ttlHours,omitempty"`
}

// SecretsConfig for secret storage.
type SecretsConfig struct {
	Defaults map[string]string `json:"defaults,omitempty"`
}

// ChannelsConfig holds channel-level settings from deneb.json.
// Per-channel plugin configs (e.g., Telegram bot token, DM policy) are loaded
// directly by each channel plugin; this struct captures cross-channel settings
// that the gateway core consumes.
type ChannelsConfig struct {
	// ModelByChannel maps channel names to model overrides.
	// Structure: {"telegram": {"*": "model-id", "chat:123": "other-model"}}
	ModelByChannel map[string]map[string]string `json:"modelByChannel,omitempty"`
	// DefaultSessionScope sets the default session scope for all channels.
	DefaultSessionScope string `json:"defaultSessionScope,omitempty"`
}

// SessionConfig for session lifecycle.
type SessionConfig struct {
	Scope   string `json:"scope,omitempty"`
	DMScope string `json:"dmScope,omitempty"`
	MainKey string `json:"mainKey,omitempty"`
}

// AgentsConfig for agent runtime.
type AgentsConfig struct {
	MaxConcurrent         *int                  `json:"maxConcurrent,omitempty"`
	SubagentMaxConcurrent *int                  `json:"subagentMaxConcurrent,omitempty"`
	DefaultModel          string                `json:"defaultModel,omitempty"`
	DefaultSystem         string                `json:"defaultSystem,omitempty"`
	Defaults              *AgentsDefaultsConfig `json:"defaults,omitempty"`
	List                  []AgentEntryConfig    `json:"list,omitempty"`
}

// AgentsDefaultsConfig holds nested agents.defaults.* fields.
// Model accepts string or {primary, fallbacks} — kept as raw JSON to avoid parse errors.
type AgentsDefaultsConfig struct {
	Model     json.RawMessage `json:"model,omitempty"`
	Workspace string          `json:"workspace,omitempty"`
}

// AgentEntryConfig represents a single agent in agents.list[].
type AgentEntryConfig struct {
	ID        string `json:"id,omitempty"`
	Default   *bool  `json:"default,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

// GmailPollConfig configures the periodic Gmail polling and analysis service.
type GmailPollConfig struct {
	Enabled     *bool  `json:"enabled,omitempty"`
	IntervalMin *int   `json:"intervalMin,omitempty"` // polling interval in minutes (default 30)
	Query       string `json:"query,omitempty"`       // Gmail search query (default "is:unread newer_than:1h")
	MaxPerCycle *int   `json:"maxPerCycle,omitempty"` // max emails to process per cycle (default 5)
	Model       string `json:"model,omitempty"`       // LLM model for analysis
	PromptFile  string `json:"promptFile,omitempty"`  // path to custom analysis prompt (default ~/.deneb/gmail-analysis-prompt.md)
}
