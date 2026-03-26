//! Provider ID normalization and lookup.
//!
//! Mirrors `src/agents/provider-id.ts`. Keep in sync.

use std::collections::HashMap;

/// Normalize a provider identifier to its canonical form.
/// Handles aliases, legacy naming, and case normalization.
pub fn normalize_provider_id(provider: &str) -> String {
    let normalized = provider.trim().to_lowercase();
    match normalized.as_str() {
        "z.ai" | "z-ai" => "zai".to_string(),
        "opencode-zen" => "opencode".to_string(),
        "opencode-go-auth" => "opencode-go".to_string(),
        "qwen" => "qwen-portal".to_string(),
        "kimi" | "kimi-code" | "kimi-coding" => "kimi".to_string(),
        "bedrock" | "aws-bedrock" => "amazon-bedrock".to_string(),
        // Backward compatibility for older provider naming.
        "bytedance" | "doubao" => "volcengine".to_string(),
        _ => normalized,
    }
}

/// Normalize provider ID for auth lookup. Coding-plan variants share auth with base.
pub fn normalize_provider_id_for_auth(provider: &str) -> String {
    let normalized = normalize_provider_id(provider);
    match normalized.as_str() {
        "volcengine-plan" => "volcengine".to_string(),
        "byteplus-plan" => "byteplus".to_string(),
        _ => normalized,
    }
}

/// Find a value in a string-keyed map by normalized provider ID.
pub fn find_normalized_provider_value<'a, T>(
    entries: &'a HashMap<String, T>,
    provider: &str,
) -> Option<&'a T> {
    let provider_key = normalize_provider_id(provider);
    entries
        .iter()
        .find(|(key, _)| normalize_provider_id(key) == provider_key)
        .map(|(_, value)| value)
}

/// Find the original key in a string-keyed map by normalized provider ID.
pub fn find_normalized_provider_key<'a, T>(
    entries: &'a HashMap<String, T>,
    provider: &str,
) -> Option<&'a str> {
    let provider_key = normalize_provider_id(provider);
    entries
        .keys()
        .find(|key| normalize_provider_id(key) == provider_key)
        .map(|s| s.as_str())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalize_basic() {
        assert_eq!(normalize_provider_id("anthropic"), "anthropic");
        assert_eq!(normalize_provider_id("  Anthropic  "), "anthropic");
        assert_eq!(normalize_provider_id("OPENAI"), "openai");
    }

    #[test]
    fn normalize_aliases() {
        assert_eq!(normalize_provider_id("z.ai"), "zai");
        assert_eq!(normalize_provider_id("z-ai"), "zai");
        assert_eq!(normalize_provider_id("qwen"), "qwen-portal");
        assert_eq!(normalize_provider_id("bedrock"), "amazon-bedrock");
        assert_eq!(normalize_provider_id("aws-bedrock"), "amazon-bedrock");
        assert_eq!(normalize_provider_id("bytedance"), "volcengine");
        assert_eq!(normalize_provider_id("doubao"), "volcengine");
        assert_eq!(normalize_provider_id("kimi-code"), "kimi");
        assert_eq!(normalize_provider_id("kimi-coding"), "kimi");
    }

    #[test]
    fn normalize_for_auth() {
        assert_eq!(normalize_provider_id_for_auth("volcengine-plan"), "volcengine");
        assert_eq!(normalize_provider_id_for_auth("byteplus-plan"), "byteplus");
        assert_eq!(normalize_provider_id_for_auth("anthropic"), "anthropic");
    }

    #[test]
    fn find_provider_value() {
        let mut map = HashMap::new();
        map.insert("Anthropic".to_string(), 42);
        map.insert("openai".to_string(), 99);

        assert_eq!(find_normalized_provider_value(&map, "anthropic"), Some(&42));
        assert_eq!(find_normalized_provider_value(&map, "OPENAI"), Some(&99));
        assert_eq!(find_normalized_provider_value(&map, "google"), None);
    }

    #[test]
    fn find_provider_key() {
        let mut map: HashMap<String, ()> = HashMap::new();
        map.insert("Anthropic".to_string(), ());
        map.insert("aws-bedrock".to_string(), ());

        assert_eq!(find_normalized_provider_key(&map, "anthropic"), Some("Anthropic"));
        assert_eq!(
            find_normalized_provider_key(&map, "amazon-bedrock"),
            Some("aws-bedrock")
        );
        assert_eq!(find_normalized_provider_key(&map, "google"), None);
    }
}
