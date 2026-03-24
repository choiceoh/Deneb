use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct SecretsArgs {
    #[command(subcommand)]
    pub command: SecretsCommand,
}

#[derive(Subcommand, Debug)]
pub enum SecretsCommand {
    /// Reload secret references from config.
    Reload {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Audit for plaintext secrets in config.
    Audit {
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &SecretsArgs) -> Result<(), CliError> {
    match &args.command {
        SecretsCommand::Reload { gw } => {
            super::rpc_helpers::rpc_action(
                "secrets.reload",
                serde_json::json!({}),
                gw,
                "Secrets reloaded.",
            )
            .await
        }
        SecretsCommand::Audit { gw } => {
            super::rpc_helpers::rpc_print("secrets.audit", serde_json::json!({}), gw).await
        }
    }
}
