use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct SystemArgs {
    #[command(subcommand)]
    pub command: SystemCommand,
}

#[derive(Subcommand, Debug)]
pub enum SystemCommand {
    /// Send a system event.
    Event {
        /// Event text.
        #[arg(long)]
        text: String,
        /// Delivery mode: now or next-heartbeat.
        #[arg(long, default_value = "now")]
        mode: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Show last heartbeat.
    Heartbeat {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Show presence status.
    Presence {
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &SystemArgs) -> Result<(), CliError> {
    match &args.command {
        SystemCommand::Event { text, mode, gw } => {
            super::rpc_helpers::rpc_action(
                "wake",
                serde_json::json!({"text": text, "mode": mode}),
                gw,
                "Event sent.",
            )
            .await
        }
        SystemCommand::Heartbeat { gw } => {
            super::rpc_helpers::rpc_print("last-heartbeat", serde_json::json!({}), gw).await
        }
        SystemCommand::Presence { gw } => {
            super::rpc_helpers::rpc_print("presence.list", serde_json::json!({}), gw).await
        }
    }
}
