//! Agent registry resolution — pure logic for config-driven agent management.
//!
//! Mirrors `src/agents/agent-scope.ts` (pure-logic subset). Keep in sync.
//! Filesystem-dependent functions remain in TypeScript.

use once_cell::sync::Lazy;
use regex::Regex;
use serde::{Deserialize, Serialize};

/// Default agent identifier when none is configured.
pub const DEFAULT_AGENT_ID: &str = "main";

// Pre-compiled regexes for agent ID normalization.
static VALID_ID_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$").unwrap());
static INVALID_CHARS_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"[^a-z0-9_-]+").unwrap());
static LEADING_DASH_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"^-+").unwrap());
static TRAILING_DASH_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"-+$").unwrap());

/// Normalize an agent ID to a path-safe, shell-friendly form.
pub fn normalize_agent_id(value: &str) -> String {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return DEFAULT_AGENT_ID.to_string();
    }
    if VALID_ID_RE.is_match(trimmed) {
        return trimmed.to_lowercase();
    }
    // Best-effort fallback: collapse invalid characters to "-".
    let lowered = trimmed.to_lowercase();
    let collapsed = INVALID_CHARS_RE.replace_all(&lowered, "-");
    let no_leading = LEADING_DASH_RE.replace(&collapsed, "");
    let no_trailing = TRAILING_DASH_RE.replace(&no_leading, "");
    let result = if no_trailing.len() > 64 {
        &no_trailing[..64]
    } else {
        &no_trailing
    };
    if result.is_empty() {
        DEFAULT_AGENT_ID.to_string()
    } else {
        result.to_string()
    }
}

/// Agent entry from config (mirrors DenebConfig.agents.list[]).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AgentEntry {
    /// Agent identifier.
    #[serde(default)]
    pub id: String,
    /// Human-readable name.
    pub name: Option<String>,
    /// Whether this is the default agent.
    #[serde(default)]
    pub default: bool,
    /// Workspace directory path.
    pub workspace: Option<String>,
    /// Agent directory path override.
    pub agent_dir: Option<String>,
    /// Model configuration (string or object with primary + fallbacks).
    pub model: Option<serde_json::Value>,
    /// Skills filter (array of skill names).
    pub skills: Option<Vec<String>>,
}

/// Resolved agent configuration (subset of AgentEntry with normalized values).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ResolvedAgentConfig {
    pub name: Option<String>,
    pub workspace: Option<String>,
    pub agent_dir: Option<String>,
    pub model: Option<serde_json::Value>,
    pub skills: Option<Vec<String>>,
}

/// Result of resolving both default and session agent IDs.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SessionAgentIds {
    pub default_agent_id: String,
    pub session_agent_id: String,
}

/// Extract valid agent entries from a config's agent list.
pub fn list_agent_entries(agents_list: &[serde_json::Value]) -> Vec<AgentEntry> {
    agents_list
        .iter()
        .filter_map(|v| serde_json::from_value::<AgentEntry>(v.clone()).ok())
        .collect()
}

/// List all agent IDs from config, defaulting to [DEFAULT_AGENT_ID].
pub fn list_agent_ids(agents_list: &[serde_json::Value]) -> Vec<String> {
    let agents = list_agent_entries(agents_list);
    if agents.is_empty() {
        return vec![DEFAULT_AGENT_ID.to_string()];
    }

    let mut seen = std::collections::HashSet::new();
    let mut ids = Vec::new();
    for entry in &agents {
        let id = normalize_agent_id(&entry.id);
        if seen.insert(id.clone()) {
            ids.push(id);
        }
    }

    if ids.is_empty() {
        vec![DEFAULT_AGENT_ID.to_string()]
    } else {
        ids
    }
}

/// Resolve the default agent ID from config.
/// Uses the first agent marked `default=true`, or the first entry.
pub fn resolve_default_agent_id(agents_list: &[serde_json::Value]) -> String {
    let agents = list_agent_entries(agents_list);
    if agents.is_empty() {
        return DEFAULT_AGENT_ID.to_string();
    }

    let defaults: Vec<_> = agents.iter().filter(|a| a.default).collect();
    let chosen = defaults.first().copied().or(agents.first());

    match chosen {
        Some(agent) => {
            let id = agent.id.trim();
            normalize_agent_id(if id.is_empty() { DEFAULT_AGENT_ID } else { id })
        }
        None => DEFAULT_AGENT_ID.to_string(),
    }
}

/// Resolve both default and session-specific agent IDs.
pub fn resolve_session_agent_ids(
    agents_list: &[serde_json::Value],
    session_key: Option<&str>,
    explicit_agent_id: Option<&str>,
) -> SessionAgentIds {
    let default_agent_id = resolve_default_agent_id(agents_list);

    let explicit_id = explicit_agent_id
        .map(|s| s.trim().to_lowercase())
        .filter(|s| !s.is_empty())
        .map(|s| normalize_agent_id(&s));

    let session_agent_id = if let Some(id) = explicit_id {
        id
    } else if let Some(key) = session_key {
        // Extract agent ID from session key format "agent:<id>:<rest>".
        parse_agent_id_from_session_key(key).unwrap_or_else(|| default_agent_id.clone())
    } else {
        default_agent_id.clone()
    };

    SessionAgentIds {
        default_agent_id,
        session_agent_id,
    }
}

/// Extract the agent ID from a session key of format "agent:<id>:<rest>".
fn parse_agent_id_from_session_key(session_key: &str) -> Option<String> {
    let trimmed = session_key.trim().to_lowercase();
    if !trimmed.starts_with("agent:") {
        return None;
    }
    let rest = &trimmed["agent:".len()..];
    let colon_pos = rest.find(':')?;
    let agent_id = &rest[..colon_pos];
    if agent_id.is_empty() {
        return None;
    }
    Some(normalize_agent_id(agent_id))
}

/// Resolve a specific agent's config from the list.
pub fn resolve_agent_config(
    agents_list: &[serde_json::Value],
    agent_id: &str,
) -> Option<ResolvedAgentConfig> {
    let id = normalize_agent_id(agent_id);
    let entries = list_agent_entries(agents_list);
    let entry = entries
        .into_iter()
        .find(|e| normalize_agent_id(&e.id) == id)?;

    Some(ResolvedAgentConfig {
        name: entry.name,
        workspace: entry.workspace,
        agent_dir: entry.agent_dir,
        model: entry.model,
        skills: entry.skills,
    })
}

/// Resolve the primary model string for an agent from its config entry.
fn resolve_model_primary(raw: Option<&serde_json::Value>) -> Option<String> {
    match raw? {
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

/// Resolve the agent's explicitly configured primary model.
pub fn resolve_agent_explicit_model_primary(
    agents_list: &[serde_json::Value],
    agent_id: &str,
) -> Option<String> {
    let config = resolve_agent_config(agents_list, agent_id)?;
    resolve_model_primary(config.model.as_ref())
}

/// Resolve the agent's effective primary model (agent config -> global defaults).
pub fn resolve_agent_effective_model_primary(
    agents_list: &[serde_json::Value],
    agent_id: &str,
    global_model: Option<&serde_json::Value>,
) -> Option<String> {
    resolve_agent_explicit_model_primary(agents_list, agent_id)
        .or_else(|| resolve_model_primary(global_model))
}

/// Resolve the agent's model fallbacks override.
pub fn resolve_agent_model_fallbacks_override(
    agents_list: &[serde_json::Value],
    agent_id: &str,
) -> Option<Vec<String>> {
    let config = resolve_agent_config(agents_list, agent_id)?;
    let model_value = config.model.as_ref()?;

    match model_value {
        serde_json::Value::String(_) => None,
        serde_json::Value::Object(obj) => {
            if !obj.contains_key("fallbacks") {
                return None;
            }
            let fallbacks = obj.get("fallbacks")?;
            if let serde_json::Value::Array(arr) = fallbacks {
                Some(
                    arr.iter()
                        .filter_map(|v| v.as_str().map(|s| s.to_string()))
                        .collect(),
                )
            } else {
                None
            }
        }
        _ => None,
    }
}

/// Resolve the agent's skills filter.
pub fn resolve_agent_skills_filter(
    agents_list: &[serde_json::Value],
    agent_id: &str,
) -> Option<Vec<String>> {
    let config = resolve_agent_config(agents_list, agent_id)?;
    config.skills
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn normalize_agent_id_basic() {
        assert_eq!(normalize_agent_id("main"), "main");
        assert_eq!(normalize_agent_id("  Main  "), "main");
        assert_eq!(normalize_agent_id(""), DEFAULT_AGENT_ID);
        assert_eq!(normalize_agent_id("my-agent_1"), "my-agent_1");
    }

    #[test]
    fn normalize_agent_id_invalid_chars() {
        assert_eq!(normalize_agent_id("my agent!"), "my-agent");
        assert_eq!(normalize_agent_id("---"), DEFAULT_AGENT_ID);
    }

    #[test]
    fn list_agent_ids_empty() {
        assert_eq!(list_agent_ids(&[]), vec!["main"]);
    }

    #[test]
    fn list_agent_ids_deduplicates() {
        let list = vec![
            json!({"id": "alpha"}),
            json!({"id": "beta"}),
            json!({"id": "Alpha"}),
        ];
        let ids = list_agent_ids(&list);
        assert_eq!(ids, vec!["alpha", "beta"]);
    }

    #[test]
    fn resolve_default_agent_id_empty() {
        assert_eq!(resolve_default_agent_id(&[]), "main");
    }

    #[test]
    fn resolve_default_agent_id_uses_default_flag() {
        let list = vec![
            json!({"id": "alpha"}),
            json!({"id": "beta", "default": true}),
        ];
        assert_eq!(resolve_default_agent_id(&list), "beta");
    }

    #[test]
    fn resolve_default_agent_id_first_entry_fallback() {
        let list = vec![json!({"id": "alpha"}), json!({"id": "beta"})];
        assert_eq!(resolve_default_agent_id(&list), "alpha");
    }

    #[test]
    fn resolve_session_agent_ids_explicit() {
        let list = vec![json!({"id": "alpha"})];
        let result = resolve_session_agent_ids(&list, None, Some("beta"));
        assert_eq!(result.session_agent_id, "beta");
    }

    #[test]
    fn resolve_session_agent_ids_from_key() {
        let list = vec![json!({"id": "alpha"})];
        let result = resolve_session_agent_ids(&list, Some("agent:beta:main"), None);
        assert_eq!(result.session_agent_id, "beta");
    }

    #[test]
    fn resolve_session_agent_ids_default_fallback() {
        let list = vec![json!({"id": "alpha"})];
        let result = resolve_session_agent_ids(&list, None, None);
        assert_eq!(result.session_agent_id, "alpha");
    }

    #[test]
    fn resolve_agent_config_found() {
        let list = vec![
            json!({"id": "alpha", "name": "Alpha Agent", "workspace": "/tmp/alpha"}),
        ];
        let config = resolve_agent_config(&list, "alpha").unwrap();
        assert_eq!(config.name.as_deref(), Some("Alpha Agent"));
        assert_eq!(config.workspace.as_deref(), Some("/tmp/alpha"));
    }

    #[test]
    fn resolve_agent_config_not_found() {
        let list = vec![json!({"id": "alpha"})];
        assert!(resolve_agent_config(&list, "beta").is_none());
    }

    #[test]
    fn resolve_explicit_model_primary_string() {
        let list = vec![json!({"id": "alpha", "model": "claude-opus-4-6"})];
        assert_eq!(
            resolve_agent_explicit_model_primary(&list, "alpha"),
            Some("claude-opus-4-6".to_string())
        );
    }

    #[test]
    fn resolve_explicit_model_primary_object() {
        let list = vec![json!({"id": "alpha", "model": {"primary": "claude-sonnet-4-6"}})];
        assert_eq!(
            resolve_agent_explicit_model_primary(&list, "alpha"),
            Some("claude-sonnet-4-6".to_string())
        );
    }

    #[test]
    fn resolve_model_fallbacks_override() {
        let list = vec![
            json!({"id": "alpha", "model": {"primary": "m1", "fallbacks": ["m2", "m3"]}}),
        ];
        assert_eq!(
            resolve_agent_model_fallbacks_override(&list, "alpha"),
            Some(vec!["m2".to_string(), "m3".to_string()])
        );
    }

    #[test]
    fn resolve_model_fallbacks_override_string_model() {
        let list = vec![json!({"id": "alpha", "model": "claude-opus-4-6"})];
        assert_eq!(resolve_agent_model_fallbacks_override(&list, "alpha"), None);
    }

    #[test]
    fn parse_agent_id_from_session_key_valid() {
        assert_eq!(
            parse_agent_id_from_session_key("agent:mybot:main"),
            Some("mybot".to_string())
        );
    }

    #[test]
    fn parse_agent_id_from_session_key_invalid() {
        assert_eq!(parse_agent_id_from_session_key("not-agent-key"), None);
        assert_eq!(parse_agent_id_from_session_key("agent::main"), None);
    }
}
