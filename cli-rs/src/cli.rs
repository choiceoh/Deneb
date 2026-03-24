use clap::{Parser, Subcommand};

use crate::commands;

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
    /// Check gateway health.
    Health(commands::health::HealthArgs),

    /// Show status summary.
    Status(commands::status::StatusArgs),

    /// Manage configuration.
    Config(commands::config_cmd::ConfigArgs),
    // --- Placeholder commands for Phase 2+ ---
    // Uncomment and implement as each command is ported.

    // /// Initialize config and workspace.
    // Setup(commands::setup::SetupArgs),

    // /// Interactive onboarding.
    // Onboard(commands::onboard::OnboardArgs),

    // /// Interactive configuration.
    // Configure(commands::configure::ConfigureArgs),

    // /// Create/verify backup archives.
    // Backup(commands::backup::BackupArgs),

    // /// Run diagnostic checks.
    // Doctor(commands::doctor::DoctorArgs),

    // /// Open the dashboard.
    // Dashboard(commands::dashboard::DashboardArgs),

    // /// Reset state.
    // Reset(commands::reset::ResetArgs),

    // /// Uninstall Deneb.
    // Uninstall(commands::uninstall::UninstallArgs),

    // /// Send and manage messages.
    // Message(commands::message::MessageArgs),

    // /// Memory search and management.
    // Memory(commands::memory::MemoryArgs),

    // /// Single agent turn.
    // Agent(commands::agent::AgentArgs),

    // /// Agent CRUD operations.
    // Agents(commands::agents::AgentsArgs),

    // /// List and manage sessions.
    // Sessions(commands::sessions::SessionsArgs),

    // /// Browser management.
    // Browser(commands::browser::BrowserArgs),

    // --- SubCLI commands (Phase 2+) ---

    // /// Gateway management.
    // Gateway(subcli::gateway_cmd::GatewayArgs),

    // /// Tail gateway logs.
    // Logs(subcli::logs::LogsArgs),

    // /// Model discovery and configuration.
    // Models(subcli::models::ModelsArgs),

    // /// Channel management.
    // Channels(subcli::channels::ChannelsArgs),

    // /// Plugin management.
    // Plugins(subcli::plugins::PluginsArgs),

    // /// Update management.
    // Update(subcli::update::UpdateArgs),

    // /// Shell completion generation.
    // Completion(subcli::completion::CompletionArgs),
}
