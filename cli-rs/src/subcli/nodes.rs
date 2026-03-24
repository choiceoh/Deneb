use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct NodesArgs {
    #[command(subcommand)]
    pub command: NodesCommand,
}

#[derive(Subcommand, Debug)]
pub enum NodesCommand {
    /// List registered compute nodes.
    List {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Show node status.
    Status {
        /// Node ID.
        id: Option<String>,
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &NodesArgs) -> Result<(), CliError> {
    match &args.command {
        NodesCommand::List { gw } => {
            super::rpc_helpers::rpc_print("nodes.list", serde_json::json!({}), gw).await
        }
        NodesCommand::Status { id, gw } => {
            let params = match id {
                Some(id) => serde_json::json!({"id": id}),
                None => serde_json::json!({}),
            };
            super::rpc_helpers::rpc_print("nodes.status", params, gw).await
        }
    }
}
