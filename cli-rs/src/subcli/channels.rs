use clap::{Args, Subcommand};

use super::rpc_helpers::{rpc_print_fmt, GatewayFlags};
use crate::errors::CliError;
use crate::terminal::{styled_table, Palette};

#[derive(Args, Debug)]
pub struct ChannelsArgs {
    #[command(subcommand)]
    pub command: ChannelsCommand,
}

#[derive(Subcommand, Debug)]
pub enum ChannelsCommand {
    /// List configured channels and accounts.
    List {
        /// Skip usage/quota snapshots.
        #[arg(long)]
        no_usage: bool,
        #[command(flatten)]
        gw: GatewayFlags,
    },

    /// Show channel status with optional connectivity probe.
    Status {
        /// Probe channel credentials for connectivity.
        #[arg(long)]
        probe: bool,
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &ChannelsArgs) -> Result<(), CliError> {
    match &args.command {
        ChannelsCommand::List { no_usage, gw } => {
            let mut params = serde_json::json!({});
            if *no_usage {
                params["noUsage"] = serde_json::json!(true);
            }
            rpc_print_fmt("channels.list", params, gw, "Fetching channels...", |result, json_mode| {
                if json_mode {
                    println!("{}", serde_json::to_string_pretty(&result)?);
                    return Ok(());
                }
                print_channels_list(&result);
                Ok(())
            })
            .await
        }
        ChannelsCommand::Status { probe, gw } => {
            let probe = *probe;
            let mut params = serde_json::json!({});
            if probe {
                params["probe"] = serde_json::json!(true);
            }
            params["timeoutMs"] = serde_json::json!(gw.timeout);
            let spinner_msg = if probe { "Probing channels..." } else { "Fetching channel status..." };
            rpc_print_fmt("channels.status", params, gw, spinner_msg, move |result, json_mode| {
                if json_mode {
                    println!("{}", serde_json::to_string_pretty(&result)?);
                    return Ok(());
                }
                print_channels_status(&result, probe);
                Ok(())
            })
            .await
        }
    }
}

fn print_channels_list(result: &serde_json::Value) {
    use crate::terminal::Symbols;
    let bold = Palette::bold();
    let muted = Palette::muted();

    let channel_accounts = result.get("channelAccounts").and_then(|ca| ca.as_object());
    let Some(channel_accounts) = channel_accounts else {
        println!("    {}", muted.apply_to("No channels configured."));
        return;
    };

    println!();
    println!("  {}", bold.apply_to("Channels"));
    println!();

    let mut table = styled_table();
    table.set_header(vec!["Channel", "Account", "Enabled"]);

    for (channel, accounts) in channel_accounts {
        let accounts_arr = accounts.as_array().cloned().unwrap_or_default();
        if accounts_arr.is_empty() {
            table.add_row(vec![
                channel.clone(),
                Symbols::DASH.to_string(),
                Symbols::DASH.to_string(),
            ]);
            continue;
        }
        for acct in &accounts_arr {
            let account_id = acct
                .get("accountId")
                .and_then(|v| v.as_str())
                .unwrap_or(Symbols::DASH);
            let enabled = acct
                .get("enabled")
                .and_then(|v| v.as_bool())
                .map(|b| if b { Symbols::DOT_FILLED } else { Symbols::DASH })
                .unwrap_or(Symbols::DASH);
            table.add_row(vec![channel.clone(), account_id.to_string(), enabled.to_string()]);
        }
    }

    println!("{table}");
    println!();
}

fn print_channels_status(result: &serde_json::Value, probe: bool) {
    use crate::terminal::Symbols;
    let bold = Palette::bold();
    let muted = Palette::muted();

    let channel_accounts = result.get("channelAccounts").and_then(|ca| ca.as_object());
    let Some(channel_accounts) = channel_accounts else {
        println!("    {}", muted.apply_to("No channels configured."));
        return;
    };

    println!();
    println!("  {}", bold.apply_to("Channel Status"));
    println!();

    let mut table = styled_table();
    let mut headers = vec!["Channel", "Account", "Enabled", "Configured", "Linked", "Connected"];
    if probe {
        headers.push("Probe");
    }
    headers.push("Last Activity");
    table.set_header(headers);

    for (channel, accounts) in channel_accounts {
        let accounts_arr = accounts.as_array().cloned().unwrap_or_default();
        for acct in &accounts_arr {
            let account_id = acct
                .get("accountId")
                .and_then(|v| v.as_str())
                .unwrap_or(Symbols::DASH);
            let enabled = bool_indicator(acct.get("enabled"));
            let configured = bool_indicator(acct.get("configured"));
            let linked = bool_indicator(acct.get("linked"));
            let connected = bool_indicator(acct.get("connected"));
            let last_activity = acct
                .get("lastInboundAt")
                .or_else(|| acct.get("lastOutboundAt"))
                .and_then(|v| v.as_f64())
                .map(format_age)
                .unwrap_or_else(|| Symbols::DASH.to_string());

            let mut row = vec![
                channel.clone(),
                account_id.to_string(),
                enabled,
                configured,
                linked,
                connected,
            ];

            if probe {
                let probe_ok = acct
                    .get("probe")
                    .and_then(|p| p.get("ok"))
                    .and_then(|v| v.as_bool());
                row.push(match probe_ok {
                    Some(true) => Symbols::SUCCESS.to_string(),
                    Some(false) => Symbols::ERROR.to_string(),
                    None => Symbols::DASH.to_string(),
                });
            }

            row.push(last_activity);
            table.add_row(row);
        }
    }

    println!("{table}");
    println!();
}

fn bool_indicator(val: Option<&serde_json::Value>) -> String {
    use crate::terminal::Symbols;
    match val.and_then(|v| v.as_bool()) {
        Some(true) => Symbols::DOT_FILLED.to_string(),
        _ => Symbols::DASH.to_string(),
    }
}

/// Format a Unix millisecond timestamp as a relative age string.
fn format_age(ts_ms: f64) -> String {
    let now_ms = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as f64)
        .unwrap_or(0.0);
    let diff_s = ((now_ms - ts_ms) / 1000.0).max(0.0);

    if diff_s < 60.0 {
        format!("{:.0}s ago", diff_s)
    } else if diff_s < 3600.0 {
        format!("{:.0}m ago", diff_s / 60.0)
    } else if diff_s < 86400.0 {
        format!("{:.0}h ago", diff_s / 3600.0)
    } else {
        format!("{:.0}d ago", diff_s / 86400.0)
    }
}
