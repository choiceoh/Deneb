use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct WebhooksArgs {
    #[command(subcommand)]
    pub command: WebhooksCommand,
}

#[derive(Subcommand, Debug)]
pub enum WebhooksCommand {
    /// List configured webhooks.
    List {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Show webhook status.
    Status {
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &WebhooksArgs) -> Result<(), CliError> {
    match &args.command {
        WebhooksCommand::List { gw } => {
            super::rpc_helpers::rpc_print("webhooks.list", serde_json::json!({}), gw).await
        }
        WebhooksCommand::Status { gw } => {
            super::rpc_helpers::rpc_print("webhooks.status", serde_json::json!({}), gw).await
        }
    }
}
