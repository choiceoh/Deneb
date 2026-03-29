mod list;
mod mutate;
mod render;
mod validate;

use clap::{Args, Subcommand};

use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct AgentsArgs {
    #[command(subcommand)]
    pub command: AgentsCommand,
}

#[derive(Subcommand, Debug)]
pub enum AgentsCommand {
    /// List configured agents.
    List {
        /// Output JSON.
        #[arg(long)]
        json: bool,
    },

    /// Add a new agent.
    Add {
        /// Agent name (used to derive the agent ID).
        name: Option<String>,

        /// Workspace directory.
        #[arg(long)]
        workspace: Option<String>,

        /// Model ID.
        #[arg(long)]
        model: Option<String>,

        /// Agent state directory.
        #[arg(long)]
        agent_dir: Option<String>,

        /// Route binding in channel[:accountId] format (repeatable).
        #[arg(long = "bind")]
        bindings: Vec<String>,

        /// Skip interactive prompts.
        #[arg(long)]
        non_interactive: bool,

        /// Output JSON.
        #[arg(long)]
        json: bool,
    },

    /// Delete an agent.
    Delete {
        /// Agent ID to delete.
        id: String,

        /// Skip confirmation prompt.
        #[arg(long)]
        force: bool,

        /// Output JSON.
        #[arg(long)]
        json: bool,
    },

    /// Add route bindings to an agent.
    Bind {
        /// Agent ID (defaults to "default").
        #[arg(long, default_value = "default")]
        agent: String,

        /// Binding in channel[:accountId] format (repeatable).
        #[arg(long = "bind")]
        bindings: Vec<String>,

        /// Output JSON.
        #[arg(long)]
        json: bool,
    },

    /// Remove route bindings from an agent.
    Unbind {
        /// Agent ID.
        #[arg(long, default_value = "default")]
        agent: String,

        /// Binding to remove in channel[:accountId] format (repeatable).
        #[arg(long = "bind")]
        bindings: Vec<String>,

        /// Remove all bindings for this agent.
        #[arg(long)]
        all: bool,

        /// Output JSON.
        #[arg(long)]
        json: bool,
    },
}

pub async fn run(args: &AgentsArgs) -> Result<(), CliError> {
    match &args.command {
        AgentsCommand::List { json } => list::cmd_list(*json).await,
        AgentsCommand::Add {
            name,
            workspace,
            model,
            agent_dir,
            bindings,
            non_interactive,
            json,
        } => {
            mutate::cmd_add(
                name.as_deref(),
                workspace.as_deref(),
                model.as_deref(),
                agent_dir.as_deref(),
                bindings,
                *non_interactive,
                *json,
            )
            .await
        }
        AgentsCommand::Delete { id, force, json } => mutate::cmd_delete(id, *force, *json).await,
        AgentsCommand::Bind {
            agent,
            bindings,
            json,
        } => mutate::cmd_bind(agent, bindings, *json).await,
        AgentsCommand::Unbind {
            agent,
            bindings,
            all,
            json,
        } => mutate::cmd_unbind(agent, bindings, *all, *json).await,
    }
}
