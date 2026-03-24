use clap::Args;

use crate::config;
use crate::errors::CliError;
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct SecurityArgs {
    /// Run deep audit (scan system services).
    #[arg(long)]
    pub deep: bool,

    /// Output JSON.
    #[arg(long)]
    pub json: bool,
}

pub async fn run(args: &SecurityArgs) -> Result<(), CliError> {
    let json_mode = is_json_mode(args.json);
    let config_path = config::resolve_config_path();
    let cfg = config::load_config_best_effort(&config_path);

    let mut findings: Vec<serde_json::Value> = Vec::new();

    // Check auth token
    if cfg.auth_token().is_none() {
        findings.push(serde_json::json!({
            "severity": "warning",
            "check": "auth.token",
            "message": "No gateway auth token configured."
        }));
    }

    // Check bind mode
    let bind = cfg
        .gateway
        .as_ref()
        .and_then(|g| g.bind.as_deref())
        .unwrap_or("loopback");
    if bind == "public" {
        findings.push(serde_json::json!({
            "severity": "warning",
            "check": "bind.public",
            "message": "Gateway bound to public interface."
        }));
    }

    // Check remote TLS
    if cfg.is_remote_mode() {
        if let Some(url) = cfg.remote_url() {
            if url.starts_with("ws://") {
                findings.push(serde_json::json!({
                    "severity": "warning",
                    "check": "remote.tls",
                    "message": "Remote gateway uses insecure ws:// (no TLS)."
                }));
            }
        }
    }

    // Check config file permissions (unix only)
    #[cfg(unix)]
    {
        use std::os::unix::fs::MetadataExt;
        if let Ok(meta) = std::fs::metadata(&config_path) {
            let mode = meta.mode() & 0o777;
            if mode & 0o077 != 0 {
                findings.push(serde_json::json!({
                    "severity": "warning",
                    "check": "config.permissions",
                    "message": format!("Config file permissions too open: {:o}", mode)
                }));
            }
        }
    }

    if json_mode {
        println!("{}", serde_json::to_string_pretty(&findings)?);
    } else {
        let bold = Palette::bold();
        println!("{}", bold.apply_to("Security Audit"));

        if findings.is_empty() {
            println!("{}", Palette::success().apply_to("  No issues found."));
        } else {
            for f in &findings {
                let severity = f["severity"].as_str().unwrap_or("info");
                let message = f["message"].as_str().unwrap_or("");
                let icon = match severity {
                    "warning" => Palette::warn().apply_to("!!"),
                    "error" => Palette::error().apply_to("xx"),
                    _ => Palette::muted().apply_to("--"),
                };
                println!("  [{icon}] {message}");
            }
        }
    }

    Ok(())
}
