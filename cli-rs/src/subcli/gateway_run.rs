use std::process::Command;

use clap::Args;

use crate::config;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct GatewayRunArgs {
    /// Gateway port.
    #[arg(long)]
    pub port: Option<u16>,

    /// Bind mode: loopback, private, or public.
    #[arg(long)]
    pub bind: Option<String>,

    /// Force start even if another instance is running.
    #[arg(long)]
    pub force: bool,

    /// Run in foreground (don't daemonize).
    #[arg(long)]
    pub foreground: bool,
}

pub async fn run(args: &GatewayRunArgs) -> Result<(), CliError> {
    let config_path = config::resolve_config_path();
    let cfg = config::load_config_best_effort(&config_path);
    let port = args
        .port
        .or(cfg.gateway_port())
        .unwrap_or(config::DEFAULT_GATEWAY_PORT);

    // Build the Node.js gateway command
    let mut cmd = Command::new("deneb");
    cmd.arg("gateway").arg("run");
    cmd.arg("--port").arg(port.to_string());

    if let Some(ref bind) = args.bind {
        cmd.arg("--bind").arg(bind);
    }
    if args.force {
        cmd.arg("--force");
    }

    // Spawn the Node.js gateway process
    let status = cmd
        .stdin(std::process::Stdio::inherit())
        .stdout(std::process::Stdio::inherit())
        .stderr(std::process::Stdio::inherit())
        .status()
        .map_err(|e| {
            CliError::User(format!(
                "Failed to start gateway. Is 'deneb' (Node.js) installed? Error: {e}"
            ))
        })?;

    if !status.success() {
        return Err(CliError::User(format!(
            "Gateway exited with code {}",
            status.code().unwrap_or(-1)
        )));
    }

    Ok(())
}
