use clap::Args;

use crate::errors::CliError;
use crate::gateway::{call_gateway, CallOptions};
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct HealthArgs {
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
}

pub async fn run(args: &HealthArgs) -> Result<(), CliError> {
    let json_mode = is_json_mode(args.json);

    let result = crate::terminal::progress::with_spinner(
        "Checking gateway health...",
        !json_mode,
        call_gateway(CallOptions {
            url: args.url.clone(),
            token: args.token.clone(),
            password: args.password.clone(),
            method: "health".to_string(),
            params: None,
            timeout_ms: args.timeout,
            expect_final: false,
        }),
    )
    .await;

    match result {
        Ok(payload) => {
            if json_mode {
                println!("{}", serde_json::to_string_pretty(&payload)?);
            } else {
                let ok_style = Palette::success();
                println!("{}", ok_style.apply_to("Gateway is healthy"));

                // Show basic info from payload if available
                if let Some(obj) = payload.as_object() {
                    if let Some(uptime) = obj.get("uptimeSeconds") {
                        let muted = Palette::muted();
                        println!("  Uptime: {}", muted.apply_to(format!("{}s", uptime)));
                    }
                    if let Some(version) = obj.get("version") {
                        let muted = Palette::muted();
                        println!(
                            "  Version: {}",
                            muted.apply_to(version.as_str().unwrap_or("unknown"))
                        );
                    }
                }
            }
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
            Err(e)
        }
    }
}
