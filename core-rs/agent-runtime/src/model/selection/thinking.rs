//! Thinking level detection and resolution.
//!
//! Mirrors `src/auto-reply/thinking.shared.ts`. Keep in sync.

use super::keys::{legacy_model_key, model_key};
use super::types::{ThinkLevel, ThinkingCatalogEntry};
use crate::model::provider_id::normalize_provider_id;

/// Claude 4.6+ model prefixes that support adaptive thinking.
const CLAUDE_46_PREFIXES: &[&str] = &["claude-opus-4-6", "claude-sonnet-4-6"];

/// Resolve the default thinking level for a model.
/// Mirrors `src/auto-reply/thinking.shared.ts#resolveThinkingDefaultForModel`. Keep in sync.
pub fn resolve_thinking_default_for_model(
    provider: &str,
    model: &str,
    catalog: Option<&[ThinkingCatalogEntry]>,
) -> ThinkLevel {
    let normalized_provider = normalize_provider_id(provider);
    let model_lower = model.to_lowercase();

    // Claude 4.6+ models on Anthropic or Bedrock get adaptive thinking.
    if (normalized_provider == "anthropic" || normalized_provider == "amazon-bedrock")
        && CLAUDE_46_PREFIXES
            .iter()
            .any(|prefix| model_lower.starts_with(prefix))
    {
        return ThinkLevel::Adaptive;
    }

    // Check catalog for models marked with reasoning.
    if let Some(entries) = catalog {
        for entry in entries {
            if normalize_provider_id(&entry.provider) == normalized_provider
                && entry.id.to_lowercase() == model_lower
                && entry.reasoning
            {
                return ThinkLevel::Low;
            }
        }
    }

    ThinkLevel::Off
}

/// Resolve thinking level with per-model config override and global default.
/// Mirrors `src/agents/models/model-selection.ts#resolveThinkingDefault`. Keep in sync.
pub fn resolve_thinking_default(
    provider: &str,
    model: &str,
    configured_models: &std::collections::HashMap<String, serde_json::Value>,
    thinking_default: Option<&str>,
    catalog: Option<&[ThinkingCatalogEntry]>,
) -> ThinkLevel {
    // Check per-model thinking config.
    let canonical_key = model_key(provider, model);
    let legacy_key = legacy_model_key(provider, model);

    let per_model_thinking = configured_models
        .get(&canonical_key)
        .or_else(|| legacy_key.as_ref().and_then(|k| configured_models.get(k)))
        .and_then(|v| v.get("params"))
        .and_then(|v| v.get("thinking"))
        .and_then(|v| v.as_str());

    if let Some(level_str) = per_model_thinking {
        if let Some(level) = ThinkLevel::from_str_opt(level_str) {
            return level;
        }
    }

    // Check global thinking default.
    if let Some(default_str) = thinking_default {
        if let Some(level) = ThinkLevel::from_str_opt(default_str) {
            return level;
        }
    }

    // Fall back to model-based detection.
    resolve_thinking_default_for_model(provider, model, catalog)
}

#[cfg(test)]
mod tests {
    use super::super::types::{ThinkLevel, ThinkingCatalogEntry};
    use super::{resolve_thinking_default, resolve_thinking_default_for_model};

    #[test]
    fn think_level_parsing() {
        assert_eq!(ThinkLevel::from_str_opt("high"), Some(ThinkLevel::High));
        assert_eq!(
            ThinkLevel::from_str_opt("ADAPTIVE"),
            Some(ThinkLevel::Adaptive)
        );
        assert_eq!(ThinkLevel::from_str_opt("invalid"), None);
    }

    #[test]
    fn thinking_default_claude_46_adaptive() {
        assert_eq!(
            resolve_thinking_default_for_model("anthropic", "claude-opus-4-6-20260301", None),
            ThinkLevel::Adaptive
        );
        assert_eq!(
            resolve_thinking_default_for_model("anthropic", "claude-sonnet-4-6", None),
            ThinkLevel::Adaptive
        );
    }

    #[test]
    fn thinking_default_bedrock_adaptive() {
        assert_eq!(
            resolve_thinking_default_for_model("amazon-bedrock", "claude-opus-4-6", None),
            ThinkLevel::Adaptive
        );
    }

    #[test]
    fn thinking_default_non_claude_off() {
        assert_eq!(
            resolve_thinking_default_for_model("openai", "gpt-4o", None),
            ThinkLevel::Off
        );
    }

    #[test]
    fn thinking_default_catalog_reasoning_low() {
        let catalog = vec![ThinkingCatalogEntry {
            provider: "openai".to_string(),
            id: "o3".to_string(),
            reasoning: true,
        }];
        assert_eq!(
            resolve_thinking_default_for_model("openai", "o3", Some(&catalog)),
            ThinkLevel::Low
        );
    }

    #[test]
    fn resolve_thinking_default_per_model_config() {
        let mut models = std::collections::HashMap::new();
        models.insert(
            "anthropic/claude-opus-4-6".to_string(),
            serde_json::json!({"params": {"thinking": "high"}}),
        );
        let result = resolve_thinking_default("anthropic", "claude-opus-4-6", &models, None, None);
        assert_eq!(result, ThinkLevel::High);
    }

    #[test]
    fn resolve_thinking_default_global_override() {
        let models = std::collections::HashMap::new();
        let result = resolve_thinking_default("openai", "gpt-4o", &models, Some("medium"), None);
        assert_eq!(result, ThinkLevel::Medium);
    }
}
