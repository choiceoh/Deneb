use std::fs;
use std::path::PathBuf;

use clap::Args;

use crate::config;
use crate::errors::CliError;
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct SetupArgs {
    /// Workspace directory for the default agent.
    #[arg(long)]
    pub workspace: Option<String>,

    /// Run the interactive onboarding wizard.
    #[arg(long)]
    pub wizard: bool,

    /// Run without prompts.
    #[arg(long)]
    pub non_interactive: bool,

    /// Output JSON.
    #[arg(long)]
    pub json: bool,
}

pub async fn run(args: &SetupArgs) -> Result<(), CliError> {
    let json_mode = is_json_mode(args.json);

    let config_path = config::resolve_config_path();
    let state_dir = config::resolve_state_dir();

    // Ensure state directory exists
    fs::create_dir_all(&state_dir)?;

    // Load or create config
    let mut cfg = config::load_config_best_effort(&config_path);

    // Resolve workspace
    let workspace = if let Some(ref w) = args.workspace {
        PathBuf::from(w)
    } else {
        state_dir.join("workspace")
    };

    // Ensure workspace directory exists
    fs::create_dir_all(&workspace)?;

    // Ensure sessions directory exists
    let sessions_dir = state_dir.join("agents").join("default").join("sessions");
    fs::create_dir_all(&sessions_dir)?;

    // Set workspace in config if not already set
    let agents = cfg
        .extra
        .entry("agents".to_string())
        .or_insert_with(|| serde_json::json!({"defaults": {}}));
    if agents.get("defaults").is_none() {
        agents["defaults"] = serde_json::json!({});
    }
    if agents["defaults"].get("workspace").is_none() {
        agents["defaults"]["workspace"] =
            serde_json::json!(workspace.to_string_lossy().to_string());
    }

    // Write config
    config::write_config(&config_path, &cfg)?;

    if json_mode {
        let out = serde_json::json!({
            "configPath": config_path.to_string_lossy(),
            "stateDir": state_dir.to_string_lossy(),
            "workspace": workspace.to_string_lossy(),
            "sessionsDir": sessions_dir.to_string_lossy(),
        });
        println!("{}", serde_json::to_string_pretty(&out)?);
    } else {
        use crate::terminal::Symbols;
        let success = Palette::success();
        let muted = Palette::muted();
        println!();
        println!(
            "    {}  {}",
            success.apply_to(Symbols::SUCCESS),
            success.apply_to("Setup complete")
        );
        println!();
        println!(
            "    Config      {}",
            muted.apply_to(config_path.to_string_lossy())
        );
        println!(
            "    State       {}",
            muted.apply_to(state_dir.to_string_lossy())
        );
        println!(
            "    Workspace   {}",
            muted.apply_to(workspace.to_string_lossy())
        );
        println!(
            "    Sessions    {}",
            muted.apply_to(sessions_dir.to_string_lossy())
        );
        println!();
    }

    Ok(())
}
