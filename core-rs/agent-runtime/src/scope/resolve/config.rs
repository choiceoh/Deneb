//! Agent registry and configuration resolution logic.
//!
//! Mirrors `src/agents/agent-scope.ts` and `src/config/model-input.ts`
//! (pure-logic subset). Keep in sync.

use serde::{Deserialize, Serialize};

use super::agent_ids::{normalize_agent_id, DEFAULT_AGENT_ID};
use super::session_keys::parse_agent_session_key;

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
    /// Memory search configuration.
    pub memory_search: Option<serde_json::Value>,
    /// Human delay configuration.
    pub human_delay: Option<serde_json::Value>,
    /// Heartbeat configuration.
    pub heartbeat: Option<serde_json::Value>,
    /// Identity/persona configuration.
    pub identity: Option<serde_json::Value>,
    /// Group chat configuration.
    pub group_chat: Option<serde_json::Value>,
    /// Subagents configuration.
    pub subagents: Option<serde_json::Value>,
    /// Sandbox configuration.
    pub sandbox: Option<serde_json::Value>,
    /// Tools configuration.
    pub tools: Option<serde_json::Value>,
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
    pub memory_search: Option<serde_json::Value>,
    pub human_delay: Option<serde_json::Value>,
    pub heartbeat: Option<serde_json::Value>,
    pub identity: Option<serde_json::Value>,
    pub group_chat: Option<serde_json::Value>,
    pub subagents: Option<serde_json::Value>,
    pub sandbox: Option<serde_json::Value>,
    pub tools: Option<serde_json::Value>,
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
        parse_agent_session_key(key)
            .map(|p| normalize_agent_id(&p.agent_id))
            .unwrap_or_else(|| default_agent_id.clone())
    } else {
        default_agent_id.clone()
    };

    SessionAgentIds {
        default_agent_id,
        session_agent_id,
    }
}

/// Resolve the fallback agent ID from explicit agent ID or session key.
pub fn resolve_fallback_agent_id(agent_id: Option<&str>, session_key: Option<&str>) -> String {
    let explicit = agent_id
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty());
    if let Some(id) = explicit {
        return normalize_agent_id(&id);
    }
    super::session_keys::resolve_agent_id_from_session_key(session_key.unwrap_or(""))
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
        memory_search: entry.memory_search,
        human_delay: entry.human_delay,
        heartbeat: entry.heartbeat,
        identity: entry.identity,
        group_chat: entry.group_chat,
        subagents: entry.subagents,
        sandbox: entry.sandbox,
        tools: entry.tools,
    })
}

/// Resolve the primary model string for an agent from its config entry.
fn resolve_model_primary(raw: Option<&serde_json::Value>) -> Option<String> {
    match raw? {
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

/// Resolve only the session agent ID (convenience wrapper).
pub fn resolve_session_agent_id(
    agents_list: &[serde_json::Value],
    session_key: Option<&str>,
) -> String {
    resolve_session_agent_ids(agents_list, session_key, None).session_agent_id
}

/// Backward-compatible alias for `resolve_agent_explicit_model_primary`.
pub fn resolve_agent_model_primary(
    agents_list: &[serde_json::Value],
    agent_id: &str,
) -> Option<String> {
    resolve_agent_explicit_model_primary(agents_list, agent_id)
}

/// Extract fallback model values from a model config value.
/// Mirrors `src/config/model-input.ts#resolveAgentModelFallbackValues`. Keep in sync.
pub fn resolve_agent_model_fallback_values(model: Option<&serde_json::Value>) -> Vec<String> {
    let val = match model {
        Some(v) => v,
        None => return Vec::new(),
    };
    match val {
        serde_json::Value::Object(obj) => match obj.get("fallbacks") {
            Some(serde_json::Value::Array(arr)) => arr
                .iter()
                .filter_map(|v| v.as_str().map(|s| s.trim().to_string()))
                .filter(|s| !s.is_empty())
                .collect(),
            _ => Vec::new(),
        },
        _ => Vec::new(),
    }
}

/// Resolve model fallbacks override for a run context.
/// Uses explicit agent ID or session key to find the agent, then checks its fallbacks.
pub fn resolve_run_model_fallbacks_override(
    agents_list: &[serde_json::Value],
    agent_id: Option<&str>,
    session_key: Option<&str>,
) -> Option<Vec<String>> {
    let id = resolve_fallback_agent_id(agent_id, session_key);
    resolve_agent_model_fallbacks_override(agents_list, &id)
}

/// Check if any model fallbacks are configured (agent-level or global defaults).
pub fn has_configured_model_fallbacks(
    agents_list: &[serde_json::Value],
    global_model: Option<&serde_json::Value>,
    agent_id: Option<&str>,
    session_key: Option<&str>,
) -> bool {
    let fallbacks_override =
        resolve_run_model_fallbacks_override(agents_list, agent_id, session_key);
    let default_fallbacks = resolve_agent_model_fallback_values(global_model);
    let effective = fallbacks_override.unwrap_or(default_fallbacks);
    !effective.is_empty()
}

/// Resolve the effective model fallbacks, considering agent overrides and session state.
pub fn resolve_effective_model_fallbacks(
    agents_list: &[serde_json::Value],
    agent_id: &str,
    global_model: Option<&serde_json::Value>,
    has_session_model_override: bool,
) -> Option<Vec<String>> {
    let agent_fallbacks_override = resolve_agent_model_fallbacks_override(agents_list, agent_id);
    if !has_session_model_override {
        return agent_fallbacks_override;
    }
    let default_fallbacks = resolve_agent_model_fallback_values(global_model);
    Some(agent_fallbacks_override.unwrap_or(default_fallbacks))
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

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
    fn resolve_agent_config_found() -> Result<(), Box<dyn std::error::Error>> {
        let list = vec![json!({"id": "alpha", "name": "Alpha Agent", "workspace": "/tmp/alpha"})];
        let config =
            resolve_agent_config(&list, "alpha").ok_or("resolve_agent_config returned None")?;
        assert_eq!(config.name.as_deref(), Some("Alpha Agent"));
        assert_eq!(config.workspace.as_deref(), Some("/tmp/alpha"));
        Ok(())
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
        let list =
            vec![json!({"id": "alpha", "model": {"primary": "m1", "fallbacks": ["m2", "m3"]}})];
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
    fn resolve_fallback_agent_id_basic() {
        assert_eq!(resolve_fallback_agent_id(Some("mybot"), None), "mybot");
        assert_eq!(
            resolve_fallback_agent_id(None, Some("agent:mybot:main")),
            "mybot"
        );
        assert_eq!(resolve_fallback_agent_id(None, None), DEFAULT_AGENT_ID);
    }

    #[test]
    fn resolve_session_agent_id_basic() {
        let list = vec![json!({"id": "alpha"})];
        assert_eq!(
            resolve_session_agent_id(&list, Some("agent:beta:main")),
            "beta"
        );
        assert_eq!(resolve_session_agent_id(&list, None), "alpha");
    }

    #[test]
    fn resolve_agent_model_primary_alias() {
        let list = vec![json!({"id": "alpha", "model": "claude-opus-4-6"})];
        assert_eq!(
            resolve_agent_model_primary(&list, "alpha"),
            Some("claude-opus-4-6".to_string())
        );
    }

    #[test]
    fn resolve_agent_model_fallback_values_basic() {
        let model = json!({"primary": "m1", "fallbacks": ["m2", "m3"]});
        assert_eq!(
            resolve_agent_model_fallback_values(Some(&model)),
            vec!["m2".to_string(), "m3".to_string()]
        );
    }

    #[test]
    fn resolve_agent_model_fallback_values_string() {
        let model = json!("claude-opus-4-6");
        assert!(resolve_agent_model_fallback_values(Some(&model)).is_empty());
    }

    #[test]
    fn resolve_agent_model_fallback_values_none() {
        assert!(resolve_agent_model_fallback_values(None).is_empty());
    }

    #[test]
    fn resolve_run_model_fallbacks_override_basic() {
        let list = vec![json!({"id": "alpha", "model": {"primary": "m1", "fallbacks": ["m2"]}})];
        assert_eq!(
            resolve_run_model_fallbacks_override(&list, Some("alpha"), None),
            Some(vec!["m2".to_string()])
        );
    }

    #[test]
    fn has_configured_model_fallbacks_agent_level() {
        let list = vec![json!({"id": "alpha", "model": {"primary": "m1", "fallbacks": ["m2"]}})];
        assert!(has_configured_model_fallbacks(
            &list,
            None,
            Some("alpha"),
            None
        ));
    }

    #[test]
    fn has_configured_model_fallbacks_global() {
        let list: Vec<serde_json::Value> = vec![];
        let global = json!({"primary": "m1", "fallbacks": ["m2"]});
        assert!(has_configured_model_fallbacks(
            &list,
            Some(&global),
            None,
            None
        ));
    }

    #[test]
    fn has_configured_model_fallbacks_none() {
        let list: Vec<serde_json::Value> = vec![];
        assert!(!has_configured_model_fallbacks(&list, None, None, None));
    }

    #[test]
    fn resolve_effective_model_fallbacks_no_override() {
        let list = vec![json!({"id": "alpha", "model": {"primary": "m1", "fallbacks": ["m2"]}})];
        assert_eq!(
            resolve_effective_model_fallbacks(&list, "alpha", None, false),
            Some(vec!["m2".to_string()])
        );
    }

    #[test]
    fn resolve_effective_model_fallbacks_with_session_override() {
        let list = vec![json!({"id": "alpha", "model": "m1"})];
        let global = json!({"primary": "m1", "fallbacks": ["g1", "g2"]});
        assert_eq!(
            resolve_effective_model_fallbacks(&list, "alpha", Some(&global), true),
            Some(vec!["g1".to_string(), "g2".to_string()])
        );
    }
}
