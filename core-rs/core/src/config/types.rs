//! Configuration types for Deneb.
//!
//! 1:1 port of `gateway-go/internal/config/types.go`.
//! All types use `Option<T>` for optional fields and `serde(default)` for
//! seamless JSON deserialization.  Unknown fields are silently discarded
//! (the CLI crate uses `#[serde(flatten)]` for roundtrip fidelity; the core
//! crate intentionally does not).

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Top-level configuration object read from deneb.json.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct DenebConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub meta: Option<MetaConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub gateway: Option<GatewayConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub logging: Option<LoggingConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub hooks: Option<HooksConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub canvas_host: Option<CanvasHostConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub media: Option<MediaConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub secrets: Option<SecretsConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub channels: Option<ChannelsConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub session: Option<SessionConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub agents: Option<AgentsConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub gmail_poll: Option<GmailPollConfig>,
}

// ── Meta ─────────────────────────────────────────────────────────────────────

/// Config version metadata.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct MetaConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub last_touched_version: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub last_touched_at: Option<String>,
}

// ── Gateway ──────────────────────────────────────────────────────────────────

/// Gateway server settings.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub port: Option<i32>,

    /// "local" | "remote"
    #[serde(skip_serializing_if = "Option::is_none")]
    pub mode: Option<String>,

    /// Bind mode: "auto" | "lan" | "loopback" | "custom" | "tailnet"
    #[serde(skip_serializing_if = "Option::is_none")]
    pub bind: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub custom_bind_host: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub control_ui: Option<GatewayControlUIConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub auth: Option<GatewayAuthConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub tailscale: Option<GatewayTailscaleConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub remote: Option<GatewayRemoteConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub reload: Option<GatewayReloadConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub tls: Option<GatewayTLSConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub http: Option<GatewayHTTPConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub push: Option<GatewayPushConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub nodes: Option<GatewayNodesConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub trusted_proxies: Option<Vec<String>>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub allow_real_ip_fallback: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub tools: Option<GatewayToolsConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub channel_health_check_minutes: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub channel_stale_event_threshold_minutes: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub channel_max_restarts_per_hour: Option<i32>,
}

/// Control UI serving configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayControlUIConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub enabled: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub base_path: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub root: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub allowed_origins: Option<Vec<String>>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub dangerously_allow_host_header_origin_fallback: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub allow_insecure_auth: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub dangerously_disable_device_auth: Option<bool>,
}

/// Gateway authentication configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayAuthConfig {
    /// "none" | "token" | "password" | "trusted-proxy"
    #[serde(skip_serializing_if = "Option::is_none")]
    pub mode: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub token: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub password: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub allow_tailscale: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub rate_limit: Option<GatewayAuthRateLimitConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub trusted_proxy: Option<GatewayTrustedProxyConfig>,
}

/// Auth rate limiting configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayAuthRateLimitConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_attempts: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub window_ms: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub lockout_ms: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub exempt_loopback: Option<bool>,
}

/// Trusted-proxy auth mode configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayTrustedProxyConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub user_header: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub required_headers: Option<Vec<String>>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub allow_users: Option<Vec<String>>,
}

/// Tailscale serve/funnel mode configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayTailscaleConfig {
    /// "off" | "serve" | "funnel"
    #[serde(skip_serializing_if = "Option::is_none")]
    pub mode: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub reset_on_exit: Option<bool>,
}

/// Remote gateway connection configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayRemoteConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub enabled: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub url: Option<String>,

    /// "ssh" | "direct"
    #[serde(skip_serializing_if = "Option::is_none")]
    pub transport: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub token: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub password: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub tls_fingerprint: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub ssh_target: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub ssh_identity: Option<String>,
}

/// Config reload behavior.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayReloadConfig {
    /// "off" | "restart" | "hot" | "hybrid"
    #[serde(skip_serializing_if = "Option::is_none")]
    pub mode: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub debounce_ms: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub deferral_timeout_ms: Option<i32>,
}

/// TLS termination configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayTLSConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub enabled: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub auto_generate: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub cert_path: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub key_path: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub ca_path: Option<String>,
}

/// HTTP endpoint settings.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayHTTPConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub endpoints: Option<GatewayHTTPEndpointsConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub security_headers: Option<GatewayHTTPSecurityHeadersConfig>,
}

/// HTTP API endpoints configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayHTTPEndpointsConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub chat_completions: Option<GatewayHTTPChatCompletionsConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub responses: Option<GatewayHTTPResponsesConfig>,
}

/// HTTP security headers configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayHTTPSecurityHeadersConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub strict_transport_security: Option<String>,
}

/// `/v1/chat/completions` endpoint configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayHTTPChatCompletionsConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub enabled: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_body_bytes: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_image_parts: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_total_image_bytes: Option<i32>,
}

/// `/v1/responses` endpoint configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayHTTPResponsesConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub enabled: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_body_bytes: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_url_parts: Option<i32>,
}

/// Push notification settings.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayPushConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub apns: Option<GatewayPushAPNSConfig>,
}

/// APNs push relay configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayPushAPNSConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub relay: Option<GatewayPushAPNSRelayConfig>,
}

/// APNs relay settings.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayPushAPNSRelayConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub base_url: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub timeout_ms: Option<i32>,
}

/// Node browser routing configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayNodesConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub browser: Option<GatewayNodesBrowserConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub allow_commands: Option<Vec<String>>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub deny_commands: Option<Vec<String>>,
}

/// Browser routing mode configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayNodesBrowserConfig {
    /// "auto" | "manual" | "off"
    #[serde(skip_serializing_if = "Option::is_none")]
    pub mode: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub node: Option<String>,
}

/// HTTP `/tools/invoke` access control.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GatewayToolsConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub deny: Option<Vec<String>>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub allow: Option<Vec<String>>,
}

// ── Logging ──────────────────────────────────────────────────────────────────

/// Structured logging configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct LoggingConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub level: Option<String>,

    /// "text" (default) or "json"
    #[serde(skip_serializing_if = "Option::is_none")]
    pub format: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub file: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub redact_sensitive: Option<String>,
}

// ── Hooks ────────────────────────────────────────────────────────────────────

/// Gateway hooks configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct HooksConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub token: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub entries: Option<Vec<HookEntry>>,
}

/// A single hook definition.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct HookEntry {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub event: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub command: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub timeout_ms: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub blocking: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub enabled: Option<bool>,
}

// ── Canvas Host ──────────────────────────────────────────────────────────────

/// A2UI canvas hosting configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct CanvasHostConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub enabled: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub root: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub port: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub live_reload: Option<bool>,
}

// ── Media ────────────────────────────────────────────────────────────────────

/// Media handling configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct MediaConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub preserve_filenames: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub ttl_hours: Option<i32>,
}

// ── Secrets ──────────────────────────────────────────────────────────────────

/// Secret storage configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct SecretsConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub defaults: Option<HashMap<String, String>>,
}

// ── Channels ─────────────────────────────────────────────────────────────────

/// Channel-level settings from deneb.json.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct ChannelsConfig {
    /// Maps channel names to model overrides.
    /// Structure: `{"telegram": {"*": "model-id", "chat:123": "other-model"}}`
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model_by_channel: Option<HashMap<String, HashMap<String, String>>>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub default_session_scope: Option<String>,
}

// ── Session ──────────────────────────────────────────────────────────────────

/// Session lifecycle configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct SessionConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub scope: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub dm_scope: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub main_key: Option<String>,
}

// ── Agents ───────────────────────────────────────────────────────────────────

/// Agent runtime configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct AgentsConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_concurrent: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub subagent_max_concurrent: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub default_model: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub default_system: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub defaults: Option<AgentsDefaultsConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub list: Option<Vec<AgentEntryConfig>>,
}

/// Nested `agents.defaults.*` fields.
/// `model` accepts string or `{primary, fallbacks}` -- kept as raw JSON.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct AgentsDefaultsConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model: Option<serde_json::Value>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub workspace: Option<String>,
}

/// A single agent entry in `agents.list[]`.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct AgentEntryConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub default: Option<bool>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub workspace: Option<String>,
}

// ── Gmail Poll ───────────────────────────────────────────────────────────────

/// Periodic Gmail polling and analysis service configuration.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct GmailPollConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub enabled: Option<bool>,

    /// Polling interval in minutes (default 30).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub interval_min: Option<i32>,

    /// Gmail search query (default `is:unread newer_than:1h`).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub query: Option<String>,

    /// Max emails to process per cycle (default 5).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_per_cycle: Option<i32>,

    /// LLM model for analysis.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,

    /// Path to custom analysis prompt.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub prompt_file: Option<String>,
}

// ── Convenience accessors ────────────────────────────────────────────────────

impl DenebConfig {
    /// Get a value at a dot-separated path (e.g. "gateway.port").
    pub fn get_path(&self, path: &str) -> Option<serde_json::Value> {
        let full = serde_json::to_value(self).ok()?;
        let mut current = &full;
        for segment in path.split('.') {
            current = current.get(segment)?;
        }
        Some(current.clone())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deserialize_empty_object() {
        let cfg: DenebConfig = serde_json::from_str("{}").expect("parse");
        assert!(cfg.gateway.is_none());
        assert!(cfg.logging.is_none());
    }

    #[test]
    fn deserialize_gateway_port() {
        let json = r#"{"gateway": {"port": 9999}}"#;
        let cfg: DenebConfig = serde_json::from_str(json).expect("parse");
        assert_eq!(cfg.gateway.as_ref().and_then(|g| g.port), Some(9999));
    }

    #[test]
    fn get_path_traversal() {
        let json = r#"{"gateway": {"port": 12345, "mode": "remote"}}"#;
        let cfg: DenebConfig = serde_json::from_str(json).expect("parse");
        assert_eq!(cfg.get_path("gateway.port"), Some(serde_json::json!(12345)));
        assert_eq!(
            cfg.get_path("gateway.mode"),
            Some(serde_json::json!("remote"))
        );
        assert!(cfg.get_path("nonexistent.key").is_none());
    }

    #[test]
    fn roundtrip_serialize() {
        let json = r#"{"gateway":{"port":8080,"bind":"loopback"}}"#;
        let cfg: DenebConfig = serde_json::from_str(json).expect("parse");
        let out = serde_json::to_string(&cfg).expect("ser");
        let cfg2: DenebConfig = serde_json::from_str(&out).expect("parse2");
        assert_eq!(cfg2.gateway.as_ref().and_then(|g| g.port), Some(8080));
    }

    #[test]
    fn deserialize_full_config() {
        let json = r#"{
            "gateway": {
                "port": 18789,
                "bind": "loopback",
                "auth": {"mode": "token", "token": "abc123"},
                "tailscale": {"mode": "off"},
                "reload": {"mode": "hybrid", "debounceMs": 300}
            },
            "logging": {"level": "info"},
            "session": {"mainKey": "main"},
            "agents": {"maxConcurrent": 8, "list": [{"id": "default", "default": true}]}
        }"#;
        let cfg: DenebConfig = serde_json::from_str(json).expect("parse");
        let gw = cfg.gateway.as_ref().expect("gateway");
        assert_eq!(gw.port, Some(18789));
        assert_eq!(gw.bind.as_deref(), Some("loopback"));
        assert_eq!(
            gw.auth.as_ref().and_then(|a| a.mode.as_deref()),
            Some("token")
        );
        assert_eq!(
            gw.reload.as_ref().and_then(|r| r.debounce_ms),
            Some(300)
        );
        assert_eq!(
            cfg.agents
                .as_ref()
                .and_then(|a| a.list.as_ref())
                .map(|l| l.len()),
            Some(1)
        );
    }
}
