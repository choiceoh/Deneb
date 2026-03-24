use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct AutonomousArgs {
    #[command(subcommand)]
    pub command: AutonomousCommand,
}

#[derive(Subcommand, Debug)]
pub enum AutonomousCommand {
    /// Show autonomous agent status.
    Status {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// List autonomous goals.
    Goals {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Add a goal.
    #[command(name = "goal-add")]
    GoalAdd {
        /// Goal description.
        description: String,
        /// Priority: high, medium, low.
        #[arg(long, default_value = "medium")]
        priority: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Remove a goal.
    #[command(name = "goal-remove")]
    GoalRemove {
        /// Goal ID.
        id: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Run a decision cycle.
    #[command(name = "cycle-run")]
    CycleRun {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Stop the autonomous cycle.
    #[command(name = "cycle-stop")]
    CycleStop {
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &AutonomousArgs) -> Result<(), CliError> {
    match &args.command {
        AutonomousCommand::Status { gw } => {
            super::rpc_helpers::rpc_print("autonomous.status", serde_json::json!({}), gw).await
        }
        AutonomousCommand::Goals { gw } => {
            super::rpc_helpers::rpc_print("autonomous.goals.list", serde_json::json!({}), gw).await
        }
        AutonomousCommand::GoalAdd {
            description,
            priority,
            gw,
        } => {
            super::rpc_helpers::rpc_action(
                "autonomous.goals.add",
                serde_json::json!({"description": description, "priority": priority}),
                gw,
                "Goal added.",
            )
            .await
        }
        AutonomousCommand::GoalRemove { id, gw } => {
            super::rpc_helpers::rpc_action(
                "autonomous.goals.remove",
                serde_json::json!({"id": id}),
                gw,
                &format!("Goal '{id}' removed."),
            )
            .await
        }
        AutonomousCommand::CycleRun { gw } => {
            super::rpc_helpers::rpc_action(
                "autonomous.cycle.run",
                serde_json::json!({}),
                gw,
                "Cycle started.",
            )
            .await
        }
        AutonomousCommand::CycleStop { gw } => {
            super::rpc_helpers::rpc_action(
                "autonomous.cycle.stop",
                serde_json::json!({}),
                gw,
                "Cycle stopped.",
            )
            .await
        }
    }
}
