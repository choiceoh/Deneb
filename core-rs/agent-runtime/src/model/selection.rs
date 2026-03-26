//! Model reference parsing, normalization, and key generation.
//!
//! Mirrors `src/agents/models/model-selection.ts`. Keep in sync.

use serde::{Deserialize, Serialize};

use super::provider_id::normalize_provider_id;

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

/// Generate a canonical model key from provider and model.
/// If the model already starts with "provider/", returns model as-is.
pub fn model_key(provider: &str, model: &str) -> String {
    let provider_id = provider.trim();
    let model_id = model.trim();

    if provider_id.is_empty() {
        return model_id.to_string();
    }
    if model_id.is_empty() {
        return provider_id.to_string();
    }

    // Check if model already contains the provider prefix (case-insensitive).
    let provider_prefix = format!("{}/", provider_id.to_lowercase());
    if model_id.to_lowercase().starts_with(&provider_prefix) {
        model_id.to_string()
    } else {
        format!("{}/{}", provider_id, model_id)
    }
}

/// Generate a legacy model key. Returns None if it would be identical to the
/// canonical key (i.e., the model doesn't already contain the provider prefix).
pub fn legacy_model_key(provider: &str, model: &str) -> Option<String> {
    let provider_id = provider.trim();
    let model_id = model.trim();

    if provider_id.is_empty() || model_id.is_empty() {
        return None;
    }

    let raw_key = format!("{}/{}", provider_id, model_id);
    let canonical_key = model_key(provider_id, model_id);

    if raw_key == canonical_key {
        None
    } else {
        Some(raw_key)
    }
}

/// Normalize Anthropic model ID aliases to canonical form.
fn normalize_anthropic_model_id(model: &str) -> String {
    let trimmed = model.trim();
    if trimmed.is_empty() {
        return trimmed.to_string();
    }

    match trimmed.to_lowercase().as_str() {
        "opus-4.6" => "claude-opus-4-6".to_string(),
        "opus-4.5" => "claude-opus-4-5".to_string(),
        "sonnet-4.6" => "claude-sonnet-4-6".to_string(),
        "sonnet-4.5" => "claude-sonnet-4-5".to_string(),
        _ => trimmed.to_string(),
    }
}

/// Normalize a Google model ID (placeholder — expand as needed).
fn normalize_google_model_id(model: &str) -> String {
    model.trim().to_string()
}

/// Normalize a model ID based on provider-specific rules.
fn normalize_provider_model_id(provider: &str, model: &str) -> String {
    match provider {
        "anthropic" => normalize_anthropic_model_id(model),
        "vercel-ai-gateway" => {
            if !model.contains('/') {
                let normalized = normalize_anthropic_model_id(model);
                if normalized.starts_with("claude-") {
                    return format!("anthropic/{}", normalized);
                }
            }
            model.to_string()
        }
        "google" | "google-vertex" => normalize_google_model_id(model),
        "openrouter" => {
            // OpenRouter-native models need the full "openrouter/<name>" prefix.
            // Models from external providers already contain a slash.
            if !model.contains('/') {
                format!("openrouter/{}", model)
            } else {
                model.to_string()
            }
        }
        _ => model.to_string(),
    }
}

/// Normalize both provider and model ID to canonical forms.
pub fn normalize_model_ref(provider: &str, model: &str) -> ModelRef {
    let normalized_provider = normalize_provider_id(provider);
    let normalized_model = normalize_provider_model_id(&normalized_provider, model.trim());
    ModelRef {
        provider: normalized_provider,
        model: normalized_model,
    }
}

/// Parse a raw model string (e.g. "anthropic/claude-opus-4-6" or "claude-opus-4-6")
/// into a ModelRef. Returns None for empty/invalid input.
pub fn parse_model_ref(raw: &str, default_provider: &str) -> Option<ModelRef> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return None;
    }

    match trimmed.find('/') {
        None => Some(normalize_model_ref(default_provider, trimmed)),
        Some(slash) => {
            let provider_raw = trimmed[..slash].trim();
            let model = trimmed[slash + 1..].trim();
            if provider_raw.is_empty() || model.is_empty() {
                return None;
            }
            Some(normalize_model_ref(provider_raw, model))
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::defaults::DEFAULT_PROVIDER;

    #[test]
    fn model_key_basic() {
        assert_eq!(model_key("anthropic", "claude-opus-4-6"), "anthropic/claude-opus-4-6");
        assert_eq!(model_key("", "claude-opus-4-6"), "claude-opus-4-6");
        assert_eq!(model_key("anthropic", ""), "anthropic");
    }

    #[test]
    fn model_key_avoids_double_prefix() {
        assert_eq!(
            model_key("anthropic", "anthropic/claude-opus-4-6"),
            "anthropic/claude-opus-4-6"
        );
        // Case-insensitive prefix check.
        assert_eq!(
            model_key("Anthropic", "anthropic/claude-opus-4-6"),
            "anthropic/claude-opus-4-6"
        );
    }

    #[test]
    fn legacy_model_key_returns_none_when_identical() {
        assert_eq!(legacy_model_key("anthropic", "claude-opus-4-6"), None);
    }

    #[test]
    fn legacy_model_key_returns_raw_when_different() {
        // When model already has prefix, canonical key = model, raw = "provider/model"
        assert_eq!(
            legacy_model_key("Anthropic", "anthropic/claude-opus-4-6"),
            Some("Anthropic/anthropic/claude-opus-4-6".to_string())
        );
    }

    #[test]
    fn parse_model_ref_with_provider() {
        let result = parse_model_ref("anthropic/claude-opus-4-6", DEFAULT_PROVIDER).unwrap();
        assert_eq!(result.provider, "anthropic");
        assert_eq!(result.model, "claude-opus-4-6");
    }

    #[test]
    fn parse_model_ref_without_provider() {
        let result = parse_model_ref("claude-opus-4-6", DEFAULT_PROVIDER).unwrap();
        assert_eq!(result.provider, "anthropic");
        assert_eq!(result.model, "claude-opus-4-6");
    }

    #[test]
    fn parse_model_ref_empty() {
        assert!(parse_model_ref("", DEFAULT_PROVIDER).is_none());
        assert!(parse_model_ref("  ", DEFAULT_PROVIDER).is_none());
    }

    #[test]
    fn parse_model_ref_invalid_slash() {
        assert!(parse_model_ref("/model", DEFAULT_PROVIDER).is_none());
        assert!(parse_model_ref("provider/", DEFAULT_PROVIDER).is_none());
    }

    #[test]
    fn normalize_anthropic_aliases() {
        let result = normalize_model_ref("anthropic", "opus-4.6");
        assert_eq!(result.model, "claude-opus-4-6");

        let result = normalize_model_ref("anthropic", "sonnet-4.5");
        assert_eq!(result.model, "claude-sonnet-4-5");
    }

    #[test]
    fn normalize_vercel_gateway() {
        let result = normalize_model_ref("vercel-ai-gateway", "opus-4.6");
        assert_eq!(result.model, "anthropic/claude-opus-4-6");
    }

    #[test]
    fn normalize_openrouter_native() {
        let result = normalize_model_ref("openrouter", "aurora-alpha");
        assert_eq!(result.model, "openrouter/aurora-alpha");

        // External provider models pass through.
        let result = normalize_model_ref("openrouter", "anthropic/claude-sonnet-4-5");
        assert_eq!(result.model, "anthropic/claude-sonnet-4-5");
    }

    #[test]
    fn normalize_provider_aliases() {
        let result = normalize_model_ref("bedrock", "some-model");
        assert_eq!(result.provider, "amazon-bedrock");

        let result = normalize_model_ref("z.ai", "some-model");
        assert_eq!(result.provider, "zai");
    }

    #[test]
    fn think_level_parsing() {
        assert_eq!(ThinkLevel::from_str_opt("high"), Some(ThinkLevel::High));
        assert_eq!(ThinkLevel::from_str_opt("ADAPTIVE"), Some(ThinkLevel::Adaptive));
        assert_eq!(ThinkLevel::from_str_opt("invalid"), None);
    }
}
