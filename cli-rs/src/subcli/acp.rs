use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct AcpArgs {
    #[command(subcommand)]
    pub command: AcpCommand,
}

#[derive(Subcommand, Debug)]
pub enum AcpCommand {
    /// Show ACP server status.
    Status {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Start ACP server.
    Start {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Stop ACP server.
    Stop {
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &AcpArgs) -> Result<(), CliError> {
    match &args.command {
        AcpCommand::Status { gw } => {
            super::rpc_helpers::rpc_print("acp.status", serde_json::json!({}), gw).await
        }
        AcpCommand::Start { gw } => {
            super::rpc_helpers::rpc_action(
                "acp.start",
                serde_json::json!({}),
                gw,
                "ACP server started.",
            )
            .await
        }
        AcpCommand::Stop { gw } => {
            super::rpc_helpers::rpc_action(
                "acp.stop",
                serde_json::json!({}),
                gw,
                "ACP server stopped.",
            )
            .await
        }
    }
}
