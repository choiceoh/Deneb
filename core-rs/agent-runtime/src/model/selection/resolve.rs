//! High-level model resolution: configured defaults, subagent spawning, and hooks.
//!
//! Mirrors `src/agents/models/model-selection.ts` and `src/config/model-input.ts`. Keep in sync.

use super::allowlist::build_model_alias_index;
use super::normalize::normalize_model_selection;
use super::parse::{parse_model_ref, resolve_model_ref_from_string};
use super::types::{ModelRef, ProviderConfigEntry, SubagentSpawnModelSelectionParams};
use crate::model::provider_id::normalize_provider_id;

/// Check if a provider is a CLI provider.
pub fn is_cli_provider(
    provider: &str,
    cli_backends: Option<&std::collections::HashMap<String, serde_json::Value>>,
) -> bool {
    let normalized = normalize_provider_id(provider);
    if normalized == "claude-cli" || normalized == "codex-cli" {
        return true;
    }
    if let Some(backends) = cli_backends {
        return backends
            .keys()
            .any(|key| normalize_provider_id(key) == normalized);
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
    // TS iterates with Object.entries() which preserves insertion order.
    // HashMap has no guaranteed order, so sort keys alphabetically for
    // deterministic fallback selection across TS/Rust boundaries.
    if let Some(providers) = configured_providers {
        let has_default = providers.contains_key(default_provider);
        if !has_default {
            let mut sorted_keys: Vec<&String> = providers.keys().collect();
            sorted_keys.sort();
            for provider_name in sorted_keys {
                let provider_cfg = &providers[provider_name];
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
    params: &SubagentSpawnModelSelectionParams<'_>,
) -> String {
    let agents_list = params.agents_list;
    let agents_defaults_model = params.agents_defaults_model;
    let configured_models = params.configured_models;
    let configured_providers = params.configured_providers;
    let agent_id = params.agent_id;
    let agents_defaults_subagents_model = params.agents_defaults_subagents_model;
    let model_override = params.model_override;

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
    use super::super::types::SubagentSpawnModelSelectionParams;
    use super::{
        infer_unique_provider_from_configured_models, is_cli_provider,
        resolve_agent_model_primary_value, resolve_configured_model_ref,
        resolve_default_model_for_agent, resolve_hooks_gmail_model,
        resolve_subagent_configured_model_selection, resolve_subagent_spawn_model_selection,
        to_agent_model_list_like,
    };
    use crate::defaults::DEFAULT_PROVIDER;

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
    fn to_agent_model_list_like_string() -> Result<(), Box<dyn std::error::Error>> {
        let val = serde_json::json!("claude-opus-4-6");
        let result =
            to_agent_model_list_like(Some(&val)).ok_or("to_agent_model_list_like returned None")?;
        assert_eq!(result["primary"], "claude-opus-4-6");
        Ok(())
    }

    #[test]
    fn to_agent_model_list_like_object() -> Result<(), Box<dyn std::error::Error>> {
        let val = serde_json::json!({"primary": "m1", "fallbacks": ["m2"]});
        let result =
            to_agent_model_list_like(Some(&val)).ok_or("to_agent_model_list_like returned None")?;
        assert_eq!(result["primary"], "m1");
        Ok(())
    }

    #[test]
    fn to_agent_model_list_like_none() {
        assert!(to_agent_model_list_like(None).is_none());
        assert!(to_agent_model_list_like(Some(&serde_json::json!(""))).is_none());
    }

    #[test]
    fn resolve_agent_model_primary_value_basic() {
        assert_eq!(
            resolve_agent_model_primary_value(Some(&serde_json::json!("claude-opus-4-6"))),
            Some("claude-opus-4-6".to_string())
        );
        assert_eq!(
            resolve_agent_model_primary_value(Some(&serde_json::json!({"primary": "m1"}))),
            Some("m1".to_string())
        );
        assert_eq!(resolve_agent_model_primary_value(None), None);
    }

    #[test]
    fn resolve_configured_model_ref_basic() {
        let models = std::collections::HashMap::new();
        let result = resolve_configured_model_ref(
            Some(&serde_json::json!("claude-sonnet-4-6")),
            &models,
            None,
            DEFAULT_PROVIDER,
            "claude-opus-4-6",
        );
        // bare model name -> anthropic default
        assert_eq!(result.provider, "anthropic");
        assert_eq!(result.model, "claude-sonnet-4-6");
    }

    #[test]
    fn resolve_configured_model_ref_with_provider() {
        let models = std::collections::HashMap::new();
        let result = resolve_configured_model_ref(
            Some(&serde_json::json!("openai/gpt-4o")),
            &models,
            None,
            DEFAULT_PROVIDER,
            "claude-opus-4-6",
        );
        assert_eq!(result.provider, "openai");
        assert_eq!(result.model, "gpt-4o");
    }

    #[test]
    fn resolve_configured_model_ref_fallback_provider() {
        let models = std::collections::HashMap::new();
        let mut providers = std::collections::HashMap::new();
        providers.insert(
            "openai".to_string(),
            serde_json::json!({"models": [{"id": "gpt-4o"}]}),
        );
        let result = resolve_configured_model_ref(
            None,
            &models,
            Some(&providers),
            "anthropic", // not in providers
            "claude-opus-4-6",
        );
        assert_eq!(result.provider, "openai");
        assert_eq!(result.model, "gpt-4o");
    }

    #[test]
    fn resolve_configured_model_ref_default() {
        let models = std::collections::HashMap::new();
        let result =
            resolve_configured_model_ref(None, &models, None, DEFAULT_PROVIDER, "claude-opus-4-6");
        assert_eq!(result.provider, "anthropic");
        assert_eq!(result.model, "claude-opus-4-6");
    }

    #[test]
    fn resolve_default_model_for_agent_basic() {
        let agents = vec![serde_json::json!({"id": "alpha", "model": "openai/gpt-4o"})];
        let models = std::collections::HashMap::new();
        let result = resolve_default_model_for_agent(&agents, None, &models, None, Some("alpha"));
        assert_eq!(result.provider, "openai");
        assert_eq!(result.model, "gpt-4o");
    }

    #[test]
    fn resolve_default_model_for_agent_no_override() {
        let agents: Vec<serde_json::Value> = vec![];
        let models = std::collections::HashMap::new();
        let result = resolve_default_model_for_agent(
            &agents,
            Some(&serde_json::json!("claude-sonnet-4-6")),
            &models,
            None,
            None,
        );
        assert_eq!(result.provider, "anthropic");
        assert_eq!(result.model, "claude-sonnet-4-6");
    }

    #[test]
    fn resolve_hooks_gmail_model_basic() -> Result<(), Box<dyn std::error::Error>> {
        let models = std::collections::HashMap::new();
        let result = resolve_hooks_gmail_model(Some("openai/gpt-4o"), &models, DEFAULT_PROVIDER);
        assert!(result.is_some());
        let model_ref = result.ok_or("resolve_hooks_gmail_model returned None")?;
        assert_eq!(model_ref.provider, "openai");
        assert_eq!(model_ref.model, "gpt-4o");
        Ok(())
    }

    #[test]
    fn resolve_hooks_gmail_model_none() {
        let models = std::collections::HashMap::new();
        assert!(resolve_hooks_gmail_model(None, &models, DEFAULT_PROVIDER).is_none());
        assert!(resolve_hooks_gmail_model(Some(""), &models, DEFAULT_PROVIDER).is_none());
    }

    #[test]
    fn resolve_subagent_configured_model_selection_basic() {
        let agents = vec![serde_json::json!({
            "id": "alpha",
            "model": "claude-sonnet-4-6",
            "subagents": {"model": "claude-haiku-4-5"}
        })];
        let result = resolve_subagent_configured_model_selection(&agents, "alpha", None);
        assert_eq!(result, Some("claude-haiku-4-5".to_string()));
    }

    #[test]
    fn resolve_subagent_configured_model_selection_fallback_to_agent() {
        let agents = vec![serde_json::json!({
            "id": "alpha",
            "model": "claude-sonnet-4-6"
        })];
        let result = resolve_subagent_configured_model_selection(&agents, "alpha", None);
        assert_eq!(result, Some("claude-sonnet-4-6".to_string()));
    }

    #[test]
    fn resolve_subagent_spawn_model_selection_override() {
        let agents: Vec<serde_json::Value> = vec![];
        let models = std::collections::HashMap::new();
        let result = resolve_subagent_spawn_model_selection(&SubagentSpawnModelSelectionParams {
            agents_list: &agents,
            agents_defaults_model: None,
            configured_models: &models,
            configured_providers: None,
            agent_id: "alpha",
            agents_defaults_subagents_model: None,
            model_override: Some(&serde_json::json!("openai/gpt-4o")),
        });
        assert_eq!(result, "openai/gpt-4o");
    }
}
