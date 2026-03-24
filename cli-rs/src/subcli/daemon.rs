use clap::{Args, Subcommand};

use crate::errors::CliError;
use crate::terminal::Palette;

#[derive(Args, Debug)]
pub struct DaemonArgs {
    #[command(subcommand)]
    pub command: DaemonCommand,
}

#[derive(Subcommand, Debug)]
pub enum DaemonCommand {
    /// Start the gateway daemon.
    Start,
    /// Stop the gateway daemon.
    Stop,
    /// Show daemon status.
    Status,
    /// Restart the daemon.
    Restart,
}

pub async fn run(args: &DaemonArgs) -> Result<(), CliError> {
    // Daemon management delegates to the Node.js CLI
    let subcmd = match &args.command {
        DaemonCommand::Start => "start",
        DaemonCommand::Stop => "stop",
        DaemonCommand::Status => "status",
        DaemonCommand::Restart => "restart",
    };

    let muted = Palette::muted();
    println!(
        "{}",
        muted.apply_to(format!("Delegating daemon {subcmd} to Node.js CLI..."))
    );

    let status = std::process::Command::new("deneb")
        .args(["daemon", subcmd])
        .stdin(std::process::Stdio::inherit())
        .stdout(std::process::Stdio::inherit())
        .stderr(std::process::Stdio::inherit())
        .status()
        .map_err(|e| CliError::User(format!("Failed to run daemon command: {e}")))?;

    if !status.success() {
        return Err(CliError::User(format!(
            "Daemon command exited with code {}",
            status.code().unwrap_or(-1)
        )));
    }

    Ok(())
}
