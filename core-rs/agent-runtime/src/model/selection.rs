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

/// Split trailing auth profile from a model string (e.g. "model@profile").
/// Handles YYYYMMDD@ version suffixes correctly.
/// Mirrors `src/agents/models/model-ref-profile.ts`. Keep in sync.
pub fn split_trailing_auth_profile(raw: &str) -> (String, Option<String>) {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return (String::new(), None);
    }

    let last_slash = trimmed.rfind('/').map(|i| i + 1).unwrap_or(0);
    let mut profile_delimiter = match trimmed[last_slash..].find('@') {
        Some(i) => last_slash + i,
        None => return (trimmed.to_string(), None),
    };
    if profile_delimiter == 0 {
        return (trimmed.to_string(), None);
    }

    // Check if @ is followed by YYYYMMDD (version suffix, not profile).
    let version_suffix = &trimmed[profile_delimiter + 1..];
    if version_suffix.len() >= 8 && version_suffix[..8].chars().all(|c| c.is_ascii_digit()) {
        // Look for another @ after the 8-digit version suffix.
        let next_at = if version_suffix.len() > 8 && version_suffix.as_bytes()[8] == b'@' {
            Some(profile_delimiter + 9)
        } else {
            trimmed[profile_delimiter + 9..].find('@').map(|i| profile_delimiter + 9 + i)
        };
        match next_at {
            Some(pos) => profile_delimiter = pos,
            None => return (trimmed.to_string(), None),
        }
    }

    let model = trimmed[..profile_delimiter].trim();
    let profile = trimmed[profile_delimiter + 1..].trim();
    if model.is_empty() || profile.is_empty() {
        return (trimmed.to_string(), None);
    }

    (model.to_string(), Some(profile.to_string()))
}

/// Resolve a model key from allowlist entry string.
pub fn resolve_allowlist_model_key(raw: &str, default_provider: &str) -> Option<String> {
    let parsed = parse_model_ref(raw, default_provider)?;
    Some(model_key(&parsed.provider, &parsed.model))
}

/// Build set of configured allowlist keys from config model map.
pub fn build_configured_allowlist_keys(
    raw_allowlist: &[String],
    default_provider: &str,
) -> Option<std::collections::HashSet<String>> {
    if raw_allowlist.is_empty() {
        return None;
    }
    let keys: std::collections::HashSet<String> = raw_allowlist
        .iter()
        .filter_map(|raw| resolve_allowlist_model_key(raw, default_provider))
        .collect();
    if keys.is_empty() { None } else { Some(keys) }
}

/// Model alias index mapping aliases to model refs and reverse lookup.
#[derive(Debug, Clone, Default)]
pub struct ModelAliasIndex {
    /// alias (normalized) -> { alias (original), ref }
    pub by_alias: std::collections::HashMap<String, (String, ModelRef)>,
    /// model key -> list of aliases
    pub by_key: std::collections::HashMap<String, Vec<String>>,
}

/// Build a model alias index from config model entries.
/// `models` maps "provider/model" -> optional { alias?: string }.
pub fn build_model_alias_index(
    models: &std::collections::HashMap<String, serde_json::Value>,
    default_provider: &str,
) -> ModelAliasIndex {
    let mut index = ModelAliasIndex::default();
    for (key_raw, entry_raw) in models {
        let parsed = match parse_model_ref(key_raw, default_provider) {
            Some(p) => p,
            None => continue,
        };
        let alias = match entry_raw.get("alias").and_then(|v| v.as_str()) {
            Some(a) => a.trim().to_string(),
            None => continue,
        };
        if alias.is_empty() {
            continue;
        }
        let alias_key = alias.trim().to_lowercase();
        index
            .by_alias
            .insert(alias_key, (alias.clone(), parsed.clone()));
        let key = model_key(&parsed.provider, &parsed.model);
        index.by_key.entry(key).or_default().push(alias);
    }
    index
}

/// Resolve a model ref from a raw string, checking aliases first.
/// Returns (ref, optional alias name).
pub fn resolve_model_ref_from_string(
    raw: &str,
    default_provider: &str,
    alias_index: Option<&ModelAliasIndex>,
) -> Option<(ModelRef, Option<String>)> {
    let (model, _profile) = split_trailing_auth_profile(raw);
    if model.is_empty() {
        return None;
    }
    // Check alias if no slash (bare name).
    if !model.contains('/') {
        if let Some(index) = alias_index {
            let alias_key = model.trim().to_lowercase();
            if let Some((alias_name, model_ref)) = index.by_alias.get(&alias_key) {
                return Some((model_ref.clone(), Some(alias_name.clone())));
            }
        }
    }
    let parsed = parse_model_ref(&model, default_provider)?;
    Some((parsed, None))
}

/// Check if a provider is a CLI provider.
pub fn is_cli_provider(provider: &str, cli_backends: Option<&std::collections::HashMap<String, serde_json::Value>>) -> bool {
    let normalized = normalize_provider_id(provider);
    if normalized == "claude-cli" || normalized == "codex-cli" {
        return true;
    }
    if let Some(backends) = cli_backends {
        return backends.keys().any(|key| normalize_provider_id(key) == normalized);
    }
    false
}

/// Infer a unique provider from configured models for a bare model name.
/// Returns the provider if exactly one provider has this model configured.
pub fn infer_unique_provider_from_configured_models(
    configured_models: &std::collections::HashMap<String, serde_json::Value>,
    model: &str,
    default_provider: &str,
) -> Option<String> {
    let model_trimmed = model.trim();
    if model_trimmed.is_empty() {
        return None;
    }
    let normalized = model_trimmed.to_lowercase();
    let mut providers = std::collections::HashSet::new();
    for key in configured_models.keys() {
        let ref_str = key.trim();
        if ref_str.is_empty() || !ref_str.contains('/') {
            continue;
        }
        let parsed = match parse_model_ref(ref_str, default_provider) {
            Some(p) => p,
            None => continue,
        };
        if parsed.model == model_trimmed || parsed.model.to_lowercase() == normalized {
            providers.insert(parsed.provider);
            if providers.len() > 1 {
                return None;
            }
        }
    }
    if providers.len() == 1 {
        providers.into_iter().next()
    } else {
        None
    }
}

/// Normalize a model selection value (string or { primary?: string } object) to a plain string.
pub fn normalize_model_selection(value: &serde_json::Value) -> Option<String> {
    match value {
        serde_json::Value::String(s) => {
            let trimmed = s.trim();
            if trimmed.is_empty() { None } else { Some(trimmed.to_string()) }
        }
        serde_json::Value::Object(obj) => {
            let primary = obj.get("primary")?.as_str()?;
            let trimmed = primary.trim();
            if trimmed.is_empty() { None } else { Some(trimmed.to_string()) }
        }
        _ => None,
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

    #[test]
    fn normalize_google_model_ids() {
        assert_eq!(normalize_google_model_id("gemini-3-pro"), "gemini-3-pro-preview");
        assert_eq!(normalize_google_model_id("gemini-3-flash"), "gemini-3-flash-preview");
        assert_eq!(normalize_google_model_id("gemini-3.1-flash"), "gemini-3-flash-preview");
        assert_eq!(normalize_google_model_id("gemini-3.1-flash-preview"), "gemini-3-flash-preview");
        assert_eq!(normalize_google_model_id("gemini-3.1-flash-lite"), "gemini-3.1-flash-lite-preview");
        assert_eq!(normalize_google_model_id("unknown-model"), "unknown-model");
    }

    #[test]
    fn split_trailing_auth_profile_basic() {
        let (model, profile) = split_trailing_auth_profile("claude-opus-4-6@myprofile");
        assert_eq!(model, "claude-opus-4-6");
        assert_eq!(profile, Some("myprofile".to_string()));
    }

    #[test]
    fn split_trailing_auth_profile_no_profile() {
        let (model, profile) = split_trailing_auth_profile("claude-opus-4-6");
        assert_eq!(model, "claude-opus-4-6");
        assert!(profile.is_none());
    }

    #[test]
    fn split_trailing_auth_profile_version_suffix() {
        // YYYYMMDD@ is a version suffix, not a profile delimiter.
        let (model, profile) = split_trailing_auth_profile("claude-opus-4-6@20250101");
        assert_eq!(model, "claude-opus-4-6@20250101");
        assert!(profile.is_none());

        // YYYYMMDD@profile: version + profile.
        let (model, profile) = split_trailing_auth_profile("claude-opus-4-6@20250101@myprofile");
        assert_eq!(model, "claude-opus-4-6@20250101");
        assert_eq!(profile, Some("myprofile".to_string()));
    }

    #[test]
    fn split_trailing_auth_profile_with_provider_slash() {
        let (model, profile) = split_trailing_auth_profile("anthropic/claude@prof");
        assert_eq!(model, "anthropic/claude");
        assert_eq!(profile, Some("prof".to_string()));
    }

    #[test]
    fn split_trailing_auth_profile_empty() {
        let (model, profile) = split_trailing_auth_profile("");
        assert_eq!(model, "");
        assert!(profile.is_none());
    }

    #[test]
    fn resolve_allowlist_model_key_basic() {
        assert_eq!(
            resolve_allowlist_model_key("anthropic/claude-opus-4-6", DEFAULT_PROVIDER),
            Some("anthropic/claude-opus-4-6".to_string())
        );
        assert_eq!(
            resolve_allowlist_model_key("claude-opus-4-6", DEFAULT_PROVIDER),
            Some("anthropic/claude-opus-4-6".to_string())
        );
    }

    #[test]
    fn build_configured_allowlist_keys_basic() {
        let keys = vec![
            "anthropic/claude-opus-4-6".to_string(),
            "openai/gpt-4o".to_string(),
        ];
        let result = build_configured_allowlist_keys(&keys, DEFAULT_PROVIDER).unwrap();
        assert!(result.contains("anthropic/claude-opus-4-6"));
        assert!(result.contains("openai/gpt-4o"));
    }

    #[test]
    fn build_configured_allowlist_keys_empty() {
        assert!(build_configured_allowlist_keys(&[], DEFAULT_PROVIDER).is_none());
    }

    #[test]
    fn build_model_alias_index_basic() {
        let mut models = std::collections::HashMap::new();
        models.insert(
            "anthropic/claude-opus-4-6".to_string(),
            serde_json::json!({"alias": "opus"}),
        );
        let index = build_model_alias_index(&models, DEFAULT_PROVIDER);
        assert!(index.by_alias.contains_key("opus"));
        let (alias, model_ref) = index.by_alias.get("opus").unwrap();
        assert_eq!(alias, "opus");
        assert_eq!(model_ref.provider, "anthropic");
        assert_eq!(model_ref.model, "claude-opus-4-6");
    }

    #[test]
    fn resolve_model_ref_from_string_alias() {
        let mut models = std::collections::HashMap::new();
        models.insert(
            "anthropic/claude-opus-4-6".to_string(),
            serde_json::json!({"alias": "opus"}),
        );
        let index = build_model_alias_index(&models, DEFAULT_PROVIDER);
        let (model_ref, alias) =
            resolve_model_ref_from_string("opus", DEFAULT_PROVIDER, Some(&index)).unwrap();
        assert_eq!(model_ref.model, "claude-opus-4-6");
        assert_eq!(alias, Some("opus".to_string()));
    }

    #[test]
    fn resolve_model_ref_from_string_no_alias() {
        let (model_ref, alias) =
            resolve_model_ref_from_string("claude-opus-4-6", DEFAULT_PROVIDER, None).unwrap();
        assert_eq!(model_ref.model, "claude-opus-4-6");
        assert!(alias.is_none());
    }

    #[test]
    fn is_cli_provider_builtin() {
        assert!(is_cli_provider("claude-cli", None));
        assert!(is_cli_provider("codex-cli", None));
        assert!(!is_cli_provider("anthropic", None));
    }

    #[test]
    fn is_cli_provider_custom_backend() {
        let mut backends = std::collections::HashMap::new();
        backends.insert("my-cli".to_string(), serde_json::json!({}));
        assert!(is_cli_provider("my-cli", Some(&backends)));
        assert!(!is_cli_provider("anthropic", Some(&backends)));
    }

    #[test]
    fn infer_unique_provider_basic() {
        let mut models = std::collections::HashMap::new();
        models.insert("openai/gpt-4o".to_string(), serde_json::json!({}));
        assert_eq!(
            infer_unique_provider_from_configured_models(&models, "gpt-4o", DEFAULT_PROVIDER),
            Some("openai".to_string())
        );
    }

    #[test]
    fn infer_unique_provider_ambiguous() {
        let mut models = std::collections::HashMap::new();
        models.insert("openai/gpt-4o".to_string(), serde_json::json!({}));
        models.insert("azure/gpt-4o".to_string(), serde_json::json!({}));
        assert_eq!(
            infer_unique_provider_from_configured_models(&models, "gpt-4o", DEFAULT_PROVIDER),
            None
        );
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
