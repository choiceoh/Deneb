use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct DevicesArgs {
    #[command(subcommand)]
    pub command: DevicesCommand,
}

#[derive(Subcommand, Debug)]
pub enum DevicesCommand {
    /// List paired devices.
    List {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Show device info.
    Info {
        /// Device ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Remove a paired device.
    Remove {
        /// Device ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &DevicesArgs) -> Result<(), CliError> {
    match &args.command {
        DevicesCommand::List { gw } => {
            super::rpc_helpers::rpc_print("devices.list", serde_json::json!({}), gw).await
        }
        DevicesCommand::Info { id, gw } => {
            super::rpc_helpers::rpc_print("devices.info", serde_json::json!({"id": id}), gw).await
        }
        DevicesCommand::Remove { id, gw } => {
            super::rpc_helpers::rpc_action(
                "devices.remove",
                serde_json::json!({"id": id}),
                gw,
                &format!("Device '{id}' removed."),
            )
            .await
        }
    }
}
