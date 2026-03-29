//! Allowlist building, alias index, and allowed-model-ref validation.
//!
//! Mirrors `src/agents/models/model-selection.ts`. Keep in sync.

use super::keys::model_key;
use super::parse::{parse_model_ref, resolve_model_ref_from_string};
use super::types::{
    AllowedModelSet, BuildAllowedModelSetParams, ModelAliasIndex, ModelCatalogEntry, ModelRef,
    ResolveAllowedModelRefParams,
};

/// Resolve a model key from allowlist entry string.
pub fn resolve_allowlist_model_key(raw: &str, default_provider: &str) -> Option<String> {
    let parsed = parse_model_ref(raw, default_provider)?;
    Some(model_key(&parsed.provider, &parsed.model).into_owned())
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
    if keys.is_empty() {
        None
    } else {
        Some(keys)
    }
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
        let key = model_key(&parsed.provider, &parsed.model).into_owned();
        index.by_key.entry(key).or_default().push(alias);
    }
    index
}

/// Build the set of allowed models from config allowlist and catalog.
/// Mirrors `src/agents/models/model-selection.ts#buildAllowedModelSet`. Keep in sync.
pub fn build_allowed_model_set(params: &BuildAllowedModelSetParams<'_>) -> AllowedModelSet {
    use crate::scope::resolve_agent_model_fallback_values;

    let agents_list = params.agents_list;
    let raw_allowlist = params.raw_allowlist;
    let catalog = params.catalog;
    let default_provider = params.default_provider;
    let default_model = params.default_model;
    let agent_id = params.agent_id;
    let agents_defaults_model = params.agents_defaults_model;

    let allow_any = raw_allowlist.is_empty();
    let default_key = default_model.and_then(|dm| {
        let dm = dm.trim();
        if dm.is_empty() {
            return None;
        }
        parse_model_ref(dm, default_provider).map(|r| model_key(&r.provider, &r.model).into_owned())
    });
    let catalog_keys: std::collections::HashSet<String> = catalog
        .iter()
        .map(|e| model_key(&e.provider, &e.id).into_owned())
        .collect();

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
            let key = model_key(&parsed.provider, &parsed.model).into_owned();
            if !catalog_keys.contains(&key) && !synthetic.contains_key(&key) {
                synthetic.insert(
                    key.clone(),
                    ModelCatalogEntry {
                        provider: parsed.provider,
                        id: parsed.model.clone(),
                        ..Default::default()
                    },
                );
            }
            allowed_keys.insert(key);
        }
    }

    // Add fallback models.
    let agent_fallbacks = agent_id
        .and_then(|id| crate::scope::resolve_agent_model_fallbacks_override(agents_list, id));
    let fallbacks = agent_fallbacks
        .unwrap_or_else(|| resolve_agent_model_fallback_values(agents_defaults_model));
    for fallback in &fallbacks {
        if let Some(parsed) = parse_model_ref(fallback, default_provider) {
            let key = model_key(&parsed.provider, &parsed.model).into_owned();
            if !catalog_keys.contains(&key) && !synthetic.contains_key(&key) {
                synthetic.insert(
                    key.clone(),
                    ModelCatalogEntry {
                        provider: parsed.provider,
                        id: parsed.model.clone(),
                        ..Default::default()
                    },
                );
            }
            allowed_keys.insert(key);
        }
    }

    if let Some(ref dk) = default_key {
        allowed_keys.insert(dk.clone());
    }

    let mut allowed_catalog: Vec<ModelCatalogEntry> = catalog
        .iter()
        .filter(|e| allowed_keys.contains(model_key(&e.provider, &e.id).as_ref()))
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
#[allow(clippy::too_many_arguments)]
pub fn resolve_allowed_model_ref(
    params: &ResolveAllowedModelRefParams<'_>,
) -> Result<(ModelRef, String), String> {
    let raw = params.raw;
    let agents_list = params.agents_list;
    let raw_allowlist = params.raw_allowlist;
    let catalog = params.catalog;
    let configured_models = params.configured_models;
    let default_provider = params.default_provider;
    let default_model = params.default_model;
    let agents_defaults_model = params.agents_defaults_model;
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return Err("invalid model: empty".to_string());
    }

    let alias_index = build_model_alias_index(configured_models, default_provider);
    let resolved =
        match resolve_model_ref_from_string(trimmed, default_provider, Some(&alias_index)) {
            Some((model_ref, _alias)) => model_ref,
            None => return Err(format!("invalid model: {}", trimmed)),
        };

    let allowed = build_allowed_model_set(&BuildAllowedModelSetParams {
        agents_list,
        raw_allowlist,
        catalog,
        default_provider,
        default_model,
        agent_id: None,
        agents_defaults_model,
    });
    let key = model_key(&resolved.provider, &resolved.model).into_owned();
    if !allowed.allow_any && !allowed.allowed_keys.contains(key.as_str()) {
        return Err(format!("model not allowed: {}", key));
    }

    Ok((resolved, key))
}

#[cfg(test)]
mod tests {
    use super::super::types::{
        BuildAllowedModelSetParams, ModelCatalogEntry, ResolveAllowedModelRefParams,
    };
    use super::{
        build_allowed_model_set, build_configured_allowlist_keys, build_model_alias_index,
        resolve_allowed_model_ref, resolve_allowlist_model_key,
    };
    use crate::defaults::DEFAULT_PROVIDER;

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
    fn build_configured_allowlist_keys_basic() -> Result<(), Box<dyn std::error::Error>> {
        let keys = vec![
            "anthropic/claude-opus-4-6".to_string(),
            "openai/gpt-4o".to_string(),
        ];
        let result = build_configured_allowlist_keys(&keys, DEFAULT_PROVIDER)
            .ok_or("build_configured_allowlist_keys returned None")?;
        assert!(result.contains("anthropic/claude-opus-4-6"));
        assert!(result.contains("openai/gpt-4o"));
        Ok(())
    }

    #[test]
    fn build_configured_allowlist_keys_empty() {
        assert!(build_configured_allowlist_keys(&[], DEFAULT_PROVIDER).is_none());
    }

    #[test]
    fn build_model_alias_index_basic() -> Result<(), Box<dyn std::error::Error>> {
        let mut models = std::collections::HashMap::new();
        models.insert(
            "anthropic/claude-opus-4-6".to_string(),
            serde_json::json!({"alias": "opus"}),
        );
        let index = build_model_alias_index(&models, DEFAULT_PROVIDER);
        assert!(index.by_alias.contains_key("opus"));
        let (alias, model_ref) = index
            .by_alias
            .get("opus")
            .ok_or("alias 'opus' not found in index")?;
        assert_eq!(alias, "opus");
        assert_eq!(model_ref.provider, "anthropic");
        assert_eq!(model_ref.model, "claude-opus-4-6");
        Ok(())
    }

    #[test]
    fn build_allowed_model_set_allow_any() {
        let agents: Vec<serde_json::Value> = vec![];
        let catalog = vec![ModelCatalogEntry {
            provider: "anthropic".to_string(),
            id: "claude-opus-4-6".to_string(),
            ..Default::default()
        }];
        let result = build_allowed_model_set(&BuildAllowedModelSetParams {
            agents_list: &agents,
            raw_allowlist: &[],
            catalog: &catalog,
            default_provider: DEFAULT_PROVIDER,
            default_model: None,
            agent_id: None,
            agents_defaults_model: None,
        });
        assert!(result.allow_any);
        assert_eq!(result.allowed_catalog.len(), 1);
    }

    #[test]
    fn build_allowed_model_set_restricted() {
        let agents: Vec<serde_json::Value> = vec![];
        let catalog = vec![
            ModelCatalogEntry {
                provider: "anthropic".to_string(),
                id: "claude-opus-4-6".to_string(),
                ..Default::default()
            },
            ModelCatalogEntry {
                provider: "openai".to_string(),
                id: "gpt-4o".to_string(),
                ..Default::default()
            },
        ];
        let allowlist = vec!["anthropic/claude-opus-4-6".to_string()];
        let result = build_allowed_model_set(&BuildAllowedModelSetParams {
            agents_list: &agents,
            raw_allowlist: &allowlist,
            catalog: &catalog,
            default_provider: DEFAULT_PROVIDER,
            default_model: None,
            agent_id: None,
            agents_defaults_model: None,
        });
        assert!(!result.allow_any);
        assert!(result.allowed_keys.contains("anthropic/claude-opus-4-6"));
        assert!(!result.allowed_keys.contains("openai/gpt-4o"));
    }

    #[test]
    fn resolve_allowed_model_ref_valid() -> Result<(), Box<dyn std::error::Error>> {
        let agents: Vec<serde_json::Value> = vec![];
        let models = std::collections::HashMap::new();
        let result = resolve_allowed_model_ref(&ResolveAllowedModelRefParams {
            raw: "anthropic/claude-opus-4-6",
            agents_list: &agents,
            raw_allowlist: &[],
            catalog: &[],
            configured_models: &models,
            default_provider: DEFAULT_PROVIDER,
            default_model: None,
            agents_defaults_model: None,
        });
        assert!(result.is_ok());
        let (model_ref, key) = result?;
        assert_eq!(model_ref.provider, "anthropic");
        assert_eq!(key, "anthropic/claude-opus-4-6");
        Ok(())
    }

    #[test]
    fn resolve_allowed_model_ref_empty() {
        let agents: Vec<serde_json::Value> = vec![];
        let models = std::collections::HashMap::new();
        let result = resolve_allowed_model_ref(&ResolveAllowedModelRefParams {
            raw: "",
            agents_list: &agents,
            raw_allowlist: &[],
            catalog: &[],
            configured_models: &models,
            default_provider: DEFAULT_PROVIDER,
            default_model: None,
            agents_defaults_model: None,
        });
        assert!(result.is_err());
    }
}
