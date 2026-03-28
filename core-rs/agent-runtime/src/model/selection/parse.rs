//! Model reference parsing and auth-profile splitting.
//!
//! Mirrors `src/agents/models/model-selection.ts` and
//! `src/agents/models/model-ref-profile.ts`. Keep in sync.

use super::normalize::normalize_model_ref;
use super::types::{ModelAliasIndex, ModelRef};

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
    let is_version_suffix =
        after_at.len() >= 8 && after_at.as_bytes()[..8].iter().all(|b| b.is_ascii_digit());

    if is_version_suffix {
        // Look for another @ after the 8-digit version suffix.
        let next_at = if after_at.len() > 8 && after_at.as_bytes()[8] == b'@' {
            // YYYYMMDD@ immediately followed by profile.
            Some(profile_delimiter + 9)
        } else {
            // Search further for @.
            after_at
                .get(9..)
                .and_then(|rest| rest.find('@').map(|i| profile_delimiter + 9 + i))
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

#[cfg(test)]
mod tests {
    use super::{parse_model_ref, resolve_model_ref_from_string, split_trailing_auth_profile};
    use super::super::allowlist::build_model_alias_index;
    use crate::defaults::DEFAULT_PROVIDER;

    #[test]
    fn parse_model_ref_with_provider() -> Result<(), Box<dyn std::error::Error>> {
        let result = parse_model_ref("anthropic/claude-opus-4-6", DEFAULT_PROVIDER)
            .ok_or("parse_model_ref returned None")?;
        assert_eq!(result.provider, "anthropic");
        assert_eq!(result.model, "claude-opus-4-6");
        Ok(())
    }

    #[test]
    fn parse_model_ref_without_provider() -> Result<(), Box<dyn std::error::Error>> {
        let result = parse_model_ref("claude-opus-4-6", DEFAULT_PROVIDER)
            .ok_or("parse_model_ref returned None")?;
        assert_eq!(result.provider, "anthropic");
        assert_eq!(result.model, "claude-opus-4-6");
        Ok(())
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
    fn resolve_model_ref_from_string_alias() -> Result<(), Box<dyn std::error::Error>> {
        let mut models = std::collections::HashMap::new();
        models.insert(
            "anthropic/claude-opus-4-6".to_string(),
            serde_json::json!({"alias": "opus"}),
        );
        let index = build_model_alias_index(&models, DEFAULT_PROVIDER);
        let (model_ref, alias) =
            resolve_model_ref_from_string("opus", DEFAULT_PROVIDER, Some(&index))
                .ok_or("resolve_model_ref_from_string returned None")?;
        assert_eq!(model_ref.model, "claude-opus-4-6");
        assert_eq!(alias, Some("opus".to_string()));
        Ok(())
    }

    #[test]
    fn resolve_model_ref_from_string_no_alias() -> Result<(), Box<dyn std::error::Error>> {
        let (model_ref, alias) =
            resolve_model_ref_from_string("claude-opus-4-6", DEFAULT_PROVIDER, None)
                .ok_or("resolve_model_ref_from_string returned None")?;
        assert_eq!(model_ref.model, "claude-opus-4-6");
        assert!(alias.is_none());
        Ok(())
    }
}
