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

// NormalizeBindMode maps backwards-compatible IP-form bind values to their
// canonical mode names. Older configs (and humans typing what they want)
// often write "0.0.0.0" for LAN or "127.0.0.1" for loopback; honor those
// as aliases instead of rejecting at validation time. Returns the input
// unchanged when no alias matches.
func NormalizeBindMode(s string) string {
	switch s {
	case "0.0.0.0", "all":
		return BindLAN
	case "127.0.0.1", "localhost":
		return BindLoopback
	}
	return s
}

// Gateway auth modes.
const (
	AuthModeNone         = "none"
	AuthModeToken        = "token"
	AuthModePassword     = "password"
	AuthModeTrustedProxy = "trusted-proxy"
)

// Tailscale modes.
const (
	TailscaleOff    = "off"
	TailscaleServe  = "serve"
	TailscaleFunnel = "funnel"
)

// Config reload modes.
const (
	ReloadOff     = "off"
	ReloadRestart = "restart"
	ReloadHot     = "hot"
	ReloadHybrid  = "hybrid"
)

// Logging formats.
const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

// ── Default values ───────────────────────────────────────────────────────────

const (
	DefaultChannelHealthCheckMinutes    = 5
	DefaultChannelStaleThresholdMinutes = 30
	DefaultChannelMaxRestartsPerHour    = 10
	DefaultReloadDebounceMs             = 300
	DefaultReloadDeferralTimeoutMs      = 300_000
	DefaultAgentMaxConcurrent           = 8
	DefaultSubagentMaxConcurrent        = 2
	DefaultLogRedactSensitive           = "tools"
	DefaultMaxHookTimeoutMs             = 300_000 // 5 minutes
)

// DenebConfig is the top-level configuration object read from deneb.json.
// Only gateway-relevant sections are fully typed; other sections are preserved
// as raw JSON for forwarding to the Node.js Plugin Host bridge.
type DenebConfig struct {
	Meta        *MetaConfig        `json:"meta,omitempty"`
	Gateway     *GatewayConfig     `json:"gateway,omitempty"`
	Logging     *LoggingConfig     `json:"logging,omitempty"`
	Hooks       *HooksConfig       `json:"hooks,omitempty"`
	Session     *SessionConfig     `json:"session,omitempty"`
	Agents      *AgentsConfig      `json:"agents,omitempty"`
	GmailPoll   *GmailPollConfig   `json:"gmailPoll,omitempty"`
	MailLMTP    *MailLMTPConfig    `json:"mailLmtp,omitempty"`
	DropboxPoll *DropboxPollConfig `json:"dropboxPoll,omitempty"`
	Cron        *CronConfig        `json:"cron,omitempty"`
	Topics      *TopicsConfig      `json:"topics,omitempty"`
	// Timezone is an optional IANA zone name (e.g. "Asia/Seoul") used by
	// pkg/dentime for Deneb's zone-aware clock. The DENEB_TIMEZONE env var
	// still wins; an empty or invalid value falls back to server local.
	Timezone string `json:"timezone,omitempty"`
}

// MetaConfig tracks config version metadata.
type MetaConfig struct {
	LastTouchedVersion string `json:"lastTouchedVersion,omitempty"`
	LastTouchedAt      string `json:"lastTouchedAt,omitempty"`
}

// GatewayConfig holds all gateway server settings.
type GatewayConfig struct {
	Port                              *int                    `json:"port,omitempty"`
	Bind                              string                  `json:"bind,omitempty"` // GatewayBindMode
	CustomBindHost                    string                  `json:"customBindHost,omitempty"`
	ControlUI                         *GatewayControlUIConfig `json:"controlUi,omitempty"`
	Auth                              *GatewayAuthConfig      `json:"auth,omitempty"`
	Tailscale                         *GatewayTailscaleConfig `json:"tailscale,omitempty"`
	Reload                            *GatewayReloadConfig    `json:"reload,omitempty"`
	TrustedProxies                    []string                `json:"trustedProxies,omitempty"`
	AllowRealIPFallback               *bool                   `json:"allowRealIpFallback,omitempty"`
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
}

// GatewayAuthConfig configures gateway authentication.
type GatewayAuthConfig struct {
	Mode           string                     `json:"mode,omitempty"` // "none" | "token" | "password" | "trusted-proxy"
	Token          string                     `json:"token,omitempty"`
	Password       string                     `json:"password,omitempty"`
	AllowTailscale *bool                      `json:"allowTailscale,omitempty"`
	TrustedProxy   *GatewayTrustedProxyConfig `json:"trustedProxy,omitempty"`
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

// GatewayReloadConfig for config reload behavior.
type GatewayReloadConfig struct {
	Mode              string `json:"mode,omitempty"` // "off" | "restart" | "hot" | "hybrid"
	DebounceMs        *int   `json:"debounceMs,omitempty"`
	DeferralTimeoutMs *int   `json:"deferralTimeoutMs,omitempty"`
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

// SessionConfig for session lifecycle.
type SessionConfig struct {
	// AutoResume opts the gateway into re-injecting a resume message for
	// sessions whose previous agent run was interrupted by a crash or
	// restart. Default: enabled (nil or unset means true). See
	// internal/runtime/server/auto_resume.go.
	AutoResume *bool `json:"autoResume,omitempty"`
}

// AgentsConfig for agent runtime.
type AgentsConfig struct {
	MaxConcurrent         *int   `json:"maxConcurrent,omitempty"`
	SubagentMaxConcurrent *int   `json:"subagentMaxConcurrent,omitempty"`
	DefaultModel          string `json:"defaultModel,omitempty"`
	// LightweightModel / FallbackModel override the modelrole registry's
	// lightweight and fallback roles (used by gmail-poll, genesis, pilot,
	// and the chat fallback chain). Empty leaves the built-in default
	// (the local vLLM model) in place. Format: "provider/model".
	LightweightModel string                `json:"lightweightModel,omitempty"`
	CodingModel      string                `json:"codingModel,omitempty"`
	FallbackModel    string                `json:"fallbackModel,omitempty"`
	DefaultSystem    string                `json:"defaultSystem,omitempty"`
	Defaults         *AgentsDefaultsConfig `json:"defaults,omitempty"`
	List             []AgentEntryConfig    `json:"list,omitempty"`
}

// AgentsDefaultsConfig holds nested agents.defaults.* fields.
// Model accepts string or {primary, fallbacks} — kept as raw JSON to avoid parse errors.
type AgentsDefaultsConfig struct {
	Model     json.RawMessage         `json:"model,omitempty"`
	Workspace string                  `json:"workspace,omitempty"`
	Thinking  *AgentsThinkingDefaults `json:"thinking,omitempty"`
}

// AgentsThinkingDefaults seeds new sessions with extended-thinking settings
// so operators don't have to toggle them with /think on every fresh session.
//
// Level: one of minimal / low / medium / high / xhigh / adaptive (mapped to
// budget tokens by resolveThinkingConfig). Empty / "off" disables.
// Interleaved: when true, sessions opt into Anthropic's interleaved-thinking
// beta (thinking blocks between tool calls within a turn). Pointer so the
// "unset" state is distinguishable from explicit false.
type AgentsThinkingDefaults struct {
	Level       string `json:"level,omitempty"`
	Interleaved *bool  `json:"interleaved,omitempty"`
}

// AgentEntryConfig represents a single agent in agents.list[].
type AgentEntryConfig struct {
	ID        string `json:"id,omitempty"`
	Default   *bool  `json:"default,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

// CronConfig configures the cron scheduler service.
// When nil or Enabled is nil, the cron service defaults to enabled.
type CronConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// TopicsConfig configures per-topic knowledge injection into the system
// prompt's Static (cached) block. It maps a transport/source topic ID to a
// topic key; the agent then loads <Dir>/<topicKey>.md and injects it for that
// topic's sessions. Unmapped topics get no injection (graceful no-op).
type TopicsConfig struct {
	// Dir holds the <topicKey>.md knowledge files. Relative paths resolve
	// against the agent workspace dir; absolute paths are used as-is. Empty
	// defaults to "topics".
	Dir string `json:"dir,omitempty"`
	// Map maps a source topic ID (as a string) to a topic key. The default
	// native topic uses the "0" key. Older configs may still contain
	// legacy forum/topic thread IDs here.
	// Example: {"42": "coding", "57": "work", "0": "general"}.
	Map map[string]string `json:"map,omitempty"`
}

// GmailPollConfig configures the periodic Gmail polling and analysis service.
type GmailPollConfig struct {
	Enabled     *bool  `json:"enabled,omitempty"`
	IntervalMin *int   `json:"intervalMin,omitempty"` // polling interval in minutes (default 30)
	Query       string `json:"query,omitempty"`       // Gmail search query (default "is:unread newer_than:1h")
	MaxPerCycle *int   `json:"maxPerCycle,omitempty"` // max emails to process per cycle (default 5)
	Model       string `json:"model,omitempty"`       // LLM model for analysis
	PromptFile  string `json:"promptFile,omitempty"`  // path to custom analysis prompt (default ~/.deneb/gmail-analysis-prompt.md)
	// Silent runs the poller as a cache/wiki pre-warmer only: it still
	// analyzes each new mail and fills the Mini App analysis cache + per-message
	// wiki page (so mail-detail opens as a cache hit instead of re-analyzing),
	// but suppresses the proactive chat delivery. Use when another path (the
	// kakao-watch email-single-analysis cron) already delivers the prose
	// analysis to chat and gmailpoll would only duplicate it. Default false.
	Silent *bool `json:"silent,omitempty"`
}

// MailLMTPConfig configures the LMTP (RFC 2033) mail-ingest server. When enabled,
// an on-box mail server (e.g. a Docker mail service running Postfix) PUSHES new
// mail to Deneb over LMTP instead of Deneb polling IMAP — each received message is
// analyzed through the same pipeline as a polled one (cache + per-message wiki +
// proactive 업무 chat). The listener trusts its peer (no SMTP AUTH), so bind it to
// loopback or a unix socket reachable only by the local mail server.
type MailLMTPConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
	// ListenAddr is "host:port" / "tcp:host:port" / "unix:/path.sock".
	// Default "127.0.0.1:10024". For a Docker mail server, bind the host's
	// docker-bridge address (or share a unix socket via a volume) so the
	// container can reach it; never expose it to the public internet.
	ListenAddr string `json:"listenAddr,omitempty"`
}

// DropboxPollConfig configures the periodic Dropbox folder watcher. When
// enabled, new files in FolderPath trigger an agent turn that analyzes them
// (dropbox + wiki tools) and reports to the 업무 chat.
type DropboxPollConfig struct {
	Enabled     *bool  `json:"enabled,omitempty"`
	FolderPath  string `json:"folderPath,omitempty"`  // watched folder (default "/Deneb-Inbox")
	IntervalMin *int   `json:"intervalMin,omitempty"` // poll interval in minutes (default 10)
	MaxPerCycle *int   `json:"maxPerCycle,omitempty"` // max files per cycle (default 10)
}
