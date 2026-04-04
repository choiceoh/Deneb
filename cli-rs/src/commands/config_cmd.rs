use clap::{Args, Subcommand};

use crate::config::{self, io as config_io};
use crate::errors::CliError;
use crate::terminal::Palette;

#[derive(Args, Debug)]
pub struct ConfigArgs {
    #[command(subcommand)]
    pub command: ConfigCommand,
}

#[derive(Subcommand, Debug)]
pub enum ConfigCommand {
    /// Get a config value by dot-separated path.
    Get {
        /// Config key path (e.g. "gateway.port").
        path: String,

        /// Output raw JSON value (no formatting).
        #[arg(long)]
        json: bool,
    },

    /// Set a config value by dot-separated path.
    Set {
        /// Config key path (e.g. "gateway.port").
        path: String,

        /// Value to set (JSON-parsed, or string if not valid JSON).
        value: String,
    },

    /// Remove a config value by dot-separated path.
    Unset {
        /// Config key path (e.g. "gateway.port").
        path: String,
    },

    /// Show the config file path.
    File,

    /// Validate the config file.
    Validate,
}

pub async fn run(args: &ConfigArgs) -> Result<(), CliError> {
    match &args.command {
        ConfigCommand::Get { path, json } => cmd_get(path, *json),
        ConfigCommand::Set { path, value } => cmd_set(path, value),
        ConfigCommand::Unset { path } => cmd_unset(path),
        ConfigCommand::File => cmd_file(),
        ConfigCommand::Validate => cmd_validate(),
    }
}

fn cmd_get(path: &str, json_output: bool) -> Result<(), CliError> {
    let config_path = config::resolve_config_path();
    let config = config::load_config(&config_path)?;

    match config.get_path(path) {
        Some(value) => {
            if json_output {
                println!("{}", serde_json::to_string_pretty(&value)?);
            } else {
                // Print simple values without quotes
                match &value {
                    serde_json::Value::String(s) => println!("{s}"),
                    serde_json::Value::Number(n) => println!("{n}"),
                    serde_json::Value::Bool(b) => println!("{b}"),
                    serde_json::Value::Null => println!("null"),
                    _ => println!("{}", serde_json::to_string_pretty(&value)?),
                }
            }
            Ok(())
        }
        None => {
            let muted = Palette::muted();
            eprintln!("{}", muted.apply_to(format!("Key not found: {path}")));
            std::process::exit(1);
        }
    }
}

fn cmd_set(path: &str, value_str: &str) -> Result<(), CliError> {
    let config_path = config::resolve_config_path();
    let mut config = config::load_config(&config_path)?;

    // Try to parse as JSON, fall back to string
    let value: serde_json::Value = serde_json::from_str(value_str)
        .unwrap_or_else(|_| serde_json::Value::String(value_str.to_string()));

    config_io::set_config_value(&mut config, path, &value)?;
    config_io::write_config(&config_path, &config)?;

    let success = Palette::success();
    println!("{} {path} = {value}", success.apply_to("Set"));
    Ok(())
}

fn cmd_unset(path: &str) -> Result<(), CliError> {
    let config_path = config::resolve_config_path();
    let mut config = config::load_config(&config_path)?;

    let removed = config_io::unset_config_value(&mut config, path)?;
    if removed {
        config_io::write_config(&config_path, &config)?;
        let success = Palette::success();
        println!("{} {path}", success.apply_to("Unset"));
    } else {
        let muted = Palette::muted();
        println!("{}", muted.apply_to(format!("Key not found: {path}")));
    }
    Ok(())
}

fn cmd_file() -> Result<(), CliError> {
    let config_path = config::resolve_config_path();
    println!("{}", config_path.display());
    Ok(())
}

fn cmd_validate() -> Result<(), CliError> {
    let config_path = config::resolve_config_path();

    if !config_path.exists() {
        let muted = Palette::muted();
        println!(
            "{} (no config file at {})",
            muted.apply_to("No config file found"),
            config_path.display()
        );
        return Ok(());
    }

    match config::load_config(&config_path) {
        Ok(_config) => {
            let success = Palette::success();
            println!("{} {}", success.apply_to("Valid"), config_path.display());
            Ok(())
        }
        Err(e) => {
            let error = Palette::error();
            eprintln!(
                "{} {}: {}",
                error.apply_to("Invalid"),
                config_path.display(),
                e.user_message()
            );
            std::process::exit(1);
        }
    }
}
