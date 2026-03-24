use clap::{Parser, Subcommand};

use crate::commands;
use crate::subcli;

#[derive(Parser)]
#[command(
    name = "deneb",
    version,
    about = "Deneb CLI — high-performance command-line interface",
    long_about = "Deneb CLI (Rust). A thin client that communicates with the Deneb gateway via WebSocket RPC."
)]
pub struct Cli {
    /// Output raw JSON (where supported).
    #[arg(long, global = true)]
    pub json: bool,

    /// Dev profile (uses ~/.deneb-dev/).
    #[arg(long, global = true)]
    pub dev: bool,

    /// Named profile (uses ~/.deneb-{name}/).
    #[arg(long, global = true)]
    pub profile: Option<String>,

    /// Log level.
    #[arg(long, global = true, value_parser = ["debug", "info", "warn", "error"])]
    pub log_level: Option<String>,

    /// Disable colors.
    #[arg(long, global = true)]
    pub no_color: bool,

    #[command(subcommand)]
    pub command: Commands,
}

#[derive(Subcommand)]
pub enum Commands {
    // --- Core commands ---
    /// Check gateway health.
    Health(commands::health::HealthArgs),

    /// Show status summary.
    Status(commands::status::StatusArgs),

    /// Manage configuration.
    Config(commands::config_cmd::ConfigArgs),

    /// List and manage sessions.
    Sessions(commands::sessions::SessionsArgs),

    /// Agent operations.
    Agents(commands::agents::AgentsArgs),

    /// Memory search and management.
    Memory(commands::memory::MemoryArgs),

    // --- SubCLI commands ---
    /// Gateway management.
    Gateway(subcli::gateway_cmd::GatewayArgs),

    /// Tail gateway logs.
    Logs(subcli::logs::LogsArgs),

    /// Model discovery and configuration.
    Models(subcli::models::ModelsArgs),
}
