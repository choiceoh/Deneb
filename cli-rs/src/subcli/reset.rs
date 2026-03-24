use std::fs;

use clap::Args;

use crate::config;
use crate::errors::CliError;
use crate::terminal::Palette;

#[derive(Args, Debug)]
pub struct ResetArgs {
    /// Reset scope: config, sessions, or all.
    #[arg(long, default_value = "config")]
    pub scope: String,

    /// Skip confirmation.
    #[arg(long)]
    pub force: bool,

    /// Output JSON.
    #[arg(long)]
    pub json: bool,
}

pub async fn run(args: &ResetArgs) -> Result<(), CliError> {
    if !args.force {
        let confirmed = dialoguer::Confirm::new()
            .with_prompt(format!("Reset {} data? This cannot be undone.", args.scope))
            .default(false)
            .interact()
            .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;

        if !confirmed {
            println!("Cancelled.");
            return Ok(());
        }
    }

    let config_path = config::resolve_config_path();
    let state_dir = config::resolve_state_dir();
    let mut removed = Vec::new();

    match args.scope.as_str() {
        "config" => {
            if config_path.exists() {
                fs::remove_file(&config_path)?;
                removed.push("config");
            }
        }
        "sessions" => {
            let sessions = state_dir.join("agents");
            if sessions.exists() {
                fs::remove_dir_all(&sessions)?;
                removed.push("sessions");
            }
        }
        "all" => {
            if config_path.exists() {
                fs::remove_file(&config_path)?;
                removed.push("config");
            }
            let sessions = state_dir.join("agents");
            if sessions.exists() {
                fs::remove_dir_all(&sessions)?;
                removed.push("sessions");
            }
        }
        other => {
            return Err(CliError::User(format!(
                "Unknown scope: {other}. Use config, sessions, or all."
            )));
        }
    }

    if args.json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({"removed": removed}))?
        );
    } else {
        let success = Palette::success();
        if removed.is_empty() {
            println!("Nothing to reset.");
        } else {
            println!(
                "{}",
                success.apply_to(format!("Reset complete: {}", removed.join(", ")))
            );
        }
    }

    Ok(())
}
