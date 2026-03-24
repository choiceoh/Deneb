use clap::Args;

use crate::errors::CliError;
use crate::gateway::{call_gateway, CallOptions};
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct LogsArgs {
    /// Max lines to return.
    #[arg(long, default_value = "200")]
    pub limit: u64,

    /// Output JSON lines.
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

pub async fn run(args: &LogsArgs) -> Result<(), CliError> {
    let json_mode = is_json_mode(args.json);

    let result = crate::terminal::progress::with_spinner(
        "Fetching logs...",
        !json_mode,
        call_gateway(CallOptions {
            url: args.url.clone(),
            token: args.token.clone(),
            password: args.password.clone(),
            method: "logs.tail".to_string(),
            params: Some(serde_json::json!({
                "limit": args.limit,
            })),
            timeout_ms: args.timeout,
            expect_final: false,
        }),
    )
    .await?;

    if json_mode {
        println!("{}", serde_json::to_string_pretty(&result)?);
        return Ok(());
    }

    // Parse and display log lines
    if let Some(lines) = result.get("lines").and_then(|v| v.as_array()) {
        let muted = Palette::muted();
        let error_style = Palette::error();
        let warn_style = Palette::warn();

        for line in lines {
            if let Some(obj) = line.as_object() {
                let ts = obj.get("ts").and_then(|v| v.as_str()).unwrap_or("");
                let level = obj.get("level").and_then(|v| v.as_str()).unwrap_or("info");
                let label = obj.get("label").and_then(|v| v.as_str()).unwrap_or("");
                let msg = obj
                    .get("msg")
                    .or_else(|| obj.get("message"))
                    .and_then(|v| v.as_str())
                    .unwrap_or("");

                let ts_short = if ts.len() > 19 { &ts[11..19] } else { ts };

                match level {
                    "error" => {
                        println!(
                            "{} {} {} {}",
                            muted.apply_to(ts_short),
                            error_style.apply_to("ERR"),
                            muted.apply_to(label),
                            msg
                        );
                    }
                    "warn" => {
                        println!(
                            "{} {} {} {}",
                            muted.apply_to(ts_short),
                            warn_style.apply_to("WRN"),
                            muted.apply_to(label),
                            msg
                        );
                    }
                    _ => {
                        println!(
                            "{} {} {} {}",
                            muted.apply_to(ts_short),
                            muted.apply_to("INF"),
                            muted.apply_to(label),
                            msg
                        );
                    }
                }
            } else if let Some(text) = line.as_str() {
                println!("{text}");
            }
        }

        if lines.is_empty() {
            println!("{}", muted.apply_to("No log lines found."));
        }
    } else {
        // Fallback: print raw
        println!("{}", serde_json::to_string_pretty(&result)?);
    }

    Ok(())
}
