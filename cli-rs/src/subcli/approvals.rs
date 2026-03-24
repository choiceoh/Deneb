use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct ApprovalsArgs {
    #[command(subcommand)]
    pub command: ApprovalsCommand,
}

#[derive(Subcommand, Debug)]
pub enum ApprovalsCommand {
    /// List pending approvals.
    List {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Approve a pending request.
    Approve {
        /// Approval ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Deny a pending request.
    Deny {
        /// Approval ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &ApprovalsArgs) -> Result<(), CliError> {
    match &args.command {
        ApprovalsCommand::List { gw } => {
            super::rpc_helpers::rpc_print("approvals.list", serde_json::json!({}), gw).await
        }
        ApprovalsCommand::Approve { id, gw } => {
            super::rpc_helpers::rpc_action(
                "approvals.approve",
                serde_json::json!({"id": id}),
                gw,
                &format!("Approval '{id}' approved."),
            )
            .await
        }
        ApprovalsCommand::Deny { id, gw } => {
            super::rpc_helpers::rpc_action(
                "approvals.deny",
                serde_json::json!({"id": id}),
                gw,
                &format!("Approval '{id}' denied."),
            )
            .await
        }
    }
}
