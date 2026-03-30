use clap::{Args, Subcommand};

use crate::errors::CliError;
use crate::gateway::{call_gateway, CallOptions};
use crate::subcli::rpc_helpers::GatewayFlags;
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct MessageArgs {
    #[command(subcommand)]
    pub command: MessageCommand,
}

#[derive(Subcommand, Debug)]
pub enum MessageCommand {
    /// Send a message to a channel target.
    Send(SendArgs),
}

/// Arguments for `message send`.
#[derive(Args, Debug)]
pub struct SendArgs {
    /// Destination target (phone number, username, chat ID, etc.).
    #[arg(short = 't', long)]
    pub target: String,

    /// Message text.
    #[arg(short = 'm', long)]
    pub message: Option<String>,

    /// Channel name (e.g. telegram, discord).
    #[arg(long)]
    pub channel: Option<String>,

    /// Account ID within the channel.
    #[arg(long)]
    pub account: Option<String>,

    /// Media attachment path or URL.
    #[arg(long)]
    pub media: Option<String>,

    /// Reply to a specific message ID.
    #[arg(long)]
    pub reply_to: Option<String>,

    /// Thread ID for threaded conversations.
    #[arg(long)]
    pub thread_id: Option<String>,

    /// Force media as document attachment (Telegram).
    #[arg(long)]
    pub force_document: bool,

    /// Send silently (no notification).
    #[arg(long)]
    pub silent: bool,

    /// Print the RPC params without sending.
    #[arg(long)]
    pub dry_run: bool,

    #[command(flatten)]
    pub gw: GatewayFlags,
}

pub async fn run(args: &MessageArgs) -> Result<(), CliError> {
    match &args.command {
        MessageCommand::Send(send) => cmd_send(send).await,
    }
}

async fn cmd_send(args: &SendArgs) -> Result<(), CliError> {
    let json_mode = is_json_mode(args.gw.json);
    let idempotency_key = uuid::Uuid::new_v4().to_string();

    // Build RPC params
    let mut params = serde_json::json!({
        "to": args.target,
        "idempotencyKey": idempotency_key,
    });

    if let Some(msg) = &args.message {
        params["message"] = serde_json::json!(msg);
    }
    if let Some(ch) = &args.channel {
        params["channel"] = serde_json::json!(ch);
    }
    if let Some(acct) = &args.account {
        params["accountId"] = serde_json::json!(acct);
    }
    if let Some(m) = &args.media {
        params["mediaUrl"] = serde_json::json!(m);
    }
    if let Some(rt) = &args.reply_to {
        params["replyTo"] = serde_json::json!(rt);
    }
    if let Some(tid) = &args.thread_id {
        params["threadId"] = serde_json::json!(tid);
    }
    if args.force_document {
        params["forceDocument"] = serde_json::json!(true);
    }
    if args.silent {
        params["silent"] = serde_json::json!(true);
    }

    // Dry run: print params and exit
    if args.dry_run {
        println!("{}", serde_json::to_string_pretty(&params)?);
        return Ok(());
    }

    if args.message.is_none() && args.media.is_none() {
        return Err(CliError::User(
            "either --message or --media is required".to_string(),
        ));
    }

    let result = crate::terminal::progress::with_spinner(
        "Sending message...",
        !json_mode,
        call_gateway(CallOptions {
            url: args.gw.url.clone(),
            token: args.gw.token.clone(),
            password: args.gw.password.clone(),
            method: "send".to_string(),
            params: Some(params),
            timeout_ms: args.gw.timeout,
            expect_final: true,
        }),
    )
    .await?;

    if json_mode {
        println!("{}", serde_json::to_string_pretty(&result)?);
    } else {
        let success = Palette::success();
        if let Some(text) = result
            .get("payload")
            .and_then(|p| p.get("text"))
            .and_then(|t| t.as_str())
        {
            println!("{}", text);
        } else {
            println!("{}", success.apply_to("Message sent."));
        }
    }

    Ok(())
}
