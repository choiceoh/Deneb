use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct PairingArgs {
    #[command(subcommand)]
    pub command: PairingCommand,
}

#[derive(Subcommand, Debug)]
pub enum PairingCommand {
    /// Start device pairing.
    Start {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Show pairing status.
    Status {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Cancel active pairing.
    Cancel {
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &PairingArgs) -> Result<(), CliError> {
    match &args.command {
        PairingCommand::Start { gw } => {
            super::rpc_helpers::rpc_print("pairing.start", serde_json::json!({}), gw).await
        }
        PairingCommand::Status { gw } => {
            super::rpc_helpers::rpc_print("pairing.status", serde_json::json!({}), gw).await
        }
        PairingCommand::Cancel { gw } => {
            super::rpc_helpers::rpc_action(
                "pairing.cancel",
                serde_json::json!({}),
                gw,
                "Pairing cancelled.",
            )
            .await
        }
    }
}
