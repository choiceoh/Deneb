use clap::Args;

use crate::config;
use crate::errors::CliError;
use crate::gateway::{call_gateway_with_config, CallOptions};
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct StatusArgs {
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

    /// Output JSON.
    #[arg(long)]
    pub json: bool,

    /// Show all details.
    #[arg(long)]
    pub all: bool,
}

pub async fn run(args: &StatusArgs) -> Result<(), CliError> {
    let json_mode = is_json_mode(args.json);
    let config_path = config::resolve_config_path();
    let config = config::load_config_best_effort(&config_path);

    // Call gateway health to get status
    let result = crate::terminal::progress::with_spinner(
        "Fetching status...",
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
            &config,
        ),
    )
    .await;

    match result {
        Ok(payload) => {
            if json_mode {
                println!("{}", serde_json::to_string_pretty(&payload)?);
                return Ok(());
            }

            print_status_summary(&payload, &config);
            Ok(())
        }
        Err(e) => {
            if json_mode {
                let err_json = serde_json::json!({
                    "ok": false,
                    "error": e.user_message(),
                });
                println!("{}", serde_json::to_string_pretty(&err_json)?);
                std::process::exit(1);
            }

            let error_style = Palette::error();
            eprintln!(
                "{} {}",
                error_style.apply_to("Gateway unreachable:"),
                e.user_message()
            );

            // Show config info even when gateway is down
            let muted = Palette::muted();
            let port = config::resolve_gateway_port(config.gateway_port());
            eprintln!("  Port: {}", muted.apply_to(port));
            eprintln!("  Config: {}", muted.apply_to(config_path.display()));
            eprintln!(
                "  State: {}",
                muted.apply_to(config::resolve_state_dir().display())
            );

            std::process::exit(1);
        }
    }
}

fn print_status_summary(payload: &serde_json::Value, config: &config::DenebConfig) {
    let bold = Palette::bold();
    let success = Palette::success();
    let muted = Palette::muted();

    println!("{}", bold.apply_to("Deneb Status"));
    println!("  Gateway: {}", success.apply_to("connected"));

    let port = config::resolve_gateway_port(config.gateway_port());
    println!("  Port: {}", muted.apply_to(port));

    let mode = config
        .gateway
        .as_ref()
        .and_then(|g| g.mode.as_deref())
        .unwrap_or("local");
    println!("  Mode: {}", muted.apply_to(mode));

    if let Some(obj) = payload.as_object() {
        if let Some(version) = obj.get("version").and_then(|v| v.as_str()) {
            println!("  Version: {}", muted.apply_to(version));
        }
        if let Some(uptime) = obj.get("uptimeSeconds").and_then(|v| v.as_f64()) {
            let hours = uptime / 3600.0;
            if hours >= 1.0 {
                println!("  Uptime: {}", muted.apply_to(format!("{hours:.1}h")));
            } else {
                let mins = uptime / 60.0;
                println!("  Uptime: {}", muted.apply_to(format!("{mins:.0}m")));
            }
        }
        if let Some(channels) = obj.get("channels").and_then(|v| v.as_object()) {
            println!(
                "  Channels: {}",
                muted.apply_to(format!("{} configured", channels.len()))
            );
        }
    }

    println!(
        "  Config: {}",
        muted.apply_to(config::resolve_config_path().display())
    );
}
