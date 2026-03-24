use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct SkillsArgs {
    #[command(subcommand)]
    pub command: SkillsCommand,
}

#[derive(Subcommand, Debug)]
pub enum SkillsCommand {
    /// List available skills.
    List {
        #[arg(long)]
        eligible: bool,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Show skill info.
    Info {
        /// Skill ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Check skill requirements.
    Check {
        /// Skill ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &SkillsArgs) -> Result<(), CliError> {
    match &args.command {
        SkillsCommand::List { eligible, gw } => {
            super::rpc_helpers::rpc_print(
                "skills.list",
                serde_json::json!({"eligible": eligible}),
                gw,
            )
            .await
        }
        SkillsCommand::Info { id, gw } => {
            super::rpc_helpers::rpc_print("skills.info", serde_json::json!({"id": id}), gw).await
        }
        SkillsCommand::Check { id, gw } => {
            super::rpc_helpers::rpc_print("skills.check", serde_json::json!({"id": id}), gw).await
        }
    }
}
