use clap::{Args, Subcommand};

use crate::errors::CliError;
use crate::gateway::{call_gateway, CallOptions};
use crate::terminal::{is_json_mode, styled_table, Palette};

#[derive(Args, Debug)]
pub struct PluginsArgs {
    #[command(subcommand)]
    pub command: PluginsCommand,
}

#[derive(Subcommand, Debug)]
pub enum PluginsCommand {
    /// List installed plugins.
    List {
        #[arg(long)]
        all: bool,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        url: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        password: Option<String>,
        #[arg(long, default_value = "10000")]
        timeout: u64,
    },
    /// Show plugin info.
    Info {
        /// Plugin ID.
        id: String,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        url: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        password: Option<String>,
        #[arg(long, default_value = "10000")]
        timeout: u64,
    },
    /// Enable a plugin.
    Enable {
        /// Plugin ID.
        id: String,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        url: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        password: Option<String>,
        #[arg(long, default_value = "10000")]
        timeout: u64,
    },
    /// Disable a plugin.
    Disable {
        /// Plugin ID.
        id: String,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        url: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        password: Option<String>,
        #[arg(long, default_value = "10000")]
        timeout: u64,
    },
    /// Install a plugin.
    Install {
        /// Plugin package name or path.
        source: String,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        url: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        password: Option<String>,
        #[arg(long, default_value = "30000")]
        timeout: u64,
    },
    /// Uninstall a plugin.
    Uninstall {
        /// Plugin ID.
        id: String,
        #[arg(long)]
        force: bool,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        url: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        password: Option<String>,
        #[arg(long, default_value = "30000")]
        timeout: u64,
    },
}

pub async fn run(args: &PluginsArgs) -> Result<(), CliError> {
    match &args.command {
        PluginsCommand::List {
            all,
            json,
            url,
            token,
            password,
            timeout,
        } => {
            let json_mode = is_json_mode(*json);
            let result = crate::terminal::progress::with_spinner(
                "Fetching plugins...",
                !json_mode,
                call_gateway(CallOptions {
                    url: url.clone(),
                    token: token.clone(),
                    password: password.clone(),
                    method: "plugins.list".to_string(),
                    params: Some(serde_json::json!({"all": all})),
                    timeout_ms: *timeout,
                    expect_final: false,
                }),
            )
            .await?;
            if json_mode {
                println!("{}", serde_json::to_string_pretty(&result)?);
            } else {
                print_plugins_table(&result);
            }
            Ok(())
        }
        PluginsCommand::Info {
            id,
            json,
            url,
            token,
            password,
            timeout,
        } => {
            rpc_simple(
                "plugins.info",
                serde_json::json!({"id": id}),
                *json,
                url,
                token,
                password,
                *timeout,
            )
            .await
        }
        PluginsCommand::Enable {
            id,
            json,
            url,
            token,
            password,
            timeout,
        } => {
            rpc_action(
                "plugins.enable",
                serde_json::json!({"id": id}),
                *json,
                url,
                token,
                password,
                *timeout,
                &format!("Plugin '{id}' enabled."),
            )
            .await
        }
        PluginsCommand::Disable {
            id,
            json,
            url,
            token,
            password,
            timeout,
        } => {
            rpc_action(
                "plugins.disable",
                serde_json::json!({"id": id}),
                *json,
                url,
                token,
                password,
                *timeout,
                &format!("Plugin '{id}' disabled."),
            )
            .await
        }
        PluginsCommand::Install {
            source,
            json,
            url,
            token,
            password,
            timeout,
        } => {
            rpc_action(
                "plugins.install",
                serde_json::json!({"source": source}),
                *json,
                url,
                token,
                password,
                *timeout,
                &format!("Plugin '{source}' installed."),
            )
            .await
        }
        PluginsCommand::Uninstall {
            id,
            force,
            json,
            url,
            token,
            password,
            timeout,
        } => {
            rpc_action(
                "plugins.uninstall",
                serde_json::json!({"id": id, "force": force}),
                *json,
                url,
                token,
                password,
                *timeout,
                &format!("Plugin '{id}' uninstalled."),
            )
            .await
        }
    }
}

fn print_plugins_table(result: &serde_json::Value) {
    let plugins = result
        .as_array()
        .or_else(|| result.get("plugins").and_then(|p| p.as_array()));
    let Some(plugins) = plugins else {
        println!("{}", Palette::muted().apply_to("No plugins installed."));
        return;
    };
    let bold = Palette::bold();
    println!(
        "{}",
        bold.apply_to(format!("Plugins ({} installed)", plugins.len()))
    );
    let mut table = styled_table();
    table.set_header(vec!["ID", "Version", "Enabled"]);
    for p in plugins {
        let id = p.get("id").and_then(|v| v.as_str()).unwrap_or("-");
        let version = p.get("version").and_then(|v| v.as_str()).unwrap_or("-");
        let enabled = p
            .get("enabled")
            .and_then(|v| v.as_bool())
            .map(|b| if b { "yes" } else { "no" })
            .unwrap_or("-");
        table.add_row(vec![id, version, enabled]);
    }
    println!("{table}");
}

async fn rpc_simple(
    method: &str,
    params: serde_json::Value,
    json: bool,
    url: &Option<String>,
    token: &Option<String>,
    password: &Option<String>,
    timeout: u64,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);
    let result = call_gateway(CallOptions {
        url: url.clone(),
        token: token.clone(),
        password: password.clone(),
        method: method.to_string(),
        params: Some(params),
        timeout_ms: timeout,
        expect_final: false,
    })
    .await?;
    if json_mode {
        println!("{}", serde_json::to_string_pretty(&result)?);
    } else {
        println!("{}", serde_json::to_string_pretty(&result)?);
    }
    Ok(())
}

async fn rpc_action(
    method: &str,
    params: serde_json::Value,
    json: bool,
    url: &Option<String>,
    token: &Option<String>,
    password: &Option<String>,
    timeout: u64,
    success_msg: &str,
) -> Result<(), CliError> {
    let json_mode = is_json_mode(json);
    let result = crate::terminal::progress::with_spinner(
        "Working...",
        !json_mode,
        call_gateway(CallOptions {
            url: url.clone(),
            token: token.clone(),
            password: password.clone(),
            method: method.to_string(),
            params: Some(params),
            timeout_ms: timeout,
            expect_final: false,
        }),
    )
    .await?;
    if json_mode {
        println!("{}", serde_json::to_string_pretty(&result)?);
    } else {
        println!("{}", Palette::success().apply_to(success_msg));
    }
    Ok(())
}
