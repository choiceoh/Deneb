use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Partial DenebConfig — only the fields the CLI needs.
/// Unknown fields are preserved in `extra` for roundtrip fidelity.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct DenebConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub gateway: Option<GatewayConfig>,

    /// All other fields preserved for roundtrip writes.
    #[serde(flatten)]
    pub extra: HashMap<String, serde_json::Value>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct GatewayConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub port: Option<u16>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub mode: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub bind: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub auth: Option<GatewayAuthConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub remote: Option<GatewayRemoteConfig>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub tls: Option<GatewayTlsConfig>,

    #[serde(flatten)]
    pub extra: HashMap<String, serde_json::Value>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct GatewayAuthConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub token: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub password: Option<String>,

    #[serde(flatten)]
    pub extra: HashMap<String, serde_json::Value>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct GatewayRemoteConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub url: Option<String>,

    #[serde(flatten)]
    pub extra: HashMap<String, serde_json::Value>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct GatewayTlsConfig {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub enabled: Option<bool>,

    #[serde(flatten)]
    pub extra: HashMap<String, serde_json::Value>,
}

impl DenebConfig {
    /// Get the gateway port from config, falling back to the default.
    pub fn gateway_port(&self) -> Option<u16> {
        self.gateway.as_ref().and_then(|g| g.port)
    }

    /// Check if the gateway is in remote mode.
    pub fn is_remote_mode(&self) -> bool {
        self.gateway
            .as_ref()
            .and_then(|g| g.mode.as_deref())
            .map(|m| m == "remote")
            .unwrap_or(false)
    }

    /// Get the remote gateway URL from config.
    pub fn remote_url(&self) -> Option<&str> {
        self.gateway
            .as_ref()
            .and_then(|g| g.remote.as_ref())
            .and_then(|r| r.url.as_deref())
            .filter(|u| !u.trim().is_empty())
    }

    /// Get the auth token from config.
    pub fn auth_token(&self) -> Option<&str> {
        self.gateway
            .as_ref()
            .and_then(|g| g.auth.as_ref())
            .and_then(|a| a.token.as_deref())
            .filter(|t| !t.trim().is_empty())
    }

    /// Check if TLS is enabled.
    pub fn tls_enabled(&self) -> bool {
        self.gateway
            .as_ref()
            .and_then(|g| g.tls.as_ref())
            .and_then(|t| t.enabled)
            .unwrap_or(false)
    }

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
