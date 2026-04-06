use crate::config;

/// Get or create the agents.list array in config.extra.
pub(super) fn get_agents_list_mut(cfg: &mut config::DenebConfig) -> &mut Vec<serde_json::Value> {
    let agents = cfg
        .extra
        .entry("agents".to_string())
        .or_insert_with(|| serde_json::json!({"list": []}));

    if agents.get("list").is_none() {
        agents["list"] = serde_json::json!([]);
    }

    agents
        .get_mut("list")
        .unwrap_or_else(|| unreachable!("list key was just inserted"))
        .as_array_mut()
        .unwrap_or_else(|| unreachable!("list was initialized as a JSON array"))
}

/// Parse a binding spec "channel[:accountId]" into a JSON object.
pub(super) fn parse_binding(agent_id: &str, spec: &str) -> serde_json::Value {
    let mut parts = spec.splitn(2, ':');
    let channel = parts.next().unwrap_or(spec);
    let account_id = parts.next();

    let mut binding = serde_json::json!({
        "agentId": agent_id,
        "channel": channel,
    });

    if let Some(acct) = account_id {
        binding["accountId"] = serde_json::json!(acct);
    }

    binding
}

pub(super) fn normalize_agent_id(name: &str) -> String {
    name.to_lowercase().replace([' ', '_'], "-")
}
