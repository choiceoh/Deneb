//! Model catalog queries: capability checks and status resolution.

use super::keys::model_key;
use super::types::{ModelCatalogEntry, ModelInputType, ModelRef, ModelRefStatus};
use crate::model::provider_id::normalize_provider_id;

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
    catalog
        .iter()
        .find(|e| normalize_provider_id(&e.provider) == p && e.id.to_lowercase() == m)
}

/// Get the status of a model ref against the allowlist and catalog.
pub fn get_model_ref_status(
    model_ref: &ModelRef,
    catalog: &[ModelCatalogEntry],
    allowed_keys: Option<&std::collections::HashSet<String>>,
) -> ModelRefStatus {
    let key = model_key(&model_ref.provider, &model_ref.model);
    let in_catalog =
        find_model_in_catalog(catalog, &model_ref.provider, &model_ref.model).is_some();
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

#[cfg(test)]
mod tests {
    use super::super::types::{ModelCatalogEntry, ModelInputType, ModelRef};
    use super::{
        find_model_in_catalog, get_model_ref_status, model_supports_document,
        model_supports_vision, resolve_reasoning_default,
    };

    #[test]
    fn model_catalog_support_checks() {
        let entry = ModelCatalogEntry {
            provider: "anthropic".to_string(),
            id: "claude-opus-4-6".to_string(),
            name: "Claude Opus 4.6".to_string(),
            input: Some(vec![
                ModelInputType::Text,
                ModelInputType::Image,
                ModelInputType::Document,
            ]),
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
