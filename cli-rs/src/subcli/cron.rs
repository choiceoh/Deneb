use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct CronArgs {
    #[command(subcommand)]
    pub command: CronCommand,
}

#[derive(Subcommand, Debug)]
pub enum CronCommand {
    /// Show cron scheduler status.
    Status {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// List scheduled cron jobs.
    List {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Add a new cron job.
    Add {
        /// Cron expression (e.g. "*/5 * * * *").
        #[arg(long)]
        schedule: String,
        /// Agent message to run.
        #[arg(long)]
        message: String,
        /// Agent ID.
        #[arg(long)]
        agent: Option<String>,
        /// Job label.
        #[arg(long)]
        label: Option<String>,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Start a cron job.
    Start {
        /// Job ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Stop a cron job.
    Stop {
        /// Job ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Delete a cron job.
    Delete {
        /// Job ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &CronArgs) -> Result<(), CliError> {
    match &args.command {
        CronCommand::Status { gw } => {
            super::rpc_helpers::rpc_print("cron.status", serde_json::json!({}), gw).await
        }
        CronCommand::List { gw } => {
            super::rpc_helpers::rpc_print("cron.list", serde_json::json!({}), gw).await
        }
        CronCommand::Add {
            schedule,
            message,
            agent,
            label,
            gw,
        } => {
            let mut params = serde_json::json!({"schedule": schedule, "message": message});
            if let Some(a) = agent {
                params["agentId"] = serde_json::json!(a);
            }
            if let Some(l) = label {
                params["label"] = serde_json::json!(l);
            }
            super::rpc_helpers::rpc_action("cron.add", params, gw, "Cron job added.").await
        }
        CronCommand::Start { id, gw } => {
            super::rpc_helpers::rpc_action(
                "cron.start",
                serde_json::json!({"id": id}),
                gw,
                &format!("Cron job '{id}' started."),
            )
            .await
        }
        CronCommand::Stop { id, gw } => {
            super::rpc_helpers::rpc_action(
                "cron.stop",
                serde_json::json!({"id": id}),
                gw,
                &format!("Cron job '{id}' stopped."),
            )
            .await
        }
        CronCommand::Delete { id, gw } => {
            super::rpc_helpers::rpc_action(
                "cron.delete",
                serde_json::json!({"id": id}),
                gw,
                &format!("Cron job '{id}' deleted."),
            )
            .await
        }
    }
}
