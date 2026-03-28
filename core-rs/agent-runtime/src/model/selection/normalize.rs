//! Provider-specific model ID normalization.
//!
//! Mirrors `src/agents/models/model-id-normalization.ts`. Keep in sync.

use crate::model::provider_id::normalize_provider_id;
use super::types::ModelRef;

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

/// Normalize a Google model ID to canonical form.
/// Mirrors `src/agents/models/model-id-normalization.ts`. Keep in sync.
pub fn normalize_google_model_id(model: &str) -> String {
    match model {
        "gemini-3-pro" => "gemini-3-pro-preview".to_string(),
        "gemini-3-flash" => "gemini-3-flash-preview".to_string(),
        "gemini-3.1-pro" => "gemini-3.1-pro-preview".to_string(),
        "gemini-3.1-flash-lite" => "gemini-3.1-flash-lite-preview".to_string(),
        // Compatibility: earlier Deneb docs/config pointed at non-existent IDs.
        "gemini-3.1-flash" | "gemini-3.1-flash-preview" => "gemini-3-flash-preview".to_string(),
        _ => model.to_string(),
    }
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

/// Normalize a model selection value (string or { primary?: string } object) to a plain string.
pub fn normalize_model_selection(value: &serde_json::Value) -> Option<String> {
    match value {
        serde_json::Value::String(s) => {
            let trimmed = s.trim();
            if trimmed.is_empty() {
                None
            } else {
                Some(trimmed.to_string())
            }
        }
        serde_json::Value::Object(obj) => {
            let primary = obj.get("primary")?.as_str()?;
            let trimmed = primary.trim();
            if trimmed.is_empty() {
                None
            } else {
                Some(trimmed.to_string())
            }
        }
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

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
    fn normalize_google_model_ids() {
        assert_eq!(
            normalize_google_model_id("gemini-3-pro"),
            "gemini-3-pro-preview"
        );
        assert_eq!(
            normalize_google_model_id("gemini-3-flash"),
            "gemini-3-flash-preview"
        );
        assert_eq!(
            normalize_google_model_id("gemini-3.1-flash"),
            "gemini-3-flash-preview"
        );
        assert_eq!(
            normalize_google_model_id("gemini-3.1-flash-preview"),
            "gemini-3-flash-preview"
        );
        assert_eq!(
            normalize_google_model_id("gemini-3.1-flash-lite"),
            "gemini-3.1-flash-lite-preview"
        );
        assert_eq!(normalize_google_model_id("unknown-model"), "unknown-model");
    }

    #[test]
    fn normalize_model_selection_string() {
        assert_eq!(
            normalize_model_selection(&serde_json::json!("claude-opus-4-6")),
            Some("claude-opus-4-6".to_string())
        );
    }

    #[test]
    fn normalize_model_selection_object() {
        assert_eq!(
            normalize_model_selection(&serde_json::json!({"primary": "claude-opus-4-6"})),
            Some("claude-opus-4-6".to_string())
        );
    }

    #[test]
    fn normalize_model_selection_empty() {
        assert_eq!(normalize_model_selection(&serde_json::json!("")), None);
        assert_eq!(normalize_model_selection(&serde_json::json!(null)), None);
    }
}
