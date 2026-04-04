use crate::config;
use crate::errors::CliError;
use crate::terminal::is_json_mode;

use super::render;
use super::validate::{get_agents_list_mut, normalize_agent_id, parse_binding};

pub(super) async fn cmd_add(
    name: Option<&str>,
    workspace: Option<&str>,
    model: Option<&str>,
    agent_dir: Option<&str>,
    bindings: &[String],
    non_interactive: bool,
    json: bool,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);

    let agent_name = if let Some(n) = name {
        n.to_string()
    } else if non_interactive {
        return Err(CliError::User(
            "agent name is required in non-interactive mode".to_string(),
        ));
    } else {
        dialoguer::Input::<String>::new()
            .with_prompt("Agent name")
            .interact_text()
            .map_err(|e| CliError::User(format!("prompt failed: {e}")))?
    };

    let workspace_path = if let Some(w) = workspace {
        w.to_string()
    } else if non_interactive {
        return Err(CliError::User(
            "--workspace is required in non-interactive mode".to_string(),
        ));
    } else {
        dialoguer::Input::<String>::new()
            .with_prompt("Workspace directory")
            .interact_text()
            .map_err(|e| CliError::User(format!("prompt failed: {e}")))?
    };

    let agent_id = normalize_agent_id(&agent_name);

    let config_path = config::resolve_config_path();
    let mut cfg = config::load_config(&config_path).unwrap_or_default();

    let agents_list = get_agents_list_mut(&mut cfg);

    if agents_list.iter().any(|a| {
        a.get("id")
            .and_then(|v| v.as_str())
            .is_some_and(|id| id == agent_id)
    }) {
        return Err(CliError::User(format!(
            "agent with ID '{agent_id}' already exists"
        )));
    }

    let mut entry = serde_json::json!({
        "id": agent_id,
        "name": agent_name,
        "workspace": workspace_path,
    });
    if let Some(m) = model {
        entry["model"] = serde_json::json!(m);
    }
    if let Some(ad) = agent_dir {
        entry["agentDir"] = serde_json::json!(ad);
    }

    agents_list.push(entry.clone());
    config::write_config(&config_path, &cfg)?;

    if !bindings.is_empty() {
        add_bindings_to_config(&mut cfg, &agent_id, bindings)?;
        config::write_config(&config_path, &cfg)?;
    }

    if json_mode {
        render::print_json(&entry)?;
    } else {
        render::print_success(&format!("Agent '{agent_id}' added"));
    }

    Ok(())
}

pub(super) async fn cmd_delete(id: &str, force: bool, json: bool) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);
    let config_path = config::resolve_config_path();
    let mut cfg = config::load_config(&config_path).unwrap_or_default();

    let agents_list = get_agents_list_mut(&mut cfg);

    let idx = agents_list.iter().position(|a| {
        a.get("id")
            .and_then(|v| v.as_str())
            .is_some_and(|i| i == id)
    });

    let Some(idx) = idx else {
        return Err(CliError::User(format!("agent '{id}' not found")));
    };

    if agents_list[idx]
        .get("isDefault")
        .and_then(serde_json::Value::as_bool)
        .unwrap_or(false)
    {
        return Err(CliError::User(
            "cannot delete the default agent".to_string(),
        ));
    }

    if !force {
        let confirmed = dialoguer::Confirm::new()
            .with_prompt(format!("Delete agent '{id}'?"))
            .default(false)
            .interact()
            .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;

        if !confirmed {
            println!("Cancelled.");
            return Ok(());
        }
    }

    agents_list.remove(idx);
    config::write_config(&config_path, &cfg)?;

    remove_agent_bindings(&mut cfg, id);
    config::write_config(&config_path, &cfg)?;

    if json_mode {
        render::print_json(&serde_json::json!({ "deleted": id }))?;
    } else {
        render::print_success(&format!("Agent '{id}' deleted"));
    }

    Ok(())
}

pub(super) async fn cmd_bind(agent: &str, bindings: &[String], json: bool) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);

    if bindings.is_empty() {
        return Err(CliError::User(
            "at least one --bind is required".to_string(),
        ));
    }

    let config_path = config::resolve_config_path();
    let mut cfg = config::load_config(&config_path).unwrap_or_default();

    let added = add_bindings_to_config(&mut cfg, agent, bindings)?;
    config::write_config(&config_path, &cfg)?;

    if json_mode {
        render::print_json(&added)?;
    } else {
        render::print_success(&format!(
            "Added {} binding(s) to agent '{agent}'",
            added.len()
        ));
    }

    Ok(())
}

pub(super) async fn cmd_unbind(
    agent: &str,
    bindings: &[String],
    all: bool,
    json: bool,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);

    if !all && bindings.is_empty() {
        return Err(CliError::User(
            "either --bind or --all is required".to_string(),
        ));
    }

    let config_path = config::resolve_config_path();
    let mut cfg = config::load_config(&config_path).unwrap_or_default();

    let removed = if all {
        remove_agent_bindings(&mut cfg, agent)
    } else {
        remove_specific_bindings(&mut cfg, agent, bindings)
    };

    config::write_config(&config_path, &cfg)?;

    if json_mode {
        render::print_json(&serde_json::json!({ "removed": removed }))?;
    } else {
        render::print_success(&format!(
            "Removed {removed} binding(s) from agent '{agent}'"
        ));
    }

    Ok(())
}

fn add_bindings_to_config(
    cfg: &mut config::DenebConfig,
    agent_id: &str,
    specs: &[String],
) -> Result<Vec<serde_json::Value>, CliError> {
    let routing = cfg
        .extra
        .entry("routing".to_string())
        .or_insert_with(|| serde_json::json!({"bindings": []}));

    if routing.get("bindings").is_none() {
        routing["bindings"] = serde_json::json!([]);
    }

    let bindings_arr = routing
        .get_mut("bindings")
        .unwrap_or_else(|| unreachable!("bindings key was just inserted"))
        .as_array_mut()
        .unwrap_or_else(|| unreachable!("bindings was initialized as a JSON array"));

    let mut added = Vec::new();
    for spec in specs {
        let binding = parse_binding(agent_id, spec);
        bindings_arr.push(binding.clone());
        added.push(binding);
    }

    Ok(added)
}

fn remove_agent_bindings(cfg: &mut config::DenebConfig, agent_id: &str) -> usize {
    let Some(routing) = cfg.extra.get_mut("routing") else {
        return 0;
    };
    let Some(bindings_arr) = routing.get_mut("bindings").and_then(|b| b.as_array_mut()) else {
        return 0;
    };

    let before = bindings_arr.len();
    bindings_arr.retain(|b| {
        b.get("agentId")
            .and_then(|v| v.as_str())
            .is_none_or(|id| id != agent_id)
    });
    before - bindings_arr.len()
}

fn remove_specific_bindings(
    cfg: &mut config::DenebConfig,
    agent_id: &str,
    specs: &[String],
) -> usize {
    let Some(routing) = cfg.extra.get_mut("routing") else {
        return 0;
    };
    let Some(bindings_arr) = routing.get_mut("bindings").and_then(|b| b.as_array_mut()) else {
        return 0;
    };

    let parsed: Vec<serde_json::Value> = specs.iter().map(|s| parse_binding(agent_id, s)).collect();

    let before = bindings_arr.len();
    bindings_arr.retain(|b| {
        !parsed.iter().any(|p| {
            b.get("agentId") == p.get("agentId")
                && b.get("channel") == p.get("channel")
                && (p.get("accountId").is_none() || b.get("accountId") == p.get("accountId"))
        })
    });
    before - bindings_arr.len()
}
