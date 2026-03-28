//! Shared types for model selection.

use serde::{Deserialize, Serialize};

/// A parsed model reference with provider and model ID.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ModelRef {
    pub provider: String,
    pub model: String,
}

/// Valid thinking levels for model configuration.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ThinkLevel {
    Off,
    Minimal,
    Low,
    Medium,
    High,
    Xhigh,
    Adaptive,
}

impl ThinkLevel {
    /// Parse a string into a ThinkLevel, returning None for invalid values.
    pub fn from_str_opt(s: &str) -> Option<Self> {
        match s.trim().to_lowercase().as_str() {
            "off" => Some(Self::Off),
            "minimal" => Some(Self::Minimal),
            "low" => Some(Self::Low),
            "medium" => Some(Self::Medium),
            "high" => Some(Self::High),
            "xhigh" => Some(Self::Xhigh),
            "adaptive" => Some(Self::Adaptive),
            _ => None,
        }
    }
}

/// Model alias index mapping aliases to model refs and reverse lookup.
#[derive(Debug, Clone, Default)]
pub struct ModelAliasIndex {
    /// alias (normalized) -> { alias (original), ref }
    pub by_alias: std::collections::HashMap<String, (String, ModelRef)>,
    /// model key -> list of aliases
    pub by_key: std::collections::HashMap<String, Vec<String>>,
}

/// Catalog entry for thinking level resolution.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ThinkingCatalogEntry {
    pub provider: String,
    pub id: String,
    #[serde(default)]
    pub reasoning: bool,
}

/// Model catalog entry for vision/document/reasoning support checks.
/// Matches TS `ModelCatalogEntry` type from `src/agents/models/model-catalog.ts`.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ModelCatalogEntry {
    pub provider: String,
    pub id: String,
    /// Human-readable name (defaults to id if not provided).
    #[serde(default)]
    pub name: String,
    /// Input modalities supported by this model (e.g., ["text", "image", "document"]).
    pub input: Option<Vec<ModelInputType>>,
    #[serde(default)]
    pub reasoning: bool,
    pub context_window: Option<u64>,
}

/// Model input type classification.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ModelInputType {
    Text,
    Image,
    Document,
}

/// Status of a model ref against allowlist and catalog.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ModelRefStatus {
    pub key: String,
    pub in_catalog: bool,
    pub allow_any: bool,
    pub allowed: bool,
}

/// Result of building an allowed model set.
#[derive(Debug, Clone)]
pub struct AllowedModelSet {
    pub allow_any: bool,
    pub allowed_catalog: Vec<ModelCatalogEntry>,
    pub allowed_keys: std::collections::HashSet<String>,
}

/// Provider config entry for configured model resolution.
#[derive(Debug, Clone, Default, Deserialize)]
pub struct ProviderConfigEntry {
    pub models: Option<Vec<ProviderModelEntry>>,
}

/// Individual model entry within a provider config.
#[derive(Debug, Clone, Default, Deserialize)]
pub struct ProviderModelEntry {
    pub id: Option<String>,
    pub name: Option<String>,
}

/// Parameters for `resolve_allowed_model_ref`.
pub struct ResolveAllowedModelRefParams<'a> {
    pub raw: &'a str,
    pub agents_list: &'a [serde_json::Value],
    pub raw_allowlist: &'a [String],
    pub catalog: &'a [ModelCatalogEntry],
    pub configured_models: &'a std::collections::HashMap<String, serde_json::Value>,
    pub default_provider: &'a str,
    pub default_model: Option<&'a str>,
    pub agents_defaults_model: Option<&'a serde_json::Value>,
}
