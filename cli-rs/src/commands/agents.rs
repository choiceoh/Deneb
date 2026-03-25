use clap::{Args, Subcommand};

use crate::config;
use crate::errors::CliError;
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct AgentsArgs {
    #[command(subcommand)]
    pub command: AgentsCommand,
}

#[derive(Subcommand, Debug)]
pub enum AgentsCommand {
    /// List configured agents.
    List {
        /// Output JSON.
        #[arg(long)]
        json: bool,
    },

    /// Add a new agent.
    Add {
        /// Agent name (used to derive the agent ID).
        name: Option<String>,

        /// Workspace directory.
        #[arg(long)]
        workspace: Option<String>,

        /// Model ID.
        #[arg(long)]
        model: Option<String>,

        /// Agent state directory.
        #[arg(long)]
        agent_dir: Option<String>,

        /// Route binding in channel[:accountId] format (repeatable).
        #[arg(long = "bind")]
        bindings: Vec<String>,

        /// Skip interactive prompts.
        #[arg(long)]
        non_interactive: bool,

        /// Output JSON.
        #[arg(long)]
        json: bool,
    },

    /// Delete an agent.
    Delete {
        /// Agent ID to delete.
        id: String,

        /// Skip confirmation prompt.
        #[arg(long)]
        force: bool,

        /// Output JSON.
        #[arg(long)]
        json: bool,
    },

    /// Add route bindings to an agent.
    Bind {
        /// Agent ID (defaults to "default").
        #[arg(long, default_value = "default")]
        agent: String,

        /// Binding in channel[:accountId] format (repeatable).
        #[arg(long = "bind")]
        bindings: Vec<String>,

        /// Output JSON.
        #[arg(long)]
        json: bool,
    },

    /// Remove route bindings from an agent.
    Unbind {
        /// Agent ID.
        #[arg(long, default_value = "default")]
        agent: String,

        /// Binding to remove in channel[:accountId] format (repeatable).
        #[arg(long = "bind")]
        bindings: Vec<String>,

        /// Remove all bindings for this agent.
        #[arg(long)]
        all: bool,

        /// Output JSON.
        #[arg(long)]
        json: bool,
    },
}

pub async fn run(args: &AgentsArgs) -> Result<(), CliError> {
    match &args.command {
        AgentsCommand::List { json } => cmd_list(*json).await,
        AgentsCommand::Add {
            name,
            workspace,
            model,
            agent_dir,
            bindings,
            non_interactive,
            json,
        } => {
            cmd_add(
                name.as_deref(),
                workspace.as_deref(),
                model.as_deref(),
                agent_dir.as_deref(),
                bindings,
                *non_interactive,
                *json,
            )
            .await
        }
        AgentsCommand::Delete { id, force, json } => cmd_delete(id, *force, *json).await,
        AgentsCommand::Bind {
            agent,
            bindings,
            json,
        } => cmd_bind(agent, bindings, *json).await,
        AgentsCommand::Unbind {
            agent,
            bindings,
            all,
            json,
        } => cmd_unbind(agent, bindings, *all, *json).await,
    }
}

async fn cmd_list(json: bool) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);
    let config_path = config::resolve_config_path();
    let config = config::load_config_best_effort(&config_path);

    // Extract agents from config extra fields
    let agents_value = config
        .extra
        .get("agents")
        .and_then(|a| a.as_object())
        .and_then(|a| a.get("list"))
        .cloned()
        .unwrap_or(serde_json::json!([]));

    let agents = agents_value.as_array().cloned().unwrap_or_default();

    if json_mode {
        println!("{}", serde_json::to_string_pretty(&agents)?);
        return Ok(());
    }

    let bold = Palette::bold();
    let muted = Palette::muted();

    if agents.is_empty() {
        println!("{}", muted.apply_to("No agents configured."));
        return Ok(());
    }

    println!(
        "{}",
        bold.apply_to(format!("Agents ({} configured)", agents.len()))
    );

    for agent in &agents {
        let id = agent
            .get("id")
            .and_then(|v| v.as_str())
            .unwrap_or("unknown");
        let name = agent.get("name").and_then(|v| v.as_str());
        let model = agent.get("model").and_then(|v| v.as_str());
        let is_default = agent
            .get("isDefault")
            .and_then(|v| v.as_bool())
            .unwrap_or(false);

        let accent = Palette::accent();
        let label = if is_default {
            format!("{id} (default)")
        } else {
            id.to_string()
        };
        println!("  {}", accent.apply_to(&label));

        if let Some(name) = name {
            println!("    Name: {}", muted.apply_to(name));
        }
        if let Some(model) = model {
            println!("    Model: {}", muted.apply_to(model));
        }
    }

    Ok(())
}

async fn cmd_add(
    name: Option<&str>,
    workspace: Option<&str>,
    model: Option<&str>,
    agent_dir: Option<&str>,
    bindings: &[String],
    non_interactive: bool,
    json: bool,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);

    // In non-interactive mode, name and workspace are required
    let agent_name = if let Some(n) = name {
        n.to_string()
    } else if non_interactive {
        return Err(CliError::User(
            "agent name is required in non-interactive mode".to_string(),
        ));
    } else {
        // Interactive prompt
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

    // Normalize ID from name: lowercase, replace spaces/underscores with hyphens
    let agent_id = agent_name.to_lowercase().replace([' ', '_'], "-");

    // Load config and check for duplicates
    let config_path = config::resolve_config_path();
    let mut cfg = config::load_config(&config_path).unwrap_or_default();

    let agents_list = get_agents_list_mut(&mut cfg);

    // Check uniqueness
    if agents_list.iter().any(|a| {
        a.get("id")
            .and_then(|v| v.as_str())
            .is_some_and(|id| id == agent_id)
    }) {
        return Err(CliError::User(format!(
            "agent with ID '{agent_id}' already exists"
        )));
    }

    // Build agent entry
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

    // Add bindings if provided
    if !bindings.is_empty() {
        add_bindings_to_config(&mut cfg, &agent_id, bindings)?;
        config::write_config(&config_path, &cfg)?;
    }

    if json_mode {
        println!("{}", serde_json::to_string_pretty(&entry)?);
    } else {
        let success = Palette::success();
        println!("{}", success.apply_to(format!("Agent '{agent_id}' added.")));
    }

    Ok(())
}

async fn cmd_delete(id: &str, force: bool, json: bool) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);
    let config_path = config::resolve_config_path();
    let mut cfg = config::load_config(&config_path).unwrap_or_default();

    let agents_list = get_agents_list_mut(&mut cfg);

    // Find agent
    let idx = agents_list.iter().position(|a| {
        a.get("id")
            .and_then(|v| v.as_str())
            .is_some_and(|i| i == id)
    });

    let idx = match idx {
        Some(i) => i,
        None => {
            return Err(CliError::User(format!("agent '{id}' not found")));
        }
    };

    // Check if default
    if agents_list[idx]
        .get("isDefault")
        .and_then(|v| v.as_bool())
        .unwrap_or(false)
    {
        return Err(CliError::User(
            "cannot delete the default agent".to_string(),
        ));
    }

    // Confirm
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

    // Also remove bindings for this agent
    remove_agent_bindings(&mut cfg, id);
    config::write_config(&config_path, &cfg)?;

    if json_mode {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({ "deleted": id }))?
        );
    } else {
        let success = Palette::success();
        println!("{}", success.apply_to(format!("Agent '{id}' deleted.")));
    }

    Ok(())
}

async fn cmd_bind(agent: &str, bindings: &[String], json: bool) -> Result<(), CliError> {
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
        println!("{}", serde_json::to_string_pretty(&added)?);
    } else {
        let success = Palette::success();
        println!(
            "{}",
            success.apply_to(format!(
                "Added {} binding(s) to agent '{agent}'.",
                added.len()
            ))
        );
    }

    Ok(())
}

async fn cmd_unbind(
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
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({ "removed": removed }))?
        );
    } else {
        let success = Palette::success();
        println!(
            "{}",
            success.apply_to(format!(
                "Removed {removed} binding(s) from agent '{agent}'."
            ))
        );
    }

    Ok(())
}

// --- Config helpers ---

/// Get or create the agents.list array in config.extra.
fn get_agents_list_mut(cfg: &mut config::DenebConfig) -> &mut Vec<serde_json::Value> {
    let agents = cfg
        .extra
        .entry("agents".to_string())
        .or_insert_with(|| serde_json::json!({"list": []}));

    if agents.get("list").is_none() {
        agents["list"] = serde_json::json!([]);
    }

    // Safety: we just ensured the key exists and is an array
    agents
        .get_mut("list")
        .unwrap()
        .as_array_mut()
        .expect("agents.list must be an array")
}

/// Parse a binding spec "channel[:accountId]" into a JSON object.
fn parse_binding(agent_id: &str, spec: &str) -> serde_json::Value {
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

/// Add bindings to config.extra["routing"]["bindings"].
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
        .unwrap()
        .as_array_mut()
        .expect("routing.bindings must be an array");

    let mut added = Vec::new();
    for spec in specs {
        let binding = parse_binding(agent_id, spec);
        bindings_arr.push(binding.clone());
        added.push(binding);
    }

    Ok(added)
}

/// Remove all bindings for an agent. Returns the count removed.
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

/// Remove specific bindings matching the given specs.
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_binding_with_account() {
        let b = parse_binding("my-agent", "discord:123456");
        assert_eq!(b["agentId"], "my-agent");
        assert_eq!(b["channel"], "discord");
        assert_eq!(b["accountId"], "123456");
    }

    #[test]
    fn parse_binding_without_account() {
        let b = parse_binding("my-agent", "telegram");
        assert_eq!(b["agentId"], "my-agent");
        assert_eq!(b["channel"], "telegram");
        assert!(b.get("accountId").is_none());
    }

    #[test]
    fn normalize_agent_id() {
        let name = "My Cool Agent";
        let id = name.to_lowercase().replace([' ', '_'], "-");
        assert_eq!(id, "my-cool-agent");
    }
}
