use std::future::Future;

use crate::errors::CliError;
use crate::gateway::CallOptions;
use crate::terminal::is_json_mode;

pub struct GatewayQueryArgs<'a> {
    pub url: &'a Option<String>,
    pub token: &'a Option<String>,
    pub password: &'a Option<String>,
    pub timeout: u64,
    pub json: bool,
}

impl GatewayQueryArgs<'_> {
    pub fn call_options(&self, method: &str) -> CallOptions {
        CallOptions {
            url: self.url.clone(),
            token: self.token.clone(),
            password: self.password.clone(),
            method: method.to_string(),
            params: None,
            timeout_ms: self.timeout,
            expect_final: false,
        }
    }
}

pub async fn run_gateway_query<FCall, FCallFuture, FFormat>(
    args: GatewayQueryArgs<'_>,
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
    let json_mode = is_json_mode(args.json);
    let result = crate::terminal::progress::with_spinner(
        spinner_text,
        !json_mode,
        gateway_call(args.call_options(method)),
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
