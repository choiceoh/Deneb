use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct SandboxArgs {
    #[command(subcommand)]
    pub command: SandboxCommand,
}

#[derive(Subcommand, Debug)]
pub enum SandboxCommand {
    /// List sandbox containers.
    List {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Recreate a sandbox container.
    Recreate {
        /// Session or agent scope.
        #[arg(long)]
        session: Option<String>,
        #[arg(long)]
        agent: Option<String>,
        #[arg(long)]
        force: bool,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Explain sandbox configuration.
    Explain {
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &SandboxArgs) -> Result<(), CliError> {
    match &args.command {
        SandboxCommand::List { gw } => {
            super::rpc_helpers::rpc_print("sandbox.list", serde_json::json!({}), gw).await
        }
        SandboxCommand::Recreate {
            session,
            agent,
            force,
            gw,
        } => {
            let mut params = serde_json::json!({"force": force});
            if let Some(s) = session {
                params["session"] = serde_json::json!(s);
            }
            if let Some(a) = agent {
                params["agentId"] = serde_json::json!(a);
            }
            super::rpc_helpers::rpc_action("sandbox.recreate", params, gw, "Sandbox recreated.")
                .await
        }
        SandboxCommand::Explain { gw } => {
            super::rpc_helpers::rpc_print("sandbox.explain", serde_json::json!({}), gw).await
        }
    }
}
