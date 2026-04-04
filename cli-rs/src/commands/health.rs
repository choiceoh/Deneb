use clap::Args;

use crate::errors::CliError;
use crate::subcli::rpc_helpers::{rpc_print_fmt, GatewayFlags};
use crate::terminal::Palette;

#[derive(Args, Debug)]
pub struct HealthArgs {
    #[command(flatten)]
    pub gw: GatewayFlags,
}

pub async fn run(args: &HealthArgs) -> Result<(), CliError> {
    rpc_print_fmt(
        "health",
        serde_json::json!({}),
        &args.gw,
        "Checking gateway health...",
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

            if let Some(obj) = payload.as_object() {
                if let Some(uptime) = obj.get("uptimeSeconds") {
                    println!(
                        "       Uptime    {}",
                        muted.apply_to(format!("{uptime}s"))
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
