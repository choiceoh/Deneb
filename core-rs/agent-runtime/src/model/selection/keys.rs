//! Canonical model key generation.

use std::borrow::Cow;

/// Generate a canonical model key from provider and model.
/// If the model already starts with "provider/", returns the model borrowed
/// without any allocation. Otherwise allocates `"provider/model"`.
pub fn model_key<'a>(provider: &'a str, model: &'a str) -> Cow<'a, str> {
    let provider_id = provider.trim();
    let model_id = model.trim();

    if provider_id.is_empty() {
        return Cow::Borrowed(model_id);
    }
    if model_id.is_empty() {
        return Cow::Borrowed(provider_id);
    }

    // Check if model already contains the provider prefix (case-insensitive).
    // Avoids allocating a format string just for the prefix comparison.
    let prefix_len = provider_id.len() + 1; // provider + "/"
    if model_id.len() >= prefix_len
        && model_id.as_bytes()[provider_id.len()] == b'/'
        && model_id[..provider_id.len()].eq_ignore_ascii_case(provider_id)
    {
        return Cow::Borrowed(model_id);
    }

    Cow::Owned(format!("{}/{}", provider_id, model_id))
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn model_key_basic() {
        assert_eq!(
            model_key("anthropic", "claude-opus-4-6"),
            "anthropic/claude-opus-4-6"
        );
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
}
