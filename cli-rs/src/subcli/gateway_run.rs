use std::path::PathBuf;
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

    /// Log level: debug, info, warn, error.
    #[arg(long)]
    pub log_level: Option<String>,

    /// Force Node.js gateway (skip Go binary).
    #[arg(long)]
    pub legacy: bool,
}

pub async fn run(args: &GatewayRunArgs) -> Result<(), CliError> {
    let config_path = config::resolve_config_path();
    let cfg = config::load_config_best_effort(&config_path);
    let port = args
        .port
        .or(cfg.gateway_port())
        .unwrap_or(config::DEFAULT_GATEWAY_PORT);

    // Try Go gateway first (unless --legacy is set).
    if !args.legacy {
        if let Some(result) = try_go_gateway(args, port)? {
            return result;
        }
    }

    // Fallback: Node.js gateway.
    run_nodejs_gateway(args, port)
}

/// Attempt to locate and start the Go gateway binary.
/// Returns None if the binary is not found (caller should fall back to Node.js).
/// Returns Some(Ok(())) on successful exit, Some(Err(..)) on failure.
fn try_go_gateway(
    args: &GatewayRunArgs,
    port: u16,
) -> Result<Option<Result<(), CliError>>, CliError> {
    let go_binary = find_go_binary();
    let go_binary = match go_binary {
        Some(p) => p,
        None => return Ok(None),
    };

    // Locate the Node.js plugin host entry point for channel extensions.
    let plugin_host_entry = find_plugin_host_entry();

    let mut cmd = Command::new(&go_binary);
    cmd.arg("--port").arg(port.to_string());

    // Wire the plugin host if available.
    if let Some(ref entry) = plugin_host_entry {
        cmd.arg("--plugin-host-cmd")
            .arg(format!("node {}", entry.display()));
    }

    if let Some(ref bind) = args.bind {
        // Map CLI bind modes to Go gateway bind modes.
        let go_bind = match bind.as_str() {
            "private" => "lan",
            "public" => "all",
            other => other,
        };
        cmd.arg("--bind").arg(go_bind);
    }

    if !args.foreground {
        cmd.arg("--daemon");
    }

    if let Some(ref level) = args.log_level {
        cmd.arg("--log-level").arg(level);
    }

    eprintln!(
        "Starting Go gateway: {} (port {})",
        go_binary.display(),
        port
    );

    let status = cmd
        .stdin(std::process::Stdio::inherit())
        .stdout(std::process::Stdio::inherit())
        .stderr(std::process::Stdio::inherit())
        .status()
        .map_err(|e| {
            CliError::User(format!(
                "Failed to start Go gateway at '{}': {e}",
                go_binary.display()
            ))
        })?;

    if !status.success() {
        return Ok(Some(Err(CliError::User(format!(
            "Go gateway exited with code {}",
            status.code().unwrap_or(-1)
        )))));
    }

    Ok(Some(Ok(())))
}

/// Fall back to the Node.js gateway (legacy path).
fn run_nodejs_gateway(args: &GatewayRunArgs, port: u16) -> Result<(), CliError> {
    let mut cmd = Command::new("deneb");
    cmd.arg("gateway").arg("run");
    cmd.arg("--port").arg(port.to_string());

    if let Some(ref bind) = args.bind {
        cmd.arg("--bind").arg(bind);
    }
    if args.force {
        cmd.arg("--force");
    }

    eprintln!("Starting Node.js gateway (legacy fallback, port {port})");

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

/// Search for the Go gateway binary in standard locations.
fn find_go_binary() -> Option<PathBuf> {
    // 1. DENEB_GO_GATEWAY env override.
    if let Ok(p) = std::env::var("DENEB_GO_GATEWAY") {
        let path = PathBuf::from(p);
        if path.is_file() {
            return Some(path);
        }
    }

    // 2. Relative to the current executable (sibling in same dir).
    if let Ok(exe) = std::env::current_exe() {
        if let Some(dir) = exe.parent() {
            let candidate = dir.join("deneb-gateway");
            if candidate.is_file() {
                return Some(candidate);
            }
        }
    }

    // 3. dist/deneb-gateway relative to repo root (dev layout).
    // Walk up from the current exe to find the repo root by looking for Makefile.
    if let Ok(exe) = std::env::current_exe() {
        let mut dir = exe.parent().map(|p| p.to_path_buf());
        for _ in 0..5 {
            if let Some(ref d) = dir {
                let candidate = d.join("dist").join("deneb-gateway");
                if candidate.is_file() && d.join("Makefile").is_file() {
                    return Some(candidate);
                }
                dir = d.parent().map(|p| p.to_path_buf());
            }
        }
    }

    // 4. Check PATH.
    if let Ok(output) = Command::new("which")
        .arg("deneb-gateway")
        .output()
    {
        if output.status.success() {
            let path_str = String::from_utf8_lossy(&output.stdout).trim().to_string();
            if !path_str.is_empty() {
                return Some(PathBuf::from(path_str));
            }
        }
    }

    None
}

/// Search for the Node.js plugin host entry point.
fn find_plugin_host_entry() -> Option<PathBuf> {
    // 1. DENEB_PLUGIN_HOST_ENTRY env override.
    if let Ok(p) = std::env::var("DENEB_PLUGIN_HOST_ENTRY") {
        let path = PathBuf::from(p);
        if path.is_file() {
            return Some(path);
        }
    }

    // 2. dist/plugin-host/main.js relative to repo root.
    if let Ok(exe) = std::env::current_exe() {
        let mut dir = exe.parent().map(|p| p.to_path_buf());
        for _ in 0..5 {
            if let Some(ref d) = dir {
                let candidate = d.join("dist").join("plugin-host").join("main.js");
                if candidate.is_file() {
                    return Some(candidate);
                }
                dir = d.parent().map(|p| p.to_path_buf());
            }
        }
    }

    None
}
