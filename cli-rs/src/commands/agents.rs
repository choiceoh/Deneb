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
}

pub async fn run(args: &AgentsArgs) -> Result<(), CliError> {
    match &args.command {
        AgentsCommand::List { json } => cmd_list(*json).await,
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
