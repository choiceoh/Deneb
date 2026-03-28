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
            rpc_action(RpcActionParams {
                method: "plugins.enable",
                params: serde_json::json!({"id": id}),
                json: *json,
                url,
                token,
                password,
                timeout: *timeout,
                success_msg: &format!("Plugin '{id}' enabled."),
            })
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
            rpc_action(RpcActionParams {
                method: "plugins.disable",
                params: serde_json::json!({"id": id}),
                json: *json,
                url,
                token,
                password,
                timeout: *timeout,
                success_msg: &format!("Plugin '{id}' disabled."),
            })
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
            rpc_action(RpcActionParams {
                method: "plugins.install",
                params: serde_json::json!({"source": source}),
                json: *json,
                url,
                token,
                password,
                timeout: *timeout,
                success_msg: &format!("Plugin '{source}' installed."),
            })
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
            rpc_action(RpcActionParams {
                method: "plugins.uninstall",
                params: serde_json::json!({"id": id, "force": force}),
                json: *json,
                url,
                token,
                password,
                timeout: *timeout,
                success_msg: &format!("Plugin '{id}' uninstalled."),
            })
            .await
        }
    }
}

fn print_plugins_table(result: &serde_json::Value) {
    use crate::terminal::Symbols;
    let plugins = result
        .as_array()
        .or_else(|| result.get("plugins").and_then(|p| p.as_array()));
    let Some(plugins) = plugins else {
        println!("    {}", Palette::muted().apply_to("No plugins installed."));
        return;
    };
    let bold = Palette::bold();
    let muted = Palette::muted();
    println!();
    println!(
        "  {}  {}  {}",
        bold.apply_to("Plugins"),
        muted.apply_to(Symbols::ARROW),
        muted.apply_to(format!("{} installed", plugins.len()))
    );
    println!();
    let mut table = styled_table();
    table.set_header(vec!["ID", "Version", "Enabled"]);
    for p in plugins {
        let id = p.get("id").and_then(|v| v.as_str()).unwrap_or(Symbols::DASH);
        let version = p.get("version").and_then(|v| v.as_str()).unwrap_or(Symbols::DASH);
        let enabled = p
            .get("enabled")
            .and_then(|v| v.as_bool())
            .map(|b| if b { Symbols::DOT_FILLED } else { Symbols::DASH })
            .unwrap_or(Symbols::DASH);
        table.add_row(vec![id, version, enabled]);
    }
    println!("{table}");
    println!();
}

async fn rpc_simple(
    method: &str,
    params: serde_json::Value,
    _json: bool,
    url: &Option<String>,
    token: &Option<String>,
    password: &Option<String>,
    timeout: u64,
) -> Result<(), CliError> {
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
    println!("{}", serde_json::to_string_pretty(&result)?);
    Ok(())
}

struct RpcActionParams<'a> {
    method: &'a str,
    params: serde_json::Value,
    json: bool,
    url: &'a Option<String>,
    token: &'a Option<String>,
    password: &'a Option<String>,
    timeout: u64,
    success_msg: &'a str,
}

async fn rpc_action(p: RpcActionParams<'_>) -> Result<(), CliError> {
    let json_mode = is_json_mode(p.json);
    let result = crate::terminal::progress::with_spinner(
        "Working...",
        !json_mode,
        call_gateway(CallOptions {
            url: p.url.clone(),
            token: p.token.clone(),
            password: p.password.clone(),
            method: p.method.to_string(),
            params: Some(p.params),
            timeout_ms: p.timeout,
            expect_final: false,
        }),
    )
    .await?;
    if json_mode {
        println!("{}", serde_json::to_string_pretty(&result)?);
    } else {
        use crate::terminal::Symbols;
        println!(
            "    {}  {}",
            Palette::success().apply_to(Symbols::SUCCESS),
            Palette::success().apply_to(p.success_msg)
        );
    }
    Ok(())
}
