use clap::Args;

use crate::config::{self, DenebConfig};
use crate::errors::CliError;
use crate::gateway::{call_gateway_with_config, CallOptions};
use crate::terminal::{is_json_mode, Palette};

const SECTIONS: &[(&str, &str)] = &[
    ("workspace", "Workspace & sessions directory"),
    ("model", "Model provider & credentials"),
    ("gateway", "Gateway port, bind, auth"),
    ("channels", "Channel status check"),
    ("health", "Run gateway health check"),
];

#[derive(Args, Debug)]
pub struct ConfigureArgs {
    /// Configuration section(s) to edit.
    #[arg(long, value_parser = ["workspace", "model", "gateway", "channels", "health"])]
    pub section: Vec<String>,

    /// Run without prompts (apply defaults).
    #[arg(long)]
    pub non_interactive: bool,

    /// Output JSON.
    #[arg(long)]
    pub json: bool,

    /// Gateway WebSocket URL override.
    #[arg(long)]
    pub url: Option<String>,

    /// Gateway auth token.
    #[arg(long)]
    pub token: Option<String>,

    /// Gateway password.
    #[arg(long)]
    pub password: Option<String>,

    /// Timeout in milliseconds.
    #[arg(long, default_value = "10000")]
    pub timeout: u64,
}

pub async fn run(args: &ConfigureArgs) -> Result<(), CliError> {
    let json_mode = is_json_mode(args.json);

    // Determine which sections to run
    let sections = if args.section.is_empty() {
        if args.non_interactive {
            return Err(CliError::User(
                "--section is required in non-interactive mode".to_string(),
            ));
        }
        // Interactive: let user pick sections
        let labels: Vec<&str> = SECTIONS.iter().map(|(_, desc)| *desc).collect();
        let selections = dialoguer::MultiSelect::new()
            .with_prompt("Select sections to configure")
            .items(&labels)
            .interact()
            .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;

        if selections.is_empty() {
            println!("No sections selected.");
            return Ok(());
        }

        selections
            .iter()
            .map(|&i| SECTIONS[i].0.to_string())
            .collect::<Vec<_>>()
    } else {
        args.section.clone()
    };

    let config_path = config::resolve_config_path();
    let mut cfg = config::load_config_best_effort(&config_path);
    let mut modified = false;

    for section in &sections {
        match section.as_str() {
            "workspace" => {
                modified |= configure_workspace(&mut cfg, args.non_interactive)?;
            }
            "model" => {
                modified |= configure_model(&mut cfg, args.non_interactive)?;
            }
            "gateway" => {
                modified |= configure_gateway(&mut cfg, args.non_interactive)?;
            }
            "channels" => {
                run_channels_check(args, json_mode, &cfg).await?;
            }
            "health" => {
                run_health_check(args, json_mode, &cfg).await?;
            }
            _ => {
                eprintln!("Unknown section: {section}");
            }
        }
    }

    if modified {
        config::write_config(&config_path, &cfg)?;
        if !json_mode {
            let success = Palette::success();
            println!("{}", success.apply_to("Configuration updated."));
        }
    }

    Ok(())
}

fn configure_workspace(
    cfg: &mut config::DenebConfig,
    non_interactive: bool,
) -> Result<bool, CliError> {
    let bold = Palette::bold();
    println!("\n{}", bold.apply_to("Workspace"));

    let state_dir = config::resolve_state_dir();
    let default_workspace = state_dir.join("workspace");

    let workspace = if non_interactive {
        default_workspace.to_string_lossy().to_string()
    } else {
        dialoguer::Input::<String>::new()
            .with_prompt("Workspace directory")
            .default(default_workspace.to_string_lossy().to_string())
            .interact_text()
            .map_err(|e| CliError::User(format!("prompt failed: {e}")))?
    };

    std::fs::create_dir_all(&workspace)?;

    let agents = cfg
        .extra
        .entry("agents".to_string())
        .or_insert_with(|| serde_json::json!({"defaults": {}}));
    if agents.get("defaults").is_none() {
        agents["defaults"] = serde_json::json!({});
    }
    agents["defaults"]["workspace"] = serde_json::json!(workspace);

    Ok(true)
}

fn configure_model(cfg: &mut config::DenebConfig, non_interactive: bool) -> Result<bool, CliError> {
    let bold = Palette::bold();
    println!("\n{}", bold.apply_to("Model Provider"));

    if non_interactive {
        return Ok(false);
    }

    let providers = vec!["anthropic", "openai", "google", "ollama", "skip"];
    let selection = dialoguer::Select::new()
        .with_prompt("Provider")
        .items(&providers)
        .default(0)
        .interact()
        .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;

    if providers[selection] == "skip" {
        return Ok(false);
    }

    let provider = providers[selection];
    let api_key: String = dialoguer::Input::new()
        .with_prompt(format!("{provider} API key"))
        .interact_text()
        .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;

    if api_key.is_empty() {
        return Ok(false);
    }

    let key = format!("providers.{provider}.apiKey");
    config::set_config_value(cfg, &key, serde_json::json!(api_key))?;

    Ok(true)
}

fn configure_gateway(
    cfg: &mut config::DenebConfig,
    non_interactive: bool,
) -> Result<bool, CliError> {
    let bold = Palette::bold();
    println!("\n{}", bold.apply_to("Gateway"));

    if non_interactive {
        return Ok(false);
    }

    // Port
    let current_port = cfg.gateway_port().unwrap_or(config::DEFAULT_GATEWAY_PORT);
    let port_str: String = dialoguer::Input::new()
        .with_prompt("Gateway port")
        .default(current_port.to_string())
        .interact_text()
        .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;

    if let Ok(port) = port_str.parse::<u16>() {
        config::set_config_value(cfg, "gateway.port", serde_json::json!(port))?;
    }

    // Bind mode
    let bind_modes = vec!["loopback", "private", "public"];
    let current_bind = cfg
        .gateway
        .as_ref()
        .and_then(|g| g.bind.as_deref())
        .unwrap_or("loopback");
    let default_idx = bind_modes
        .iter()
        .position(|&b| b == current_bind)
        .unwrap_or(0);
    let selection = dialoguer::Select::new()
        .with_prompt("Bind mode")
        .items(&bind_modes)
        .default(default_idx)
        .interact()
        .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;
    config::set_config_value(
        cfg,
        "gateway.bind",
        serde_json::json!(bind_modes[selection]),
    )?;

    // Auth token generation
    let gen_token = dialoguer::Confirm::new()
        .with_prompt("Generate a gateway auth token?")
        .default(cfg.auth_token().is_none())
        .interact()
        .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;

    if gen_token {
        let token = uuid::Uuid::new_v4().to_string();
        config::set_config_value(cfg, "gateway.auth.token", serde_json::json!(&token))?;
        let muted = Palette::muted();
        println!("  Token: {}", muted.apply_to(&token));
    }

    Ok(true)
}

async fn run_channels_check(
    args: &ConfigureArgs,
    json_mode: bool,
    cfg: &DenebConfig,
) -> Result<(), CliError> {
    let bold = Palette::bold();
    println!("\n{}", bold.apply_to("Channel Status"));

    let result = crate::terminal::progress::with_spinner(
        "Checking channels...",
        !json_mode,
        call_gateway_with_config(
            CallOptions {
                url: args.url.clone(),
                token: args.token.clone(),
                password: args.password.clone(),
                method: "channels.status".to_string(),
                params: Some(serde_json::json!({"probe": true})),
                timeout_ms: args.timeout,
                expect_final: false,
            },
            cfg,
        ),
    )
    .await;

    match result {
        Ok(payload) => {
            if json_mode {
                println!("{}", serde_json::to_string_pretty(&payload)?);
            } else {
                let channel_accounts = payload.get("channelAccounts").and_then(|ca| ca.as_object());
                if let Some(channels) = channel_accounts {
                    let muted = Palette::muted();
                    for (channel, accounts) in channels {
                        let count = accounts.as_array().map(|a| a.len()).unwrap_or(0);
                        println!("  {}: {} account(s)", channel, muted.apply_to(count));
                    }
                } else {
                    println!("  No channels configured.");
                }
            }
        }
        Err(e) => {
            let warn = Palette::warn();
            eprintln!(
                "  {}",
                warn.apply_to(format!("Could not check channels: {}", e.user_message()))
            );
        }
    }

    Ok(())
}

async fn run_health_check(
    args: &ConfigureArgs,
    json_mode: bool,
    cfg: &DenebConfig,
) -> Result<(), CliError> {
    let bold = Palette::bold();
    println!("\n{}", bold.apply_to("Gateway Health"));

    let result = crate::terminal::progress::with_spinner(
        "Checking gateway...",
        !json_mode,
        call_gateway_with_config(
            CallOptions {
                url: args.url.clone(),
                token: args.token.clone(),
                password: args.password.clone(),
                method: "health".to_string(),
                params: None,
                timeout_ms: args.timeout,
                expect_final: false,
            },
            cfg,
        ),
    )
    .await;

    match result {
        Ok(payload) => {
            if json_mode {
                println!("{}", serde_json::to_string_pretty(&payload)?);
            } else {
                let success = Palette::success();
                println!("  {}", success.apply_to("Gateway is healthy."));
                if let Some(v) = payload.get("version").and_then(|v| v.as_str()) {
                    let muted = Palette::muted();
                    println!("  Version: {}", muted.apply_to(v));
                }
            }
        }
        Err(e) => {
            let warn = Palette::warn();
            eprintln!(
                "  {}",
                warn.apply_to(format!("Gateway unreachable: {}", e.user_message()))
            );
        }
    }

    Ok(())
}
