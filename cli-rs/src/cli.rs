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

    /// Agent operations (list, add, delete, bind, unbind).
    Agents(commands::agents::AgentsArgs),

    /// Run a single agent turn via gateway.
    Agent(commands::agent::AgentArgs),

    /// Send messages to channel targets.
    Message(commands::message::MessageArgs),

    /// Memory search and management.
    Memory(commands::memory::MemoryArgs),

    // --- Interactive commands ---
    /// Initialize config and workspace.
    Setup(commands::setup::SetupArgs),

    /// Interactive onboarding wizard.
    Onboard(commands::onboard::OnboardArgs),

    /// Interactive configuration editor.
    Configure(commands::configure::ConfigureArgs),

    /// Run diagnostic checks and repairs.
    Doctor(commands::doctor::DoctorArgs),

    /// Open the control dashboard in the browser.
    Dashboard(commands::dashboard::DashboardArgs),

    // --- SubCLI commands ---
    /// Gateway management (status, call, usage-cost).
    Gateway(subcli::gateway_cmd::GatewayArgs),

    /// Run the gateway server (Go binary with Node.js plugin host).
    #[command(name = "gateway-run")]
    GatewayRun(subcli::gateway_run::GatewayRunArgs),

    /// Channel management and status.
    Channels(subcli::channels::ChannelsArgs),

    /// Tail gateway logs.
    Logs(subcli::logs::LogsArgs),

    /// Model discovery and configuration.
    Models(subcli::models::ModelsArgs),

    /// Manage plugins and extensions.
    Plugins(subcli::plugins::PluginsArgs),

    /// Self-update CLI.
    Update(subcli::update::UpdateArgs),

    /// Generate shell completions.
    Completion(subcli::completion::CompletionArgs),

    /// Manage sandbox containers.
    Sandbox(subcli::sandbox::SandboxArgs),

    /// QR code operations.
    Qr(subcli::qr::QrArgs),

    /// Cron job scheduling.
    Cron(subcli::cron::CronArgs),

    /// Hook management.
    Hooks(subcli::hooks::HooksArgs),

    /// Webhook management.
    Webhooks(subcli::webhooks::WebhooksArgs),

    /// Security audit.
    Security(subcli::security::SecurityArgs),

    /// Secret reference management.
    Secrets(subcli::secrets::SecretsArgs),

    /// Skill management.
    Skills(subcli::skills::SkillsArgs),

    /// Search documentation.
    Docs(subcli::docs::DocsArgs),

    /// Contact and group directory.
    Directory(subcli::directory::DirectoryArgs),

    /// Backup config and state.
    Backup(subcli::backup::BackupArgs),

    /// Reset config or state.
    Reset(subcli::reset::ResetArgs),

    /// Uninstall Deneb.
    Uninstall(subcli::uninstall::UninstallArgs),

    /// Daemon management.
    Daemon(subcli::daemon::DaemonArgs),

    /// Browser automation.
    Browser(subcli::browser::BrowserArgs),

    /// System events and presence.
    System(subcli::system::SystemArgs),

    /// Approval management.
    Approvals(subcli::approvals::ApprovalsArgs),

    /// Agent Control Protocol.
    Acp(subcli::acp::AcpArgs),
}
