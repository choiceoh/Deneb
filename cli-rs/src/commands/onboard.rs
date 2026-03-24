use std::fs;

use clap::Args;

use crate::config;
use crate::errors::CliError;
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct OnboardArgs {
    /// Workspace directory.
    #[arg(long)]
    pub workspace: Option<String>,

    /// Gateway mode: local or remote.
    #[arg(long, value_parser = ["local", "remote"])]
    pub mode: Option<String>,

    /// Remote gateway WebSocket URL.
    #[arg(long)]
    pub remote_url: Option<String>,

    /// Remote gateway token.
    #[arg(long)]
    pub remote_token: Option<String>,

    /// Gateway port.
    #[arg(long)]
    pub gateway_port: Option<u16>,

    /// Gateway bind mode.
    #[arg(long, value_parser = ["loopback", "private", "public"])]
    pub gateway_bind: Option<String>,

    /// Gateway auth mode.
    #[arg(long, value_parser = ["none", "token", "password"])]
    pub gateway_auth: Option<String>,

    /// API token for the model provider.
    #[arg(long)]
    pub token: Option<String>,

    /// Token provider ID (e.g. anthropic, openai).
    #[arg(long)]
    pub token_provider: Option<String>,

    /// Reset config before running.
    #[arg(long)]
    pub reset: bool,

    /// Run without interactive prompts.
    #[arg(long)]
    pub non_interactive: bool,

    /// Acknowledge risk (required for non-interactive).
    #[arg(long)]
    pub accept_risk: bool,

    /// Output JSON.
    #[arg(long)]
    pub json: bool,
}

pub async fn run(args: &OnboardArgs) -> Result<(), CliError> {
    let json_mode = is_json_mode(args.json);
    let config_path = config::resolve_config_path();
    let state_dir = config::resolve_state_dir();

    // Non-interactive requires --accept-risk
    if args.non_interactive && !args.accept_risk {
        return Err(CliError::User(
            "--accept-risk is required in non-interactive mode".to_string(),
        ));
    }

    // Reset config if requested
    if args.reset && config_path.exists() {
        fs::remove_file(&config_path)?;
    }

    // Load or create config
    let mut cfg = config::load_config_best_effort(&config_path);

    // Step 1: Gateway mode
    let mode = if let Some(ref m) = args.mode {
        m.clone()
    } else if args.non_interactive {
        "local".to_string()
    } else {
        let items = vec!["local", "remote"];
        let selection = dialoguer::Select::new()
            .with_prompt("Gateway mode")
            .items(&items)
            .default(0)
            .interact()
            .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;
        items[selection].to_string()
    };

    // Ensure gateway config exists
    config::set_config_value(&mut cfg, "gateway.mode", serde_json::json!(&mode))?;

    // Step 2: Remote URL (if remote mode)
    if mode == "remote" {
        let remote_url = if let Some(ref url) = args.remote_url {
            url.clone()
        } else if args.non_interactive {
            return Err(CliError::User(
                "--remote-url is required for remote mode in non-interactive mode".to_string(),
            ));
        } else {
            dialoguer::Input::<String>::new()
                .with_prompt("Remote gateway URL (wss://...)")
                .interact_text()
                .map_err(|e| CliError::User(format!("prompt failed: {e}")))?
        };
        config::set_config_value(
            &mut cfg,
            "gateway.remote.url",
            serde_json::json!(remote_url),
        )?;

        if let Some(ref token) = args.remote_token {
            config::set_config_value(&mut cfg, "gateway.auth.token", serde_json::json!(token))?;
        }
    }

    // Step 3: Gateway port (local mode)
    if mode == "local" {
        if let Some(port) = args.gateway_port {
            config::set_config_value(&mut cfg, "gateway.port", serde_json::json!(port))?;
        } else if !args.non_interactive {
            let port_str: String = dialoguer::Input::new()
                .with_prompt("Gateway port")
                .default("18789".to_string())
                .interact_text()
                .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;
            if let Ok(port) = port_str.parse::<u16>() {
                config::set_config_value(&mut cfg, "gateway.port", serde_json::json!(port))?;
            }
        }

        if let Some(ref bind) = args.gateway_bind {
            config::set_config_value(&mut cfg, "gateway.bind", serde_json::json!(bind))?;
        }

        if let Some(ref auth) = args.gateway_auth {
            if auth == "token" {
                // Generate a token if not provided
                let token = uuid::Uuid::new_v4().to_string();
                config::set_config_value(&mut cfg, "gateway.auth.token", serde_json::json!(token))?;
            }
        }
    }

    // Step 4: Model provider token
    if let Some(ref token) = args.token {
        let provider = args.token_provider.as_deref().unwrap_or("anthropic");
        let key = format!("providers.{provider}.apiKey");
        config::set_config_value(&mut cfg, &key, serde_json::json!(token))?;
    } else if !args.non_interactive {
        let providers = vec!["anthropic", "openai", "google", "ollama", "skip"];
        let selection = dialoguer::Select::new()
            .with_prompt("Model provider")
            .items(&providers)
            .default(0)
            .interact()
            .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;

        if providers[selection] != "skip" {
            let provider = providers[selection];
            let api_key: String = dialoguer::Input::new()
                .with_prompt(format!("{provider} API key"))
                .interact_text()
                .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;
            if !api_key.is_empty() {
                let key = format!("providers.{provider}.apiKey");
                config::set_config_value(&mut cfg, &key, serde_json::json!(api_key))?;
            }
        }
    }

    // Step 5: Workspace
    let workspace = if let Some(ref w) = args.workspace {
        std::path::PathBuf::from(w)
    } else {
        state_dir.join("workspace")
    };
    fs::create_dir_all(&workspace)?;

    let agents = cfg
        .extra
        .entry("agents".to_string())
        .or_insert_with(|| serde_json::json!({"defaults": {}}));
    if agents.get("defaults").is_none() {
        agents["defaults"] = serde_json::json!({});
    }
    agents["defaults"]["workspace"] = serde_json::json!(workspace.to_string_lossy().to_string());

    // Ensure sessions dir
    let sessions_dir = state_dir.join("agents").join("default").join("sessions");
    fs::create_dir_all(&sessions_dir)?;

    // Record wizard metadata
    let now = chrono_like_now();
    cfg.extra
        .entry("wizard".to_string())
        .or_insert_with(|| serde_json::json!({}));
    if let Some(wizard) = cfg.extra.get_mut("wizard") {
        wizard["lastRunAt"] = serde_json::json!(now);
        wizard["lastRunCommand"] = serde_json::json!("onboard");
        wizard["lastRunMode"] = serde_json::json!(&mode);
    }

    // Write config
    config::write_config(&config_path, &cfg)?;

    if json_mode {
        let out = serde_json::json!({
            "configPath": config_path.to_string_lossy(),
            "mode": mode,
            "workspace": workspace.to_string_lossy(),
        });
        println!("{}", serde_json::to_string_pretty(&out)?);
    } else {
        let success = Palette::success();
        let muted = Palette::muted();
        println!("{}", success.apply_to("Onboarding complete."));
        println!("  Mode: {}", muted.apply_to(&mode));
        println!(
            "  Config: {}",
            muted.apply_to(config_path.to_string_lossy())
        );
        println!(
            "  Workspace: {}",
            muted.apply_to(workspace.to_string_lossy())
        );
        println!(
            "\n  Run {} to start the gateway.",
            Palette::accent().apply_to("deneb gateway run")
        );
    }

    Ok(())
}

/// Simple ISO-8601-like timestamp without pulling in chrono.
fn chrono_like_now() -> String {
    let dur = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default();
    // Return Unix timestamp as string (good enough without chrono)
    format!("{}", dur.as_secs())
}
