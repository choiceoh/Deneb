use clap::{Args, Subcommand};

use crate::errors::CliError;
use crate::gateway::{call_gateway, CallOptions};
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct MessageArgs {
    #[command(subcommand)]
    pub command: MessageCommand,
}

#[derive(Subcommand, Debug)]
pub enum MessageCommand {
    /// Send a message to a channel target.
    Send {
        /// Destination target (phone number, username, chat ID, etc.).
        #[arg(short = 't', long)]
        target: String,

        /// Message text.
        #[arg(short = 'm', long)]
        message: Option<String>,

        /// Channel name (e.g. telegram, discord, slack).
        #[arg(long)]
        channel: Option<String>,

        /// Account ID within the channel.
        #[arg(long)]
        account: Option<String>,

        /// Media attachment path or URL.
        #[arg(long)]
        media: Option<String>,

        /// Reply to a specific message ID.
        #[arg(long)]
        reply_to: Option<String>,

        /// Thread ID for threaded conversations.
        #[arg(long)]
        thread_id: Option<String>,

        /// Send media as GIF (WhatsApp).
        #[arg(long)]
        gif_playback: bool,

        /// Force media as document attachment (Telegram).
        #[arg(long)]
        force_document: bool,

        /// Send silently (no notification).
        #[arg(long)]
        silent: bool,

        /// Print the RPC params without sending.
        #[arg(long)]
        dry_run: bool,

        /// Output JSON.
        #[arg(long)]
        json: bool,

        /// Gateway WebSocket URL override.
        #[arg(long)]
        url: Option<String>,

        /// Gateway auth token.
        #[arg(long)]
        token: Option<String>,

        /// Gateway password.
        #[arg(long)]
        password: Option<String>,

        /// Timeout in milliseconds.
        #[arg(long, default_value = "30000")]
        timeout: u64,
    },
}

pub async fn run(args: &MessageArgs) -> Result<(), CliError> {
    match &args.command {
        MessageCommand::Send {
            target,
            message,
            channel,
            account,
            media,
            reply_to,
            thread_id,
            gif_playback,
            force_document,
            silent,
            dry_run,
            json,
            url,
            token,
            password,
            timeout,
        } => {
            cmd_send(
                target,
                message.as_deref(),
                channel.as_deref(),
                account.as_deref(),
                media.as_deref(),
                reply_to.as_deref(),
                thread_id.as_deref(),
                *gif_playback,
                *force_document,
                *silent,
                *dry_run,
                *json,
                url.as_deref(),
                token.as_deref(),
                password.as_deref(),
                *timeout,
            )
            .await
        }
    }
}

#[allow(clippy::too_many_arguments)]
async fn cmd_send(
    target: &str,
    message: Option<&str>,
    channel: Option<&str>,
    account: Option<&str>,
    media: Option<&str>,
    reply_to: Option<&str>,
    thread_id: Option<&str>,
    gif_playback: bool,
    force_document: bool,
    silent: bool,
    dry_run: bool,
    json: bool,
    url: Option<&str>,
    token: Option<&str>,
    password: Option<&str>,
    timeout: u64,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);
    let idempotency_key = uuid::Uuid::new_v4().to_string();

    // Build RPC params
    let mut params = serde_json::json!({
        "to": target,
        "idempotencyKey": idempotency_key,
    });

    if let Some(msg) = message {
        params["message"] = serde_json::json!(msg);
    }
    if let Some(ch) = channel {
        params["channel"] = serde_json::json!(ch);
    }
    if let Some(acct) = account {
        params["accountId"] = serde_json::json!(acct);
    }
    if let Some(m) = media {
        params["mediaUrl"] = serde_json::json!(m);
    }
    if let Some(rt) = reply_to {
        params["replyTo"] = serde_json::json!(rt);
    }
    if let Some(tid) = thread_id {
        params["threadId"] = serde_json::json!(tid);
    }
    if gif_playback {
        params["gifPlayback"] = serde_json::json!(true);
    }
    if force_document {
        params["forceDocument"] = serde_json::json!(true);
    }
    if silent {
        params["silent"] = serde_json::json!(true);
    }

    // Dry run: print params and exit
    if dry_run {
        println!("{}", serde_json::to_string_pretty(&params)?);
        return Ok(());
    }

    if message.is_none() && media.is_none() {
        return Err(CliError::User(
            "either --message or --media is required".to_string(),
        ));
    }

    let result = crate::terminal::progress::with_spinner(
        "Sending message...",
        !json_mode,
        call_gateway(CallOptions {
            url: url.map(|s| s.to_string()),
            token: token.map(|s| s.to_string()),
            password: password.map(|s| s.to_string()),
            method: "send".to_string(),
            params: Some(params),
            timeout_ms: timeout,
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
