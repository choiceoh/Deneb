use clap::{Args, Subcommand};

use crate::errors::CliError;
use crate::gateway::{call_gateway, CallOptions};
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct ModelsArgs {
    #[command(subcommand)]
    pub command: ModelsCommand,
}

#[derive(Subcommand, Debug)]
pub enum ModelsCommand {
    /// List available models.
    List {
        /// Show full catalog (not just configured).
        #[arg(long)]
        all: bool,

        /// Filter by provider name.
        #[arg(long)]
        provider: Option<String>,

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

    /// Show configured model status.
    Status {
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

pub async fn run(args: &ModelsArgs) -> Result<(), CliError> {
    match &args.command {
        ModelsCommand::List {
            all,
            provider,
            json,
            url,
            token,
            password,
            timeout,
        } => {
            cmd_list(
                *all,
                provider.as_deref(),
                *json,
                url.as_deref(),
                token.as_deref(),
                password.as_deref(),
                *timeout,
            )
            .await
        }
        ModelsCommand::Status {
            json,
            url,
            token,
            password,
            timeout,
        } => {
            cmd_status(
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

async fn cmd_list(
    all: bool,
    provider: Option<&str>,
    json: bool,
    url: Option<&str>,
    token: Option<&str>,
    password: Option<&str>,
    timeout: u64,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);

    let params = serde_json::json!({
        "all": all,
        "provider": provider,
    });

    let result = crate::terminal::progress::with_spinner(
        "Fetching models...",
        !json_mode,
        call_gateway(CallOptions {
            url: url.map(|s| s.to_string()),
            token: token.map(|s| s.to_string()),
            password: password.map(|s| s.to_string()),
            method: "models.list".to_string(),
            params: Some(params),
            timeout_ms: timeout,
            expect_final: false,
        }),
    )
    .await?;

    if json_mode {
        println!("{}", serde_json::to_string_pretty(&result)?);
        return Ok(());
    }

    let models = result
        .as_array()
        .or_else(|| result.get("models").and_then(|v| v.as_array()));

    if let Some(models) = models {
        let bold = Palette::bold();
        let muted = Palette::muted();

        println!(
            "{}",
            bold.apply_to(format!("Models ({} found)", models.len()))
        );

        let mut table = crate::terminal::table::styled_table();
        table.set_header(vec!["Model", "Provider", "Type"]);

        for model in models {
            let id = model.get("id").and_then(|v| v.as_str()).unwrap_or("-");
            let prov = model
                .get("provider")
                .and_then(|v| v.as_str())
                .unwrap_or("-");
            let kind = model
                .get("type")
                .or_else(|| model.get("kind"))
                .and_then(|v| v.as_str())
                .unwrap_or("chat");
            table.add_row(vec![id, prov, kind]);
        }

        println!("{table}");

        if models.is_empty() {
            println!("  {}", muted.apply_to("No models found."));
        }
    } else {
        println!("{}", serde_json::to_string_pretty(&result)?);
    }

    Ok(())
}

async fn cmd_status(
    json: bool,
    url: Option<&str>,
    token: Option<&str>,
    password: Option<&str>,
    timeout: u64,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);

    let result = crate::terminal::progress::with_spinner(
        "Fetching model status...",
        !json_mode,
        call_gateway(CallOptions {
            url: url.map(|s| s.to_string()),
            token: token.map(|s| s.to_string()),
            password: password.map(|s| s.to_string()),
            method: "models.status".to_string(),
            params: None,
            timeout_ms: timeout,
            expect_final: false,
        }),
    )
    .await?;

    if json_mode {
        println!("{}", serde_json::to_string_pretty(&result)?);
        return Ok(());
    }

    let bold = Palette::bold();
    let muted = Palette::muted();
    let success = Palette::success();

    println!("{}", bold.apply_to("Model Status"));

    if let Some(obj) = result.as_object() {
        if let Some(default) = obj.get("defaultModel").and_then(|v| v.as_str()) {
            println!("  Default: {}", success.apply_to(default));
        }
        if let Some(fallbacks) = obj.get("fallbacks").and_then(|v| v.as_array()) {
            if !fallbacks.is_empty() {
                let names: Vec<&str> = fallbacks.iter().filter_map(|v| v.as_str()).collect();
                println!("  Fallbacks: {}", muted.apply_to(names.join(", ")));
            }
        }
        if let Some(providers) = obj.get("providers").and_then(|v| v.as_array()) {
            println!(
                "  Providers: {}",
                muted.apply_to(format!("{} configured", providers.len()))
            );
        }
    }

    Ok(())
}
