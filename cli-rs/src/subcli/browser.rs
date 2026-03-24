use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct BrowserArgs {
    #[command(subcommand)]
    pub command: BrowserCommand,
}

#[derive(Subcommand, Debug)]
pub enum BrowserCommand {
    /// Launch the headless browser.
    Launch {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Kill the headless browser.
    Kill {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Show browser status.
    Status {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Open a URL in the headless browser.
    Open {
        /// URL to open.
        url: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Evaluate JavaScript in the browser.
    Eval {
        /// JavaScript expression.
        script: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Take a screenshot.
    Screenshot {
        /// Output file path.
        #[arg(long, default_value = "screenshot.png")]
        output: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &BrowserArgs) -> Result<(), CliError> {
    match &args.command {
        BrowserCommand::Launch { gw } => {
            super::rpc_helpers::rpc_action(
                "browser.launch",
                serde_json::json!({}),
                gw,
                "Browser launched.",
            )
            .await
        }
        BrowserCommand::Kill { gw } => {
            super::rpc_helpers::rpc_action(
                "browser.kill",
                serde_json::json!({}),
                gw,
                "Browser killed.",
            )
            .await
        }
        BrowserCommand::Status { gw } => {
            super::rpc_helpers::rpc_print("browser.status", serde_json::json!({}), gw).await
        }
        BrowserCommand::Open { url, gw } => {
            super::rpc_helpers::rpc_action(
                "browser.open",
                serde_json::json!({"url": url}),
                gw,
                &format!("Opened {url}"),
            )
            .await
        }
        BrowserCommand::Eval { script, gw } => {
            super::rpc_helpers::rpc_print("browser.eval", serde_json::json!({"script": script}), gw)
                .await
        }
        BrowserCommand::Screenshot { output, gw } => {
            super::rpc_helpers::rpc_action(
                "browser.screenshot",
                serde_json::json!({"output": output}),
                gw,
                &format!("Screenshot saved to {output}"),
            )
            .await
        }
    }
}
