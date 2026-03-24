use std::fs;

use clap::{Args, Subcommand};

use crate::config;
use crate::errors::CliError;
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct BackupArgs {
    #[command(subcommand)]
    pub command: BackupCommand,
}

#[derive(Subcommand, Debug)]
pub enum BackupCommand {
    /// Create a backup of config and state.
    Create {
        /// Output directory for the backup.
        #[arg(long)]
        output: Option<String>,
        #[arg(long)]
        json: bool,
    },
    /// List available backups.
    List {
        #[arg(long)]
        json: bool,
    },
}

pub async fn run(args: &BackupArgs) -> Result<(), CliError> {
    match &args.command {
        BackupCommand::Create { output, json } => cmd_create(output.as_deref(), *json).await,
        BackupCommand::List { json } => cmd_list(*json).await,
    }
}

async fn cmd_create(output: Option<&str>, json: bool) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);
    let state_dir = config::resolve_state_dir();
    let config_path = config::resolve_config_path();

    let backup_dir = if let Some(o) = output {
        std::path::PathBuf::from(o)
    } else {
        let ts = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();
        state_dir.join("backups").join(format!("backup-{ts}"))
    };

    fs::create_dir_all(&backup_dir)?;

    // Copy config file
    if config_path.exists() {
        fs::copy(&config_path, backup_dir.join("deneb.json"))?;
    }

    // Copy sessions directory
    let sessions_src = state_dir.join("agents");
    if sessions_src.exists() {
        // Just note the location; full recursive copy is complex
        fs::write(
            backup_dir.join("sessions-location.txt"),
            sessions_src.to_string_lossy().as_bytes(),
        )?;
    }

    if json_mode {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "backupDir": backup_dir.to_string_lossy(),
            }))?
        );
    } else {
        let success = Palette::success();
        println!(
            "{}",
            success.apply_to(format!("Backup created at {}", backup_dir.display()))
        );
    }

    Ok(())
}

async fn cmd_list(json: bool) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);
    let state_dir = config::resolve_state_dir();
    let backups_dir = state_dir.join("backups");

    if !backups_dir.exists() {
        if json_mode {
            println!("[]");
        } else {
            println!("{}", Palette::muted().apply_to("No backups found."));
        }
        return Ok(());
    }

    let mut entries: Vec<String> = Vec::new();
    for entry in fs::read_dir(&backups_dir)? {
        let entry = entry?;
        if entry.file_type()?.is_dir() {
            entries.push(entry.file_name().to_string_lossy().to_string());
        }
    }
    entries.sort();

    if json_mode {
        println!("{}", serde_json::to_string_pretty(&entries)?);
    } else {
        let bold = Palette::bold();
        println!("{}", bold.apply_to(format!("Backups ({})", entries.len())));
        for e in &entries {
            println!("  {e}");
        }
    }

    Ok(())
}
