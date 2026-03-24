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
        call_gateway(CallOptions {
            url: gw.url.clone(),
            token: gw.token.clone(),
            password: gw.password.clone(),
            method: method.to_string(),
            params: Some(params),
            timeout_ms: gw.timeout,
            expect_final: false,
        }),
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
        call_gateway(CallOptions {
            url: gw.url.clone(),
            token: gw.token.clone(),
            password: gw.password.clone(),
            method: method.to_string(),
            params: Some(params),
            timeout_ms: gw.timeout,
            expect_final: false,
        }),
    )
    .await?;
    if json_mode {
        println!("{}", serde_json::to_string_pretty(&result)?);
    } else {
        println!("{}", Palette::success().apply_to(success_msg));
    }
    Ok(())
}
