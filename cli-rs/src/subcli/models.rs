use clap::{Args, Subcommand};

use super::rpc_helpers::{rpc_print_fmt, GatewayFlags};
use crate::errors::CliError;
use crate::terminal::Palette;

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

        #[command(flatten)]
        gw: GatewayFlags,
    },

    /// Show configured model status.
    Status {
        #[command(flatten)]
        gw: GatewayFlags,
    },
}

pub async fn run(args: &ModelsArgs) -> Result<(), CliError> {
    match &args.command {
        ModelsCommand::List { all, provider, gw } => {
            let params = serde_json::json!({
                "all": all,
                "provider": provider,
            });
            rpc_print_fmt("models.list", params, gw, "Fetching models...", |result, json_mode| {
                if json_mode {
                    println!("{}", serde_json::to_string_pretty(&result)?);
                    return Ok(());
                }
                print_models_list(&result);
                Ok(())
            })
            .await
        }
        ModelsCommand::Status { gw } => {
            rpc_print_fmt(
                "models.status",
                serde_json::json!({}),
                gw,
                "Fetching model status...",
                |result, json_mode| {
                    if json_mode {
                        println!("{}", serde_json::to_string_pretty(&result)?);
                        return Ok(());
                    }
                    print_models_status(&result);
                    Ok(())
                },
            )
            .await
        }
    }
}

fn print_models_list(result: &serde_json::Value) {
    use crate::terminal::Symbols;
    let bold = Palette::bold();
    let muted = Palette::muted();

    let models = result
        .as_array()
        .or_else(|| result.get("models").and_then(|v| v.as_array()));

    if let Some(models) = models {
        println!();
        println!(
            "  {}  {}  {}",
            bold.apply_to("Models"),
            muted.apply_to(Symbols::ARROW),
            muted.apply_to(format!("{} found", models.len()))
        );
        println!();

        let mut table = crate::terminal::table::styled_table();
        table.set_header(vec!["Model", "Provider", "Type"]);

        for model in models {
            let id = model.get("id").and_then(|v| v.as_str()).unwrap_or(Symbols::DASH);
            let prov = model
                .get("provider")
                .and_then(|v| v.as_str())
                .unwrap_or(Symbols::DASH);
            let kind = model
                .get("type")
                .or_else(|| model.get("kind"))
                .and_then(|v| v.as_str())
                .unwrap_or("chat");
            table.add_row(vec![id, prov, kind]);
        }

        println!("{table}");
        println!();

        if models.is_empty() {
            println!("    {}", muted.apply_to("No models found."));
            println!();
        }
    } else {
        println!("{}", serde_json::to_string_pretty(result).unwrap_or_default());
    }
}

fn print_models_status(result: &serde_json::Value) {
    let bold = Palette::bold();
    let muted = Palette::muted();
    let success = Palette::success();

    println!();
    println!("  {}", bold.apply_to("Model Status"));
    println!();

    if let Some(obj) = result.as_object() {
        if let Some(default) = obj.get("defaultModel").and_then(|v| v.as_str()) {
            println!("    Default      {}", success.apply_to(default));
        }
        if let Some(fallbacks) = obj.get("fallbacks").and_then(|v| v.as_array()) {
            if !fallbacks.is_empty() {
                let names: Vec<&str> = fallbacks.iter().filter_map(|v| v.as_str()).collect();
                println!("    Fallbacks    {}", muted.apply_to(names.join(", ")));
            }
        }
        if let Some(providers) = obj.get("providers").and_then(|v| v.as_array()) {
            println!(
                "    Providers    {}",
                muted.apply_to(format!("{} configured", providers.len()))
            );
        }
    }
    println!();
}
