use clap::Args;

use crate::commands::gateway_query::run_gateway_query;
use crate::config;
use crate::errors::CliError;
use crate::gateway::call_gateway_with_config;
use crate::subcli::rpc_helpers::GatewayFlags;
use crate::terminal::Palette;

#[derive(Args, Debug)]
pub struct StatusArgs {
    #[command(flatten)]
    pub gw: GatewayFlags,

    /// Show all details.
    #[arg(long)]
    pub all: bool,
}

pub async fn run(args: &StatusArgs) -> Result<(), CliError> {
    let config_path = config::resolve_config_path();
    let config = config::load_config_best_effort(&config_path);

    run_gateway_query(
        &args.gw,
        "health",
        "Fetching status...",
        |opts| call_gateway_with_config(opts, &config),
        |payload, json_mode| {
            if json_mode {
                println!("{}", serde_json::to_string_pretty(&payload)?);
                return Ok(());
            }

            print_status_summary(&payload, &config);
            Ok(())
        },
    )
    .await
    .map_err(|e| {
        use crate::terminal::Symbols;
        let error_style = Palette::error();
        eprintln!(
            "  {}  {}",
            error_style.apply_to(Symbols::ERROR),
            e.user_message()
        );

        // Show config info even when gateway is down
        let muted = Palette::muted();
        let port = config::resolve_gateway_port(config.gateway_port());
        eprintln!();
        eprintln!("    Port      {}", muted.apply_to(port));
        eprintln!("    Config    {}", muted.apply_to(config_path.display()));
        eprintln!(
            "    State     {}",
            muted.apply_to(config::resolve_state_dir().display())
        );

        std::process::exit(1);
    })
}

fn print_status_summary(payload: &serde_json::Value, config: &config::DenebConfig) {
    use crate::terminal::Symbols;
    let bold = Palette::bold();
    let success = Palette::success();
    let muted = Palette::muted();

    println!();
    println!("  {}", bold.apply_to("Deneb"));
    println!();
    println!(
        "    Gateway   {} {}",
        success.apply_to(Symbols::SUCCESS),
        success.apply_to("connected")
    );

    let port = config::resolve_gateway_port(config.gateway_port());
    println!("    Port      {}", muted.apply_to(port));

    let mode = config
        .gateway
        .as_ref()
        .and_then(|g| g.mode.as_deref())
        .unwrap_or("local");
    println!("    Mode      {}", muted.apply_to(mode));

    if let Some(obj) = payload.as_object() {
        if let Some(version) = obj.get("version").and_then(|v| v.as_str()) {
            println!("    Version   {}", muted.apply_to(version));
        }
        if let Some(uptime) = obj.get("uptimeSeconds").and_then(serde_json::Value::as_f64) {
            let hours = uptime / 3600.0;
            if hours >= 1.0 {
                println!("    Uptime    {}", muted.apply_to(format!("{hours:.1}h")));
            } else {
                let mins = uptime / 60.0;
                println!("    Uptime    {}", muted.apply_to(format!("{mins:.0}m")));
            }
        }
        if let Some(channels) = obj.get("channels").and_then(|v| v.as_object()) {
            println!(
                "    Channels  {}",
                muted.apply_to(format!("{} configured", channels.len()))
            );
        }
    }

    println!(
        "    Config    {}",
        muted.apply_to(config::resolve_config_path().display())
    );
    println!();
}
