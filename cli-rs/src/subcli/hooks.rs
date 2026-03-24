use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct HooksArgs {
    #[command(subcommand)]
    pub command: HooksCommand,
}

#[derive(Subcommand, Debug)]
pub enum HooksCommand {
    /// List installed hooks.
    List {
        #[arg(long)]
        all: bool,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Enable a hook.
    Enable {
        /// Hook ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Disable a hook.
    Disable {
        /// Hook ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Show hook info.
    Info {
        /// Hook ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &HooksArgs) -> Result<(), CliError> {
    match &args.command {
        HooksCommand::List { all, gw } => {
            super::rpc_helpers::rpc_print("hooks.list", serde_json::json!({"all": all}), gw).await
        }
        HooksCommand::Enable { id, gw } => {
            super::rpc_helpers::rpc_action(
                "hooks.enable",
                serde_json::json!({"id": id}),
                gw,
                &format!("Hook '{id}' enabled."),
            )
            .await
        }
        HooksCommand::Disable { id, gw } => {
            super::rpc_helpers::rpc_action(
                "hooks.disable",
                serde_json::json!({"id": id}),
                gw,
                &format!("Hook '{id}' disabled."),
            )
            .await
        }
        HooksCommand::Info { id, gw } => {
            super::rpc_helpers::rpc_print("hooks.info", serde_json::json!({"id": id}), gw).await
        }
    }
}
