use std::future::Future;

use crate::errors::CliError;
use crate::gateway::{call_gateway, CallOptions};
use crate::terminal::{is_json_mode, Palette};

/// Common gateway connection flags shared by many sub-CLI commands.
#[derive(clap::Args, Debug, Clone)]
pub struct GatewayFlags {
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

impl GatewayFlags {
    /// Build a `CallOptions` for a given method and params.
    pub fn call_options(&self, method: &str, params: serde_json::Value) -> CallOptions {
        CallOptions {
            url: self.url.clone(),
            token: self.token.clone(),
            password: self.password.clone(),
            method: method.to_string(),
            params: Some(params),
            timeout_ms: self.timeout,
            expect_final: false,
        }
    }
}

/// Call a gateway RPC method and print the result (JSON or pretty text).
pub async fn rpc_print(
    method: &str,
    params: serde_json::Value,
    gw: &GatewayFlags,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(gw.json);
    let result = crate::terminal::progress::with_spinner(
        "Working...",
        !json_mode,
        call_gateway(gw.call_options(method, params)),
    )
    .await?;
    println!("{}", serde_json::to_string_pretty(&result)?);
    Ok(())
}

/// Call a gateway RPC method and print a success message (or JSON result).
pub async fn rpc_action(
    method: &str,
    params: serde_json::Value,
    gw: &GatewayFlags,
    success_msg: &str,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(gw.json);
    let result = crate::terminal::progress::with_spinner(
        "Working...",
        !json_mode,
        call_gateway(gw.call_options(method, params)),
    )
    .await?;
    if json_mode {
        println!("{}", serde_json::to_string_pretty(&result)?);
    } else {
        println!("{}", Palette::success().apply_to(success_msg));
    }
    Ok(())
}

/// Call a gateway RPC method with a custom output formatter.
/// Use this when a command needs table or rich pretty output beyond a raw JSON dump.
pub async fn rpc_print_fmt<F>(
    method: &str,
    params: serde_json::Value,
    gw: &GatewayFlags,
    spinner_text: &str,
    formatter: F,
) -> Result<(), CliError>
where
    F: FnOnce(serde_json::Value, bool) -> Result<(), CliError>,
{
    let json_mode = is_json_mode(gw.json);
    let result = crate::terminal::progress::with_spinner(
        spinner_text,
        !json_mode,
        call_gateway(gw.call_options(method, params)),
    )
    .await?;
    formatter(result, json_mode)
}

/// Call a gateway RPC method using a custom call function and formatter.
/// Use this when you need to inject a pre-loaded config (e.g. `call_gateway_with_config`).
/// In JSON mode, errors are serialized and the process exits with code 1 instead of propagating.
pub async fn rpc_query_custom<FCall, FCallFuture, FFormat>(
    gw: &GatewayFlags,
    method: &str,
    spinner_text: &str,
    gateway_call: FCall,
    formatter: FFormat,
) -> Result<(), CliError>
where
    FCall: FnOnce(CallOptions) -> FCallFuture,
    FCallFuture: Future<Output = Result<serde_json::Value, CliError>>,
    FFormat: FnOnce(serde_json::Value, bool) -> Result<(), CliError>,
{
    let json_mode = is_json_mode(gw.json);
    let result = crate::terminal::progress::with_spinner(
        spinner_text,
        !json_mode,
        gateway_call(gw.call_options(method, serde_json::json!({}))),
    )
    .await;

    match result {
        Ok(payload) => formatter(payload, json_mode),
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
