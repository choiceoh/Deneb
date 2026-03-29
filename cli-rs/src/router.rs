use crate::cli::Commands;
use crate::errors::CliError;
use crate::{commands, subcli};

pub async fn dispatch(command: &Commands) -> Result<(), CliError> {
    match command {
        Commands::Health(_)
        | Commands::Status(_)
        | Commands::Config(_)
        | Commands::Sessions(_)
        | Commands::Agents(_)
        | Commands::Agent(_)
        | Commands::Message(_)
        | Commands::Memory(_) => dispatch_core(command).await,
        Commands::Setup(_)
        | Commands::Onboard(_)
        | Commands::Configure(_)
        | Commands::Doctor(_)
        | Commands::Dashboard(_) => dispatch_interactive(command).await,
        _ => dispatch_subcli(command).await,
    }
}

async fn dispatch_core(command: &Commands) -> Result<(), CliError> {
    match command {
        Commands::Health(args) => commands::health::run(args).await,
        Commands::Status(args) => commands::status::run(args).await,
        Commands::Config(args) => commands::config_cmd::run(args).await,
        Commands::Sessions(args) => commands::sessions::run(args).await,
        Commands::Agents(args) => commands::agents::run(args).await,
        Commands::Agent(args) => commands::agent::run(args).await,
        Commands::Message(args) => commands::message::run(args).await,
        Commands::Memory(args) => commands::memory::run(args).await,
        _ => unreachable!("non-core command routed to core dispatcher"),
    }
}

async fn dispatch_interactive(command: &Commands) -> Result<(), CliError> {
    match command {
        Commands::Setup(args) => commands::setup::run(args).await,
        Commands::Onboard(args) => commands::onboard::run(args).await,
        Commands::Configure(args) => commands::configure::run(args).await,
        Commands::Doctor(args) => commands::doctor::run(args).await,
        Commands::Dashboard(args) => commands::dashboard::run(args).await,
        _ => unreachable!("non-interactive command routed to interactive dispatcher"),
    }
}

async fn dispatch_subcli(command: &Commands) -> Result<(), CliError> {
    match command {
        Commands::Gateway(args) => subcli::gateway_cmd::run(args).await,
        Commands::GatewayRun(args) => subcli::gateway_run::run(args).await,
        Commands::Channels(args) => subcli::channels::run(args).await,
        Commands::Logs(args) => subcli::logs::run(args).await,
        Commands::Models(args) => subcli::models::run(args).await,
        Commands::Plugins(args) => subcli::plugins::run(args).await,
        Commands::Update(args) => subcli::update::run(args).await,
        Commands::Completion(args) => subcli::completion::run(args).await,
        Commands::Sandbox(args) => subcli::sandbox::run(args).await,
        Commands::Nodes(args) => subcli::nodes::run(args).await,
        Commands::Devices(args) => subcli::devices::run(args).await,
        Commands::Pairing(args) => subcli::pairing::run(args).await,
        Commands::Qr(args) => subcli::qr::run(args).await,
        Commands::Cron(args) => subcli::cron::run(args).await,
        Commands::Hooks(args) => subcli::hooks::run(args).await,
        Commands::Webhooks(args) => subcli::webhooks::run(args).await,
        Commands::Security(args) => subcli::security::run(args).await,
        Commands::Secrets(args) => subcli::secrets::run(args).await,
        Commands::Skills(args) => subcli::skills::run(args).await,
        Commands::Docs(args) => subcli::docs::run(args).await,
        Commands::Directory(args) => subcli::directory::run(args).await,
        Commands::Backup(args) => subcli::backup::run(args).await,
        Commands::Reset(args) => subcli::reset::run(args).await,
        Commands::Uninstall(args) => subcli::uninstall::run(args).await,
        Commands::Daemon(args) => subcli::daemon::run(args).await,
        Commands::Browser(args) => subcli::browser::run(args).await,
        Commands::System(args) => subcli::system::run(args).await,
        Commands::Approvals(args) => subcli::approvals::run(args).await,
        Commands::Acp(args) => subcli::acp::run(args).await,
        _ => unreachable!("non-subcli command routed to subcli dispatcher"),
    }
}
