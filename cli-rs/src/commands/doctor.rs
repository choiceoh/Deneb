use clap::Args;

use crate::config::{self, DenebConfig};
use crate::errors::CliError;
use crate::gateway::{call_gateway_with_config, CallOptions};
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct DoctorArgs {
    /// Accept defaults without prompting.
    #[arg(long)]
    pub yes: bool,

    /// Apply recommended repairs without prompting.
    #[arg(long)]
    pub repair: bool,

    /// Apply aggressive repairs.
    #[arg(long)]
    pub force: bool,

    /// Run without prompts (safe migrations only).
    #[arg(long)]
    pub non_interactive: bool,

    /// Generate and configure a gateway auth token.
    #[arg(long)]
    pub generate_gateway_token: bool,

    /// Output JSON.
    #[arg(long)]
    pub json: bool,

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
}

struct DiagResult {
    section: String,
    status: DiagStatus,
    message: String,
    detail: Option<String>,
}

#[derive(Clone, Copy, PartialEq)]
enum DiagStatus {
    Ok,
    Warn,
    Error,
}

impl DiagStatus {
    fn label(self) -> &'static str {
        match self {
            DiagStatus::Ok => "ok",
            DiagStatus::Warn => "warn",
            DiagStatus::Error => "error",
        }
    }
}

pub async fn run(args: &DoctorArgs) -> Result<(), CliError> {
    let json_mode = is_json_mode(args.json);
    let auto_repair = args.repair || args.force;

    // Load config once; pass it to all checks so they don't re-resolve paths.
    let config_path = config::resolve_config_path();
    let mut results: Vec<DiagResult> = Vec::new();

    // 1. Config check — returns the (possibly modified) config for reuse below.
    let cfg = check_config(&mut results, &config_path, args, auto_repair)?;

    // 2. Gateway health
    check_gateway_health(&mut results, args, &cfg).await;

    // 3. Channel status
    check_channels(&mut results, args, &cfg).await;

    // 4. Security checks
    check_security(&mut results, &cfg)?;

    // Output
    if json_mode {
        let json_results: Vec<serde_json::Value> = results
            .iter()
            .map(|r| {
                let mut obj = serde_json::json!({
                    "section": r.section,
                    "status": r.status.label(),
                    "message": r.message,
                });
                if let Some(ref detail) = r.detail {
                    obj["detail"] = serde_json::json!(detail);
                }
                obj
            })
            .collect();
        println!("{}", serde_json::to_string_pretty(&json_results)?);
    } else {
        use crate::terminal::Symbols;
        let bold = Palette::bold();
        println!();
        println!("  {}", bold.apply_to("Diagnostics"));
        println!();

        for r in &results {
            let (icon, style) = match r.status {
                DiagStatus::Ok => (Symbols::SUCCESS, Palette::success()),
                DiagStatus::Warn => (Symbols::WARNING, Palette::warn()),
                DiagStatus::Error => (Symbols::ERROR, Palette::error()),
            };
            println!(
                "    {}  {} {} {}",
                style.apply_to(icon),
                r.section,
                Palette::muted().apply_to(Symbols::BULLET),
                r.message
            );
            if let Some(ref detail) = r.detail {
                println!("       {}", Palette::muted().apply_to(detail));
            }
        }

        let errors = results
            .iter()
            .filter(|r| r.status == DiagStatus::Error)
            .count();
        let warns = results
            .iter()
            .filter(|r| r.status == DiagStatus::Warn)
            .count();
        println!();
        if errors == 0 && warns == 0 {
            println!(
                "    {}  {}",
                Palette::success().apply_to(Symbols::SUCCESS),
                Palette::success().apply_to("All checks passed")
            );
        } else {
            println!(
                "    {}",
                Palette::muted().apply_to(format!("{errors} error(s), {warns} warning(s)"))
            );
        }
        println!();
    }

    Ok(())
}

/// Check the config file and return the loaded config for reuse by callers.
/// Returns the default config if the file is missing (check recorded as Warn).
fn check_config(
    results: &mut Vec<DiagResult>,
    config_path: &std::path::Path,
    args: &DoctorArgs,
    auto_repair: bool,
) -> Result<DenebConfig, CliError> {
    // Config file exists
    if !config_path.exists() {
        results.push(DiagResult {
            section: "config".to_string(),
            status: DiagStatus::Warn,
            message: "Config file not found.".to_string(),
            detail: Some(format!(
                "Expected at {}. Run 'deneb setup' to create.",
                config_path.display()
            )),
        });
        return Ok(DenebConfig::default());
    }

    // Config parseable
    match config::load_config(config_path) {
        Ok(mut cfg) => {
            results.push(DiagResult {
                section: "config".to_string(),
                status: DiagStatus::Ok,
                message: "Config file is valid.".to_string(),
                detail: None,
            });

            // Check gateway port
            if cfg.gateway_port().is_none() {
                results.push(DiagResult {
                    section: "config".to_string(),
                    status: DiagStatus::Warn,
                    message: "Gateway port not configured (using default 18789).".to_string(),
                    detail: None,
                });
            }

            // Generate token if requested
            if args.generate_gateway_token && cfg.auth_token().is_none() {
                let token = uuid::Uuid::new_v4().to_string();
                config::set_config_value(
                    &mut cfg,
                    "gateway.auth.token",
                    serde_json::json!(&token),
                )?;
                config::write_config(config_path, &cfg)?;
                results.push(DiagResult {
                    section: "config".to_string(),
                    status: DiagStatus::Ok,
                    message: "Generated gateway auth token.".to_string(),
                    detail: Some(format!("Token: {token}")),
                });
            } else if cfg.auth_token().is_none() {
                let should_gen = auto_repair
                    || (!args.non_interactive
                        && dialoguer::Confirm::new()
                            .with_prompt("No gateway auth token. Generate one?")
                            .default(true)
                            .interact()
                            .unwrap_or(false));

                if should_gen {
                    let token = uuid::Uuid::new_v4().to_string();
                    config::set_config_value(
                        &mut cfg,
                        "gateway.auth.token",
                        serde_json::json!(&token),
                    )?;
                    config::write_config(config_path, &cfg)?;
                    results.push(DiagResult {
                        section: "security".to_string(),
                        status: DiagStatus::Ok,
                        message: "Generated gateway auth token.".to_string(),
                        detail: Some(format!("Token: {token}")),
                    });
                } else {
                    results.push(DiagResult {
                        section: "security".to_string(),
                        status: DiagStatus::Warn,
                        message: "No gateway auth token configured.".to_string(),
                        detail: Some(
                            "Run 'deneb doctor --generate-gateway-token' to fix.".to_string(),
                        ),
                    });
                }
            }

            Ok(cfg)
        }
        Err(e) => {
            results.push(DiagResult {
                section: "config".to_string(),
                status: DiagStatus::Error,
                message: format!("Config file is invalid: {}", e.user_message()),
                detail: Some(format!("Path: {}", config_path.display())),
            });
            Ok(DenebConfig::default())
        }
    }
}

async fn check_gateway_health(
    results: &mut Vec<DiagResult>,
    args: &DoctorArgs,
    cfg: &DenebConfig,
) {
    let result = crate::terminal::progress::with_spinner(
        "Checking gateway...",
        !is_json_mode(args.json),
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
            cfg,
        ),
    )
    .await;

    match result {
        Ok(payload) => {
            let version = payload
                .get("version")
                .and_then(|v| v.as_str())
                .unwrap_or("unknown");
            results.push(DiagResult {
                section: "gateway".to_string(),
                status: DiagStatus::Ok,
                message: "Gateway is reachable.".to_string(),
                detail: Some(format!("Version: {version}")),
            });
        }
        Err(e) => {
            results.push(DiagResult {
                section: "gateway".to_string(),
                status: DiagStatus::Error,
                message: "Gateway is not reachable.".to_string(),
                detail: Some(e.user_message()),
            });
        }
    }
}

async fn check_channels(results: &mut Vec<DiagResult>, args: &DoctorArgs, cfg: &DenebConfig) {
    let result = call_gateway_with_config(
        CallOptions {
            url: args.url.clone(),
            token: args.token.clone(),
            password: args.password.clone(),
            method: "telegram.status".to_string(),
            params: Some(serde_json::json!({"probe": true, "timeoutMs": 5000})),
            timeout_ms: args.timeout,
            expect_final: false,
        },
        cfg,
    )
    .await;

    match result {
        Ok(payload) => {
            let channel_accounts = payload.get("channelAccounts").and_then(|ca| ca.as_object());

            if let Some(channels) = channel_accounts {
                let total: usize = channels
                    .values()
                    .filter_map(|v| v.as_array())
                    .map(|a| a.len())
                    .sum();
                results.push(DiagResult {
                    section: "channels".to_string(),
                    status: DiagStatus::Ok,
                    message: format!("{} channel(s) with {} account(s).", channels.len(), total),
                    detail: None,
                });
            } else {
                results.push(DiagResult {
                    section: "channels".to_string(),
                    status: DiagStatus::Warn,
                    message: "No channels configured.".to_string(),
                    detail: None,
                });
            }
        }
        Err(_) => {
            // Gateway unreachable already reported above; skip duplicate
        }
    }
}

fn check_security(results: &mut Vec<DiagResult>, cfg: &DenebConfig) -> Result<(), CliError> {
    // Check bind mode
    let bind = cfg
        .gateway
        .as_ref()
        .and_then(|g| g.bind.as_deref())
        .unwrap_or("loopback");

    if bind == "public" {
        results.push(DiagResult {
            section: "security".to_string(),
            status: DiagStatus::Warn,
            message: "Gateway is bound to public interface.".to_string(),
            detail: Some("Consider using 'loopback' or 'private' for better security.".to_string()),
        });
    }

    // Check TLS on remote
    if cfg.is_remote_mode() && !cfg.tls_enabled() {
        if let Some(url) = cfg.remote_url() {
            if url.starts_with("ws://") {
                results.push(DiagResult {
                    section: "security".to_string(),
                    status: DiagStatus::Warn,
                    message: "Remote gateway uses insecure WebSocket (ws://).".to_string(),
                    detail: Some("Consider using wss:// with TLS enabled.".to_string()),
                });
            }
        }
    }

    Ok(())
}
