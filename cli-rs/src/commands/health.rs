use clap::Args;

use crate::commands::gateway_query::{run_gateway_query, GatewayQueryArgs};
use crate::errors::CliError;
use crate::gateway::call_gateway;
use crate::terminal::Palette;

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
    run_gateway_query(
        GatewayQueryArgs {
            url: &args.url,
            token: &args.token,
            password: &args.password,
            timeout: args.timeout,
            json: args.json,
        },
        "health",
        "Checking gateway health...",
        call_gateway,
        |payload, json_mode| {
            if json_mode {
                println!("{}", serde_json::to_string_pretty(&payload)?);
                return Ok(());
            }

            use crate::terminal::Symbols;
            let success = Palette::success();
            let muted = Palette::muted();
            println!();
            println!(
                "    {}  {}",
                success.apply_to(Symbols::SUCCESS),
                success.apply_to("Gateway healthy")
            );

            // Show basic info from payload if available
            if let Some(obj) = payload.as_object() {
                if let Some(uptime) = obj.get("uptimeSeconds") {
                    println!(
                        "       Uptime    {}",
                        muted.apply_to(format!("{}s", uptime))
                    );
                }
                if let Some(version) = obj.get("version") {
                    println!(
                        "       Version   {}",
                        muted.apply_to(version.as_str().unwrap_or("unknown"))
                    );
                }
            }
            println!();
            Ok(())
        },
    )
    .await
}
