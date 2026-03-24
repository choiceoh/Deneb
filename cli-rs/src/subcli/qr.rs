use clap::{Args, Subcommand};

use super::rpc_helpers::GatewayFlags;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct QrArgs {
    #[command(subcommand)]
    pub command: QrCommand,
}

#[derive(Subcommand, Debug)]
pub enum QrCommand {
    /// Show QR code for device pairing.
    Show {
        #[command(flatten)]
        gw: GatewayFlags,
    },
    /// Import config via QR code data.
    Import {
        /// QR code data (base64 or JSON).
        data: String,
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &QrArgs) -> Result<(), CliError> {
    match &args.command {
        QrCommand::Show { gw } => {
            super::rpc_helpers::rpc_print("qr.show", serde_json::json!({}), gw).await
        }
        QrCommand::Import { data, gw } => {
            super::rpc_helpers::rpc_action(
                "qr.import",
                serde_json::json!({"data": data}),
                gw,
                "QR import complete.",
            )
            .await
        }
    }
}
