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
///
/// Model strings are ASCII by convention (provider/model IDs, date suffixes).
/// Byte indexing is safe because all delimiters (`@`, `/`) are single-byte ASCII.
pub fn split_trailing_auth_profile(raw: &str) -> (String, Option<String>) {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return (String::new(), None);
    }

    let last_slash = trimmed.rfind('/').map(|i| i + 1).unwrap_or(0);
    let after_slash = &trimmed[last_slash..];
    let mut profile_delimiter = match after_slash.find('@') {
        Some(i) => last_slash + i,
        None => return (trimmed.to_string(), None),
    };
    if profile_delimiter == 0 {
        return (trimmed.to_string(), None);
    }

    // Check if @ is followed by YYYYMMDD (version suffix, not profile delimiter).
    let after_at = &trimmed[profile_delimiter + 1..];
    let is_version_suffix = after_at.len() >= 8
        && after_at.as_bytes()[..8].iter().all(|b| b.is_ascii_digit());

    if is_version_suffix {
        // Look for another @ after the 8-digit version suffix.
        let next_at = if after_at.len() > 8 && after_at.as_bytes()[8] == b'@' {
            // YYYYMMDD@ immediately followed by profile.
            Some(profile_delimiter + 9)
        } else {
            // Search further for @.
            after_at.get(9..).and_then(|rest| rest.find('@').map(|i| profile_delimiter + 9 + i))
        };
        match next_at {
            Some(pos) => profile_delimiter = pos,
            None => return (trimmed.to_string(), None),
        }
    }

    // Guard: verify char boundaries (model strings are ASCII but be safe).
    if !trimmed.is_char_boundary(profile_delimiter)
        || !trimmed.is_char_boundary(profile_delimiter + 1)
    {
        return (trimmed.to_string(), None);
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

/// Catalog entry for thinking level resolution.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ThinkingCatalogEntry {
    pub provider: String,
    pub id: String,
    #[serde(default)]
    pub reasoning: bool,
}

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
    if normalized_provider == "anthropic" || normalized_provider == "amazon-bedrock" {
        if CLAUDE_46_PREFIXES.iter().any(|prefix| model_lower.starts_with(prefix)) {
            return ThinkLevel::Adaptive;
        }
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

/// Check if a model supports vision (image input) based on catalog lookup.
pub fn model_supports_vision(entry: Option<&ModelCatalogEntry>) -> bool {
    entry
        .and_then(|e| e.input.as_ref())
        .map(|inputs| inputs.contains(&ModelInputType::Image))
        .unwrap_or(false)
}

/// Check if a model supports document input based on catalog lookup.
pub fn model_supports_document(entry: Option<&ModelCatalogEntry>) -> bool {
    entry
        .and_then(|e| e.input.as_ref())
        .map(|inputs| inputs.contains(&ModelInputType::Document))
        .unwrap_or(false)
}

/// Find a model in the catalog by provider and model ID.
pub fn find_model_in_catalog<'a>(
    catalog: &'a [ModelCatalogEntry],
    provider: &str,
    model: &str,
) -> Option<&'a ModelCatalogEntry> {
    let p = normalize_provider_id(provider);
    let m = model.to_lowercase();
    catalog.iter().find(|e| {
        normalize_provider_id(&e.provider) == p && e.id.to_lowercase() == m
    })
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

/// Get the status of a model ref against the allowlist and catalog.
pub fn get_model_ref_status(
    model_ref: &ModelRef,
    catalog: &[ModelCatalogEntry],
    allowed_keys: Option<&std::collections::HashSet<String>>,
) -> ModelRefStatus {
    let key = model_key(&model_ref.provider, &model_ref.model);
    let in_catalog = find_model_in_catalog(catalog, &model_ref.provider, &model_ref.model).is_some();
    let allow_any = allowed_keys.is_none();
    let allowed = allow_any
        || allowed_keys
            .map(|keys| keys.contains(&key))
            .unwrap_or(false);
    ModelRefStatus {
        key,
        in_catalog,
        allow_any,
        allowed,
    }
}

/// Resolve the reasoning default for a model ("on" if catalog marks it as reasoning).
pub fn resolve_reasoning_default(
    catalog: &[ModelCatalogEntry],
    provider: &str,
    model: &str,
) -> bool {
    find_model_in_catalog(catalog, provider, model)
        .map(|e| e.reasoning)
        .unwrap_or(false)
}

/// Extract the primary model value from a config model entry (string or {primary}).
/// Mirrors `src/config/model-input.ts#resolveAgentModelPrimaryValue`. Keep in sync.
pub fn resolve_agent_model_primary_value(model: Option<&serde_json::Value>) -> Option<String> {
    normalize_model_selection(model?)
}

/// Convert a model config value to a list-like structure { primary?, fallbacks? }.
/// Mirrors `src/config/model-input.ts#toAgentModelListLike`. Keep in sync.
pub fn to_agent_model_list_like(model: Option<&serde_json::Value>) -> Option<serde_json::Value> {
    let val = model?;
    match val {
        serde_json::Value::String(s) => {
            let primary = s.trim();
            if primary.is_empty() {
                None
            } else {
                Some(serde_json::json!({ "primary": primary }))
            }
        }
        serde_json::Value::Object(_) => Some(val.clone()),
        _ => None,
    }
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

/// Resolve the configured model ref from config, with alias resolution and provider fallback.
/// Mirrors `src/agents/models/model-selection.ts#resolveConfiguredModelRef`. Keep in sync.
pub fn resolve_configured_model_ref(
    agents_defaults_model: Option<&serde_json::Value>,
    configured_models: &std::collections::HashMap<String, serde_json::Value>,
    configured_providers: Option<&std::collections::HashMap<String, serde_json::Value>>,
    default_provider: &str,
    default_model: &str,
) -> ModelRef {
    let raw_model = resolve_agent_model_primary_value(agents_defaults_model).unwrap_or_default();
    if !raw_model.is_empty() {
        let trimmed = raw_model.trim();
        let alias_index = build_model_alias_index(configured_models, default_provider);

        if !trimmed.contains('/') {
            let alias_key = trimmed.to_lowercase();
            if let Some((_alias, model_ref)) = alias_index.by_alias.get(&alias_key) {
                return model_ref.clone();
            }
            // No alias match; default to anthropic for bare model names.
            return ModelRef {
                provider: "anthropic".to_string(),
                model: trimmed.to_string(),
            };
        }

        if let Some((model_ref, _)) =
            resolve_model_ref_from_string(trimmed, default_provider, Some(&alias_index))
        {
            return model_ref;
        }
    }

    // Before falling back to hardcoded default, check configured providers.
    if let Some(providers) = configured_providers {
        let has_default = providers.contains_key(default_provider);
        if !has_default {
            for (provider_name, provider_cfg) in providers {
                if let Ok(entry) =
                    serde_json::from_value::<ProviderConfigEntry>(provider_cfg.clone())
                {
                    if let Some(models) = &entry.models {
                        if let Some(first) = models.first() {
                            if let Some(id) = &first.id {
                                if !id.is_empty() {
                                    return ModelRef {
                                        provider: provider_name.clone(),
                                        model: id.clone(),
                                    };
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    ModelRef {
        provider: default_provider.to_string(),
        model: default_model.to_string(),
    }
}

/// Resolve the default model for a specific agent, considering agent-level overrides.
/// Mirrors `src/agents/models/model-selection.ts#resolveDefaultModelForAgent`. Keep in sync.
pub fn resolve_default_model_for_agent(
    agents_list: &[serde_json::Value],
    agents_defaults_model: Option<&serde_json::Value>,
    configured_models: &std::collections::HashMap<String, serde_json::Value>,
    configured_providers: Option<&std::collections::HashMap<String, serde_json::Value>>,
    agent_id: Option<&str>,
) -> ModelRef {
    use crate::defaults::{DEFAULT_MODEL, DEFAULT_PROVIDER};
    use crate::scope::resolve_agent_effective_model_primary;

    let agent_model_override = agent_id.and_then(|id| {
        resolve_agent_effective_model_primary(agents_list, id, agents_defaults_model)
    });

    // Build effective model config: agent override takes priority over global default.
    let effective_model = agent_model_override
        .filter(|m| !m.is_empty())
        .map(|override_model| {
            let mut obj = serde_json::Map::new();
            obj.insert(
                "primary".to_string(),
                serde_json::Value::String(override_model),
            );
            serde_json::Value::Object(obj)
        });

    let model_ref = effective_model.as_ref().or(agents_defaults_model);

    resolve_configured_model_ref(
        model_ref,
        configured_models,
        configured_providers,
        DEFAULT_PROVIDER,
        DEFAULT_MODEL,
    )
}

/// Resolve the subagent's configured model selection.
/// Mirrors `src/agents/models/model-selection.ts#resolveSubagentConfiguredModelSelection`. Keep in sync.
pub fn resolve_subagent_configured_model_selection(
    agents_list: &[serde_json::Value],
    agent_id: &str,
    agents_defaults_subagents_model: Option<&serde_json::Value>,
) -> Option<String> {
    use crate::scope::resolve_agent_config;

    let agent_config = resolve_agent_config(agents_list, agent_id);

    // Try agent's subagent model override first.
    if let Some(ref config) = agent_config {
        if let Some(ref subagents) = config.subagents {
            if let Some(model_val) = subagents.get("model") {
                if let Some(s) = normalize_model_selection(model_val) {
                    return Some(s);
                }
            }
        }
    }

    // Try global subagent model default.
    if let Some(val) = agents_defaults_subagents_model {
        if let Some(s) = normalize_model_selection(val) {
            return Some(s);
        }
    }

    // Fall back to agent's own model.
    if let Some(config) = agent_config {
        if let Some(ref model) = config.model {
            return normalize_model_selection(model);
        }
    }

    None
}

/// Resolve the model selection for spawning a subagent.
/// Mirrors `src/agents/models/model-selection.ts#resolveSubagentSpawnModelSelection`. Keep in sync.
pub fn resolve_subagent_spawn_model_selection(
    agents_list: &[serde_json::Value],
    agents_defaults_model: Option<&serde_json::Value>,
    configured_models: &std::collections::HashMap<String, serde_json::Value>,
    configured_providers: Option<&std::collections::HashMap<String, serde_json::Value>>,
    agent_id: &str,
    agents_defaults_subagents_model: Option<&serde_json::Value>,
    model_override: Option<&serde_json::Value>,
) -> String {
    // 1. Explicit runtime override.
    if let Some(val) = model_override {
        if let Some(s) = normalize_model_selection(val) {
            return s;
        }
    }

    // 2. Subagent-specific configured model.
    if let Some(s) = resolve_subagent_configured_model_selection(
        agents_list,
        agent_id,
        agents_defaults_subagents_model,
    ) {
        return s;
    }

    // 3. Global primary model.
    if let Some(s) = resolve_agent_model_primary_value(agents_defaults_model) {
        return s;
    }

    // 4. Runtime default.
    let runtime_default = resolve_default_model_for_agent(
        agents_list,
        agents_defaults_model,
        configured_models,
        configured_providers,
        Some(agent_id),
    );
    format!("{}/{}", runtime_default.provider, runtime_default.model)
}

/// Result of building an allowed model set.
#[derive(Debug, Clone)]
pub struct AllowedModelSet {
    pub allow_any: bool,
    pub allowed_catalog: Vec<ModelCatalogEntry>,
    pub allowed_keys: std::collections::HashSet<String>,
}

/// Build the set of allowed models from config allowlist and catalog.
/// Mirrors `src/agents/models/model-selection.ts#buildAllowedModelSet`. Keep in sync.
pub fn build_allowed_model_set(
    agents_list: &[serde_json::Value],
    raw_allowlist: &[String],
    catalog: &[ModelCatalogEntry],
    default_provider: &str,
    default_model: Option<&str>,
    agent_id: Option<&str>,
    agents_defaults_model: Option<&serde_json::Value>,
) -> AllowedModelSet {
    use crate::scope::resolve_agent_model_fallback_values;

    let allow_any = raw_allowlist.is_empty();
    let default_key = default_model.and_then(|dm| {
        let dm = dm.trim();
        if dm.is_empty() {
            return None;
        }
        parse_model_ref(dm, default_provider).map(|r| model_key(&r.provider, &r.model))
    });
    let catalog_keys: std::collections::HashSet<String> =
        catalog.iter().map(|e| model_key(&e.provider, &e.id)).collect();

    if allow_any {
        let mut keys = catalog_keys;
        if let Some(ref dk) = default_key {
            keys.insert(dk.clone());
        }
        return AllowedModelSet {
            allow_any: true,
            allowed_catalog: catalog.to_vec(),
            allowed_keys: keys,
        };
    }

    let mut allowed_keys = std::collections::HashSet::new();
    let mut synthetic = std::collections::HashMap::new();

    for raw in raw_allowlist {
        if let Some(parsed) = parse_model_ref(raw, default_provider) {
            let key = model_key(&parsed.provider, &parsed.model);
            allowed_keys.insert(key.clone());
            if !catalog_keys.contains(&key) && !synthetic.contains_key(&key) {
                synthetic.insert(
                    key,
                    ModelCatalogEntry {
                        provider: parsed.provider,
                        id: parsed.model.clone(),
                        ..Default::default()
                    },
                );
            }
        }
    }

    // Add fallback models.
    let agent_fallbacks = agent_id.and_then(|id| {
        crate::scope::resolve_agent_model_fallbacks_override(agents_list, id)
    });
    let fallbacks = agent_fallbacks.unwrap_or_else(|| {
        resolve_agent_model_fallback_values(agents_defaults_model)
    });
    for fallback in &fallbacks {
        if let Some(parsed) = parse_model_ref(fallback, default_provider) {
            let key = model_key(&parsed.provider, &parsed.model);
            allowed_keys.insert(key.clone());
            if !catalog_keys.contains(&key) && !synthetic.contains_key(&key) {
                synthetic.insert(
                    key,
                    ModelCatalogEntry {
                        provider: parsed.provider,
                        id: parsed.model.clone(),
                        ..Default::default()
                    },
                );
            }
        }
    }

    if let Some(ref dk) = default_key {
        allowed_keys.insert(dk.clone());
    }

    let mut allowed_catalog: Vec<ModelCatalogEntry> = catalog
        .iter()
        .filter(|e| allowed_keys.contains(&model_key(&e.provider, &e.id)))
        .cloned()
        .collect();
    allowed_catalog.extend(synthetic.into_values());

    if allowed_catalog.is_empty() && allowed_keys.is_empty() {
        let mut keys = catalog_keys;
        if let Some(ref dk) = default_key {
            keys.insert(dk.clone());
        }
        return AllowedModelSet {
            allow_any: true,
            allowed_catalog: catalog.to_vec(),
            allowed_keys: keys,
        };
    }

    AllowedModelSet {
        allow_any: false,
        allowed_catalog,
        allowed_keys,
    }
}

/// Validate a model ref against the allowed set and return it or an error.
/// Mirrors `src/agents/models/model-selection.ts#resolveAllowedModelRef`. Keep in sync.
pub fn resolve_allowed_model_ref(
    raw: &str,
    agents_list: &[serde_json::Value],
    raw_allowlist: &[String],
    catalog: &[ModelCatalogEntry],
    configured_models: &std::collections::HashMap<String, serde_json::Value>,
    default_provider: &str,
    default_model: Option<&str>,
    agents_defaults_model: Option<&serde_json::Value>,
) -> Result<(ModelRef, String), String> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return Err("invalid model: empty".to_string());
    }

    let alias_index = build_model_alias_index(configured_models, default_provider);
    let resolved = match resolve_model_ref_from_string(trimmed, default_provider, Some(&alias_index)) {
        Some((model_ref, _alias)) => model_ref,
        None => return Err(format!("invalid model: {}", trimmed)),
    };

    let allowed = build_allowed_model_set(
        agents_list,
        raw_allowlist,
        catalog,
        default_provider,
        default_model,
        None,
        agents_defaults_model,
    );
    let key = model_key(&resolved.provider, &resolved.model);
    if !allowed.allow_any && !allowed.allowed_keys.contains(&key) {
        return Err(format!("model not allowed: {}", key));
    }

    Ok((resolved, key))
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

/// Resolve the model for Gmail hook processing.
/// Mirrors `src/agents/models/model-selection.ts#resolveHooksGmailModel`. Keep in sync.
pub fn resolve_hooks_gmail_model(
    hooks_gmail_model: Option<&str>,
    configured_models: &std::collections::HashMap<String, serde_json::Value>,
    default_provider: &str,
) -> Option<ModelRef> {
    let model_str = hooks_gmail_model?.trim();
    if model_str.is_empty() {
        return None;
    }

    let alias_index = build_model_alias_index(configured_models, default_provider);
    resolve_model_ref_from_string(model_str, default_provider, Some(&alias_index))
        .map(|(model_ref, _)| model_ref)
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
    fn model_catalog_support_checks() {
        let entry = ModelCatalogEntry {
            provider: "anthropic".to_string(),
            id: "claude-opus-4-6".to_string(),
            name: "Claude Opus 4.6".to_string(),
            input: Some(vec![ModelInputType::Text, ModelInputType::Image, ModelInputType::Document]),
            reasoning: false,
            context_window: Some(200_000),
        };
        assert!(model_supports_vision(Some(&entry)));
        assert!(model_supports_document(Some(&entry)));
        assert!(!model_supports_vision(None));
    }

    #[test]
    fn find_model_in_catalog_basic() {
        let catalog = vec![ModelCatalogEntry {
            provider: "anthropic".to_string(),
            id: "claude-opus-4-6".to_string(),
            ..Default::default()
        }];
        assert!(find_model_in_catalog(&catalog, "anthropic", "claude-opus-4-6").is_some());
        assert!(find_model_in_catalog(&catalog, "openai", "gpt-4o").is_none());
    }

    #[test]
    fn get_model_ref_status_allowed() {
        let catalog = vec![ModelCatalogEntry {
            provider: "anthropic".to_string(),
            id: "claude-opus-4-6".to_string(),
            ..Default::default()
        }];
        let mut allowed = std::collections::HashSet::new();
        allowed.insert("anthropic/claude-opus-4-6".to_string());
        let model_ref = ModelRef {
            provider: "anthropic".to_string(),
            model: "claude-opus-4-6".to_string(),
        };
        let status = get_model_ref_status(&model_ref, &catalog, Some(&allowed));
        assert!(status.in_catalog);
        assert!(status.allowed);
        assert!(!status.allow_any);
    }

    #[test]
    fn get_model_ref_status_no_allowlist() {
        let model_ref = ModelRef {
            provider: "anthropic".to_string(),
            model: "claude-opus-4-6".to_string(),
        };
        let status = get_model_ref_status(&model_ref, &[], None);
        assert!(status.allow_any);
        assert!(status.allowed);
    }

    #[test]
    fn resolve_reasoning_default_basic() {
        let catalog = vec![ModelCatalogEntry {
            provider: "openai".to_string(),
            id: "o3".to_string(),
            reasoning: true,
            ..Default::default()
        }];
        assert!(resolve_reasoning_default(&catalog, "openai", "o3"));
        assert!(!resolve_reasoning_default(&catalog, "openai", "gpt-4o"));
    }
}
