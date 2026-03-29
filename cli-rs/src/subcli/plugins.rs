use clap::{Args, Subcommand};

use super::rpc_helpers::{rpc_action, rpc_print, rpc_print_fmt, GatewayFlags};
use crate::errors::CliError;
use crate::terminal::{styled_table, Palette};

#[derive(Args, Debug)]
pub struct PluginsArgs {
    #[command(subcommand)]
    pub command: PluginsCommand,
}

#[derive(Subcommand, Debug)]
pub enum PluginsCommand {
    /// List installed plugins.
    List {
        #[arg(long)]
        all: bool,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Show plugin info.
    Info {
        /// Plugin ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Enable a plugin.
    Enable {
        /// Plugin ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Disable a plugin.
    Disable {
        /// Plugin ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Install a plugin.
    Install {
        /// Plugin package name or path.
        source: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Uninstall a plugin.
    Uninstall {
        /// Plugin ID.
        id: String,
        #[arg(long)]
        force: bool,
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &PluginsArgs) -> Result<(), CliError> {
    match &args.command {
        PluginsCommand::List { all, gw } => {
            rpc_print_fmt(
                "plugins.list",
                serde_json::json!({"all": all}),
                gw,
                "Fetching plugins...",
                |result, json_mode| {
                    if json_mode {
                        println!("{}", serde_json::to_string_pretty(&result)?);
                    } else {
                        print_plugins_table(&result);
                    }
                    Ok(())
                },
            )
            .await
        }
        PluginsCommand::Info { id, gw } => {
            rpc_print("plugins.info", serde_json::json!({"id": id}), gw).await
        }
        PluginsCommand::Enable { id, gw } => {
            rpc_action(
                "plugins.enable",
                serde_json::json!({"id": id}),
                gw,
                &format!("Plugin '{id}' enabled."),
            )
            .await
        }
        PluginsCommand::Disable { id, gw } => {
            rpc_action(
                "plugins.disable",
                serde_json::json!({"id": id}),
                gw,
                &format!("Plugin '{id}' disabled."),
            )
            .await
        }
        PluginsCommand::Install { source, gw } => {
            rpc_action(
                "plugins.install",
                serde_json::json!({"source": source}),
                gw,
                &format!("Plugin '{source}' installed."),
            )
            .await
        }
        PluginsCommand::Uninstall { id, force, gw } => {
            rpc_action(
                "plugins.uninstall",
                serde_json::json!({"id": id, "force": force}),
                gw,
                &format!("Plugin '{id}' uninstalled."),
            )
            .await
        }
    }
}

fn print_plugins_table(result: &serde_json::Value) {
    use crate::terminal::Symbols;
    let plugins = result
        .as_array()
        .or_else(|| result.get("plugins").and_then(|p| p.as_array()));
    let Some(plugins) = plugins else {
        println!("    {}", Palette::muted().apply_to("No plugins installed."));
        return;
    };
    let bold = Palette::bold();
    let muted = Palette::muted();
    println!();
    println!(
        "  {}  {}  {}",
        bold.apply_to("Plugins"),
        muted.apply_to(Symbols::ARROW),
        muted.apply_to(format!("{} installed", plugins.len()))
    );
    println!();
    let mut table = styled_table();
    table.set_header(vec!["ID", "Version", "Enabled"]);
    for p in plugins {
        let id = p.get("id").and_then(|v| v.as_str()).unwrap_or(Symbols::DASH);
        let version = p.get("version").and_then(|v| v.as_str()).unwrap_or(Symbols::DASH);
        let enabled = p
            .get("enabled")
            .and_then(|v| v.as_bool())
            .map(|b| if b { Symbols::DOT_FILLED } else { Symbols::DASH })
            .unwrap_or(Symbols::DASH);
        table.add_row(vec![id, version, enabled]);
    }
    println!("{table}");
    println!();
}
