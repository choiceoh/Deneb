//! Provider catalog wire types.
//!
//! Mirrors `gateway-go/pkg/protocol/provider.go`.

use serde::{Deserialize, Serialize};

/// Registered model provider metadata.
/// Mirrors Go `ProviderMeta`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct ProviderMeta {
    pub id: String,
    pub label: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub docs_path: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub aliases: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub env_vars: Option<Vec<String>>,
}

/// Authentication method for a provider.
/// Mirrors Go `ProviderAuthMethod`.
#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct ProviderAuthMethod {
    pub id: String,
    pub label: String,
    pub kind: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub hint: Option<String>,
}

/// Single model in the provider catalog.
/// Mirrors Go `ProviderCatalogEntry`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct ProviderCatalogEntry {
    pub provider: String,
    pub model_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub label: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub context_window: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reasoning: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub api_type: Option<String>,
}

/// Point-in-time view of all discovered models.
/// Mirrors Go `ProviderCatalogSnapshot`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct ProviderCatalogSnapshot {
    pub providers: Vec<ProviderMeta>,
    pub entries: Vec<ProviderCatalogEntry>,
    pub snapshot_at: i64,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_provider_meta_roundtrip() {
        let meta = ProviderMeta {
            id: "anthropic".into(),
            label: "Anthropic".into(),
            docs_path: Some("/providers/anthropic".into()),
            aliases: Some(vec!["claude".into()]),
            env_vars: Some(vec!["ANTHROPIC_API_KEY".into()]),
        };
        let json = serde_json::to_string(&meta).expect("serialize");
        let parsed: ProviderMeta = serde_json::from_str(&json).expect("deserialize");
        assert_eq!(parsed.id, "anthropic");
        assert_eq!(parsed.aliases.as_ref().map(|a| a.len()), Some(1));
    }

    #[test]
    fn test_provider_catalog_entry_json_field_names() {
        let entry = ProviderCatalogEntry {
            provider: "anthropic".into(),
            model_id: "claude-opus-4-6".into(),
            label: Some("Claude Opus 4.6".into()),
            context_window: Some(200000),
            reasoning: Some(true),
            api_type: Some("messages".into()),
        };
        let json = serde_json::to_value(&entry).expect("serialize");
        // Verify camelCase matches Go json tags.
        assert_eq!(json["modelId"], "claude-opus-4-6");
        assert_eq!(json["contextWindow"], 200000);
        assert_eq!(json["apiType"], "messages");
    }

    #[test]
    fn test_provider_catalog_snapshot_roundtrip() {
        let snapshot = ProviderCatalogSnapshot {
            providers: vec![ProviderMeta {
                id: "openai".into(),
                label: "OpenAI".into(),
                docs_path: None,
                aliases: None,
                env_vars: None,
            }],
            entries: vec![ProviderCatalogEntry {
                provider: "openai".into(),
                model_id: "gpt-4".into(),
                label: None,
                context_window: Some(128000),
                reasoning: None,
                api_type: None,
            }],
            snapshot_at: 1700000000,
        };
        let json = serde_json::to_string(&snapshot).expect("serialize");
        let parsed: ProviderCatalogSnapshot = serde_json::from_str(&json).expect("deserialize");
        assert_eq!(parsed.providers.len(), 1);
        assert_eq!(parsed.entries[0].model_id, "gpt-4");
    }
}
