use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct DirectoryArgs {
    #[command(subcommand)]
    pub command: DirectoryCommand,
}

#[derive(Subcommand, Debug)]
pub enum DirectoryCommand {
    /// Show bot/self identity on a channel.
    #[command(name = "self")]
    SelfInfo {
        /// Channel name.
        #[arg(long)]
        channel: String,
        /// Account ID.
        #[arg(long)]
        account: Option<String>,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Search peers/contacts.
    Peers {
        /// Search query.
        #[arg(long)]
        query: Option<String>,
        /// Channel name.
        #[arg(long)]
        channel: String,
        /// Account ID.
        #[arg(long)]
        account: Option<String>,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// List groups/channels.
    Groups {
        /// Channel name.
        #[arg(long)]
        channel: String,
        /// Account ID.
        #[arg(long)]
        account: Option<String>,
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &DirectoryArgs) -> Result<(), CliError> {
    match &args.command {
        DirectoryCommand::SelfInfo {
            channel,
            account,
            gw,
        } => {
            let mut params = serde_json::json!({"channel": channel});
            if let Some(a) = account {
                params["accountId"] = serde_json::json!(a);
            }
            super::rpc_helpers::rpc_print("directory.self", params, gw).await
        }
        DirectoryCommand::Peers {
            query,
            channel,
            account,
            gw,
        } => {
            let mut params = serde_json::json!({"channel": channel});
            if let Some(q) = query {
                params["query"] = serde_json::json!(q);
            }
            if let Some(a) = account {
                params["accountId"] = serde_json::json!(a);
            }
            super::rpc_helpers::rpc_print("directory.peers", params, gw).await
        }
        DirectoryCommand::Groups {
            channel,
            account,
            gw,
        } => {
            let mut params = serde_json::json!({"channel": channel});
            if let Some(a) = account {
                params["accountId"] = serde_json::json!(a);
            }
            super::rpc_helpers::rpc_print("directory.groups", params, gw).await
        }
    }
}
