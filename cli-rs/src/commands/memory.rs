use clap::{Args, Subcommand};

use crate::errors::CliError;
use crate::gateway::{call_gateway, CallOptions};
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct MemoryArgs {
    #[command(subcommand)]
    pub command: MemoryCommand,
}

#[derive(Subcommand, Debug)]
pub enum MemoryCommand {
    /// Show memory search index status.
    Status {
        /// Agent ID (default: "default").
        #[arg(long, default_value = "default")]
        agent: String,

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
        #[arg(long, default_value = "10000")]
        timeout: u64,
    },
}

pub async fn run(args: &MemoryArgs) -> Result<(), CliError> {
    match &args.command {
        MemoryCommand::Status {
            agent,
            json,
            url,
            token,
            password,
            timeout,
        } => {
            cmd_status(
                agent,
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

async fn cmd_status(
    agent: &str,
    json: bool,
    url: Option<&str>,
    token: Option<&str>,
    password: Option<&str>,
    timeout: u64,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);

    let result = crate::terminal::progress::with_spinner(
        "Fetching memory status...",
        !json_mode,
        call_gateway(CallOptions {
            url: url.map(|s| s.to_string()),
            token: token.map(|s| s.to_string()),
            password: password.map(|s| s.to_string()),
            method: "memory.status".to_string(),
            params: Some(serde_json::json!({ "agentId": agent })),
            timeout_ms: timeout,
            expect_final: false,
        }),
    )
    .await;

    match result {
        Ok(payload) => {
            if json_mode {
                println!("{}", serde_json::to_string_pretty(&payload)?);
            } else {
                print_memory_status(&payload, agent);
            }
            Ok(())
        }
        Err(e) => {
            if json_mode {
                let err = serde_json::json!({ "ok": false, "error": e.user_message() });
                println!("{}", serde_json::to_string_pretty(&err)?);
                std::process::exit(1);
            }
            Err(e)
        }
    }
}

fn print_memory_status(payload: &serde_json::Value, agent: &str) {
    use crate::terminal::Symbols;
    let bold = Palette::bold();
    let muted = Palette::muted();
    let success = Palette::success();

    println!();
    println!(
        "  {}  {}  {}",
        bold.apply_to("Memory"),
        muted.apply_to(Symbols::ARROW),
        muted.apply_to(agent)
    );
    println!();

    if let Some(obj) = payload.as_object() {
        if let Some(indexed) = obj.get("indexedCount").and_then(|v| v.as_u64()) {
            println!(
                "    Indexed     {}",
                success.apply_to(format!("{indexed} files"))
            );
        }
        if let Some(dirty) = obj.get("dirty").and_then(|v| v.as_bool()) {
            let label = if dirty { "yes (reindex needed)" } else { "no" };
            println!("    Dirty       {}", muted.apply_to(label));
        }
        if let Some(provider) = obj.get("provider").and_then(|v| v.as_str()) {
            println!("    Provider    {}", muted.apply_to(provider));
        }
        if let Some(model) = obj.get("model").and_then(|v| v.as_str()) {
            println!("    Model       {}", muted.apply_to(model));
        }
        if let Some(workspace) = obj.get("workspace").and_then(|v| v.as_str()) {
            println!("    Workspace   {}", muted.apply_to(workspace));
        }
    }
    println!();
}
