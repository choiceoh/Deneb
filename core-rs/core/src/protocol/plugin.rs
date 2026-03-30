//! Plugin lifecycle wire types.
//!
//! Mirrors `gateway-go/pkg/protocol/plugin.go`.

use serde::{Deserialize, Serialize};

/// Plugin kind.
/// Mirrors Go `PluginKind`.
#[derive(Serialize, Deserialize, Debug, Clone, PartialEq, Eq)]
pub enum PluginKind {
    #[serde(rename = "")]
    Unspecified,
    #[serde(rename = "channel")]
    Channel,
    #[serde(rename = "provider")]
    Provider,
    #[serde(rename = "feature")]
    Feature,
}

/// Registered plugin metadata.
/// Mirrors Go `PluginMeta`.
#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct PluginMeta {
    pub id: String,
    pub name: String,
    pub kind: PluginKind,
    pub version: String,
    pub enabled: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,
}

/// Health status of a single plugin.
/// Mirrors Go `PluginHealthStatus`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct PluginHealthStatus {
    pub plugin_id: String,
    pub healthy: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub last_check_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub uptime_ms: Option<i64>,
}

/// Point-in-time view of all registered plugins.
/// Mirrors Go `PluginRegistrySnapshot`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct PluginRegistrySnapshot {
    pub plugins: Vec<PluginMeta>,
    pub health: Vec<PluginHealthStatus>,
    pub snapshot_at: i64,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_plugin_kind_json() {
        assert_eq!(
            serde_json::to_string(&PluginKind::Channel).expect("serialize"),
            "\"channel\""
        );
        assert_eq!(
            serde_json::to_string(&PluginKind::Unspecified).expect("serialize"),
            "\"\""
        );
    }

    #[test]
    fn test_plugin_registry_snapshot_roundtrip() {
        let snapshot = PluginRegistrySnapshot {
            plugins: vec![PluginMeta {
                id: "telegram".into(),
                name: "Telegram".into(),
                kind: PluginKind::Channel,
                version: "1.0.0".into(),
                enabled: true,
                description: Some("Telegram Bot API".into()),
                source: None,
            }],
            health: vec![PluginHealthStatus {
                plugin_id: "telegram".into(),
                healthy: true,
                error: None,
                last_check_at: Some(1700000000),
                uptime_ms: Some(3600000),
            }],
            snapshot_at: 1700000000,
        };
        let json = serde_json::to_string(&snapshot).expect("serialize");
        let parsed: PluginRegistrySnapshot = serde_json::from_str(&json).expect("deserialize");
        assert_eq!(parsed.plugins.len(), 1);
        assert_eq!(parsed.plugins[0].id, "telegram");
        assert!(parsed.health[0].healthy);
    }
}
