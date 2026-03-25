use clap::{Args, Subcommand};

use crate::config;
use crate::errors::CliError;
use crate::gateway::{call_gateway, call_gateway_with_config, CallOptions};
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct GatewayArgs {
    #[command(subcommand)]
    pub command: GatewayCommand,
}

#[derive(Subcommand, Debug)]
pub enum GatewayCommand {
    /// Show gateway status and probe reachability.
    Status {
        /// Output JSON.
        #[arg(long)]
        json: bool,

        /// Gateway WebSocket URL override.
        #[arg(long)]
        url: Option<String>,

        /// Gateway auth token.
        #[arg(long)]
        token: Option<String>,

        /// Gateway password.
        #[arg(long)]
        password: Option<String>,

        /// Timeout in milliseconds.
        #[arg(long, default_value = "10000")]
        timeout: u64,
    },

    /// Call a gateway RPC method directly.
    Call {
        /// RPC method name.
        method: String,

        /// JSON parameters.
        #[arg(long)]
        params: Option<String>,

        /// Output JSON.
        #[arg(long)]
        json: bool,

        /// Wait for final response (agent).
        #[arg(long)]
        expect_final: bool,

        /// Gateway WebSocket URL override.
        #[arg(long)]
        url: Option<String>,

        /// Gateway auth token.
        #[arg(long)]
        token: Option<String>,

        /// Gateway password.
        #[arg(long)]
        password: Option<String>,

        /// Timeout in milliseconds.
        #[arg(long, default_value = "10000")]
        timeout: u64,
    },

    /// Fetch usage cost summary.
    #[command(name = "usage-cost")]
    UsageCost {
        /// Number of days to summarize.
        #[arg(long, default_value = "30")]
        days: u32,

        /// Output JSON.
        #[arg(long)]
        json: bool,

        /// Gateway WebSocket URL override.
        #[arg(long)]
        url: Option<String>,

        /// Gateway auth token.
        #[arg(long)]
        token: Option<String>,

        /// Gateway password.
        #[arg(long)]
        password: Option<String>,

        /// Timeout in milliseconds.
        #[arg(long, default_value = "30000")]
        timeout: u64,
    },
}

pub async fn run(args: &GatewayArgs) -> Result<(), CliError> {
    match &args.command {
        GatewayCommand::Status {
            json,
            url,
            token,
            password,
            timeout,
        } => {
            cmd_status(
                *json,
                url.as_deref(),
                token.as_deref(),
                password.as_deref(),
                *timeout,
            )
            .await
        }
        GatewayCommand::Call {
            method,
            params,
            json,
            expect_final,
            url,
            token,
            password,
            timeout,
        } => {
            cmd_call(CmdCallParams {
                method,
                params_str: params.as_deref(),
                json: *json,
                expect_final: *expect_final,
                url: url.as_deref(),
                token: token.as_deref(),
                password: password.as_deref(),
                timeout: *timeout,
            })
            .await
        }
        GatewayCommand::UsageCost {
            days,
            json,
            url,
            token,
            password,
            timeout,
        } => {
            cmd_usage_cost(
                *days,
                *json,
                url.as_deref(),
                token.as_deref(),
                password.as_deref(),
                *timeout,
            )
            .await
        }
    }
}

async fn cmd_status(
    json: bool,
    url: Option<&str>,
    token: Option<&str>,
    password: Option<&str>,
    timeout: u64,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);
    let config_path = config::resolve_config_path();
    let cfg = config::load_config_best_effort(&config_path);
    let port = config::resolve_gateway_port(cfg.gateway_port());

    let result = crate::terminal::progress::with_spinner(
        "Probing gateway...",
        !json_mode,
        call_gateway_with_config(
            CallOptions {
                url: url.map(|s| s.to_string()),
                token: token.map(|s| s.to_string()),
                password: password.map(|s| s.to_string()),
                method: "health".to_string(),
                params: None,
                timeout_ms: timeout,
                expect_final: false,
            },
            &cfg,
        ),
    )
    .await;

    match result {
        Ok(payload) => {
            if json_mode {
                let mut out = serde_json::json!({ "ok": true, "port": port });
                if let Some(obj) = payload.as_object() {
                    for (k, v) in obj {
                        out[k] = v.clone();
                    }
                }
                println!("{}", serde_json::to_string_pretty(&out)?);
            } else {
                let success = Palette::success();
                let muted = Palette::muted();
                println!("{}", success.apply_to("Gateway is running"));
                println!("  Port: {}", muted.apply_to(port));
                if let Some(v) = payload.get("version").and_then(|v| v.as_str()) {
                    println!("  Version: {}", muted.apply_to(v));
                }
                if let Some(u) = payload.get("uptimeSeconds").and_then(|v| v.as_f64()) {
                    println!("  Uptime: {}", muted.apply_to(format!("{:.0}s", u)));
                }
            }
            Ok(())
        }
        Err(e) => {
            if json_mode {
                let out =
                    serde_json::json!({ "ok": false, "port": port, "error": e.user_message() });
                println!("{}", serde_json::to_string_pretty(&out)?);
                std::process::exit(1);
            }
            let error = Palette::error();
            let muted = Palette::muted();
            eprintln!("{}", error.apply_to("Gateway is not reachable"));
            eprintln!("  Port: {}", muted.apply_to(port));
            eprintln!("  Error: {}", muted.apply_to(e.user_message()));
            std::process::exit(1);
        }
    }
}

struct CmdCallParams<'a> {
    method: &'a str,
    params_str: Option<&'a str>,
    json: bool,
    expect_final: bool,
    url: Option<&'a str>,
    token: Option<&'a str>,
    password: Option<&'a str>,
    timeout: u64,
}

async fn cmd_call(p: CmdCallParams<'_>) -> Result<(), CliError> {
    let params: Option<serde_json::Value> = match p.params_str {
        Some(s) => Some(
            serde_json::from_str(s)
                .map_err(|e| CliError::User(format!("invalid JSON params: {e}")))?,
        ),
        None => None,
    };

    let result = call_gateway(CallOptions {
        url: p.url.map(|s| s.to_string()),
        token: p.token.map(|s| s.to_string()),
        password: p.password.map(|s| s.to_string()),
        method: p.method.to_string(),
        params,
        timeout_ms: p.timeout,
        expect_final: p.expect_final,
    })
    .await?;

    if is_json_mode(p.json) || result.is_object() || result.is_array() {
        println!("{}", serde_json::to_string_pretty(&result)?);
    } else {
        println!("{result}");
    }

    Ok(())
}

async fn cmd_usage_cost(
    days: u32,
    json: bool,
    url: Option<&str>,
    token: Option<&str>,
    password: Option<&str>,
    timeout: u64,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);

    let result = crate::terminal::progress::with_spinner(
        "Fetching usage cost...",
        !json_mode,
        call_gateway(CallOptions {
            url: url.map(|s| s.to_string()),
            token: token.map(|s| s.to_string()),
            password: password.map(|s| s.to_string()),
            method: "usage.cost".to_string(),
            params: Some(serde_json::json!({ "days": days })),
            timeout_ms: timeout,
            expect_final: false,
        }),
    )
    .await?;

    if json_mode {
        println!("{}", serde_json::to_string_pretty(&result)?);
    } else {
        let bold = Palette::bold();
        let muted = Palette::muted();
        println!(
            "{}",
            bold.apply_to(format!("Usage Cost (last {days} days)"))
        );

        if let Some(obj) = result.as_object() {
            if let Some(total) = obj.get("totalCost").and_then(|v| v.as_f64()) {
                println!("  Total: {}", muted.apply_to(format!("${total:.4}")));
            }
            if let Some(tokens) = obj.get("totalTokens").and_then(|v| v.as_u64()) {
                println!("  Tokens: {}", muted.apply_to(format!("{tokens}")));
            }
        }
    }

    Ok(())
}
