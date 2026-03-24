use clap::Args;

use crate::errors::CliError;
use crate::gateway::{call_gateway, CallOptions};
use crate::terminal::{is_json_mode, Palette};

const THINKING_LEVELS: [&str; 6] = ["off", "minimal", "low", "medium", "high", "xhigh"];

#[derive(Args, Debug)]
pub struct AgentArgs {
    /// Message to send to the agent.
    #[arg(short = 'm', long)]
    pub message: String,

    /// E.164 phone number to derive session key.
    #[arg(short = 't', long)]
    pub to: Option<String>,

    /// Explicit session ID.
    #[arg(long)]
    pub session_id: Option<String>,

    /// Agent ID (overrides default routing).
    #[arg(long)]
    pub agent: Option<String>,

    /// Thinking level.
    #[arg(long, value_parser = THINKING_LEVELS)]
    pub thinking: Option<String>,

    /// Send the agent reply back to the channel.
    #[arg(long)]
    pub deliver: bool,

    /// Delivery channel.
    #[arg(long)]
    pub channel: Option<String>,

    /// Delivery target override.
    #[arg(long)]
    pub reply_to: Option<String>,

    /// Delivery channel override.
    #[arg(long)]
    pub reply_channel: Option<String>,

    /// Delivery account override.
    #[arg(long)]
    pub reply_account: Option<String>,

    /// Output JSON.
    #[arg(long)]
    pub json: bool,

    /// Gateway WebSocket URL override.
    #[arg(long)]
    pub url: Option<String>,

    /// Gateway auth token.
    #[arg(long)]
    pub token: Option<String>,

    /// Gateway password.
    #[arg(long)]
    pub password: Option<String>,

    /// Timeout in seconds (default: 600).
    #[arg(long, default_value = "600")]
    pub timeout: u64,
}

pub async fn run(args: &AgentArgs) -> Result<(), CliError> {
    let json_mode = is_json_mode(args.json);
    let idempotency_key = uuid::Uuid::new_v4().to_string();
    let timeout_ms = args.timeout * 1000;

    // Build RPC params
    let mut params = serde_json::json!({
        "message": args.message,
        "idempotencyKey": idempotency_key,
        "timeout": args.timeout,
    });

    if let Some(ref to) = args.to {
        params["to"] = serde_json::json!(to);
    }
    if let Some(ref sid) = args.session_id {
        params["sessionId"] = serde_json::json!(sid);
    }
    if let Some(ref agent_id) = args.agent {
        params["agentId"] = serde_json::json!(agent_id);
    }
    if let Some(ref thinking) = args.thinking {
        params["thinking"] = serde_json::json!(thinking);
    }
    if args.deliver {
        params["deliver"] = serde_json::json!(true);
    }
    if let Some(ref ch) = args.channel {
        params["channel"] = serde_json::json!(ch);
    }
    if let Some(ref rt) = args.reply_to {
        params["replyTo"] = serde_json::json!(rt);
    }
    if let Some(ref rc) = args.reply_channel {
        params["replyChannel"] = serde_json::json!(rc);
    }
    if let Some(ref ra) = args.reply_account {
        params["replyAccountId"] = serde_json::json!(ra);
    }

    let result = crate::terminal::progress::with_spinner(
        "Running agent...",
        !json_mode,
        call_gateway(CallOptions {
            url: args.url.clone(),
            token: args.token.clone(),
            password: args.password.clone(),
            method: "agent".to_string(),
            params: Some(params),
            timeout_ms,
            expect_final: true,
        }),
    )
    .await?;

    if json_mode {
        println!("{}", serde_json::to_string_pretty(&result)?);
        return Ok(());
    }

    // Text mode: print payloads or summary
    let payloads = result
        .get("result")
        .and_then(|r| r.get("payloads"))
        .and_then(|p| p.as_array());

    if let Some(payloads) = payloads {
        for payload in payloads {
            if let Some(text) = payload.get("text").and_then(|t| t.as_str()) {
                println!("{text}");
            }
            if let Some(media_url) = payload.get("mediaUrl").and_then(|u| u.as_str()) {
                let muted = Palette::muted();
                println!("{}", muted.apply_to(format!("[media: {media_url}]")));
            }
        }
    } else if let Some(summary) = result.get("summary").and_then(|s| s.as_str()) {
        println!("{summary}");
    }

    // Show status if not successful
    if let Some(status) = result.get("status").and_then(|s| s.as_str()) {
        if status != "ok" && status != "completed" {
            let warn = Palette::warn();
            eprintln!("{}", warn.apply_to(format!("Status: {status}")));
        }
    }

    Ok(())
}
