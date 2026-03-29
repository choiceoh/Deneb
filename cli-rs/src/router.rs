use crate::cli::Commands;
use crate::errors::CliError;

pub async fn dispatch(command: &Commands) -> Result<(), CliError> {
    match command {
        // Runtime domain
        Commands::Health(_) | Commands::Status(_) | Commands::Doctor(_) => {
            runtime::dispatch(command).await
        }

        // Messaging domain
        Commands::Message(_)
        | Commands::Channels(_)
        | Commands::Webhooks(_)
        | Commands::Hooks(_)
        | Commands::Directory(_)
        | Commands::System(_) => messaging::dispatch(command).await,

        // Admin domain
        Commands::Config(_)
        | Commands::Configure(_)
        | Commands::Secrets(_)
        | Commands::Security(_)
        | Commands::Approvals(_)
        | Commands::Backup(_)
        | Commands::Reset(_)
        | Commands::Uninstall(_)
        | Commands::Setup(_)
        | Commands::Onboard(_) => admin::dispatch(command).await,

        // Platform domain
        Commands::Gateway(_)
        | Commands::GatewayRun(_)
        | Commands::Logs(_)
        | Commands::Nodes(_)
        | Commands::Devices(_)
        | Commands::Pairing(_)
        | Commands::Qr(_)
        | Commands::Cron(_)
        | Commands::Daemon(_)
        | Commands::Sandbox(_)
        | Commands::Browser(_)
        | Commands::Acp(_) => platform::dispatch(command).await,

        // Developer domain
        Commands::Agents(_)
        | Commands::Agent(_)
        | Commands::Sessions(_)
        | Commands::Memory(_)
        | Commands::Dashboard(_)
        | Commands::Models(_)
        | Commands::Plugins(_)
        | Commands::Update(_)
        | Commands::Completion(_)
        | Commands::Skills(_)
        | Commands::Docs(_) => developer::dispatch(command).await,
    }
}

mod runtime {
    use super::*;

    pub async fn dispatch(command: &Commands) -> Result<(), CliError> {
        match command {
            Commands::Health(args) => crate::commands::domains::runtime::health::run(args).await,
            Commands::Status(args) => crate::commands::domains::runtime::status::run(args).await,
            Commands::Doctor(args) => crate::commands::domains::runtime::doctor::run(args).await,
            _ => unreachable!("runtime router received non-runtime command"),
        }
    }
}

mod messaging {
    use super::*;

    pub async fn dispatch(command: &Commands) -> Result<(), CliError> {
        match command {
            Commands::Message(args) => {
                crate::commands::domains::messaging::message::run(args).await
            }
            Commands::Channels(args) => {
                crate::subcli::domains::messaging::channels::run(args).await
            }
            Commands::Webhooks(args) => {
                crate::subcli::domains::messaging::webhooks::run(args).await
            }
            Commands::Hooks(args) => crate::subcli::domains::messaging::hooks::run(args).await,
            Commands::Directory(args) => {
                crate::subcli::domains::messaging::directory::run(args).await
            }
            Commands::System(args) => crate::subcli::domains::messaging::system::run(args).await,
            _ => unreachable!("messaging router received non-messaging command"),
        }
    }
}

mod admin {
    use super::*;

    pub async fn dispatch(command: &Commands) -> Result<(), CliError> {
        match command {
            Commands::Config(args) => crate::commands::domains::admin::config_cmd::run(args).await,
            Commands::Configure(args) => {
                crate::commands::domains::admin::configure::run(args).await
            }
            Commands::Secrets(args) => crate::subcli::domains::admin::secrets::run(args).await,
            Commands::Security(args) => crate::subcli::domains::admin::security::run(args).await,
            Commands::Approvals(args) => crate::subcli::domains::admin::approvals::run(args).await,
            Commands::Backup(args) => crate::subcli::domains::admin::backup::run(args).await,
            Commands::Reset(args) => crate::subcli::domains::admin::reset::run(args).await,
            Commands::Uninstall(args) => crate::subcli::domains::admin::uninstall::run(args).await,
            Commands::Setup(args) => crate::commands::domains::admin::setup::run(args).await,
            Commands::Onboard(args) => crate::commands::domains::admin::onboard::run(args).await,
            _ => unreachable!("admin router received non-admin command"),
        }
    }
}

mod platform {
    use super::*;

    pub async fn dispatch(command: &Commands) -> Result<(), CliError> {
        match command {
            Commands::Gateway(args) => {
                crate::subcli::domains::runtime::gateway_cmd::run(args).await
            }
            Commands::GatewayRun(args) => {
                crate::subcli::domains::runtime::gateway_run::run(args).await
            }
            Commands::Logs(args) => crate::subcli::domains::runtime::logs::run(args).await,
            Commands::Nodes(args) => crate::subcli::domains::platform::nodes::run(args).await,
            Commands::Devices(args) => crate::subcli::domains::platform::devices::run(args).await,
            Commands::Pairing(args) => crate::subcli::domains::platform::pairing::run(args).await,
            Commands::Qr(args) => crate::subcli::domains::platform::qr::run(args).await,
            Commands::Cron(args) => crate::subcli::domains::platform::cron::run(args).await,
            Commands::Daemon(args) => crate::subcli::domains::runtime::daemon::run(args).await,
            Commands::Sandbox(args) => crate::subcli::domains::runtime::sandbox::run(args).await,
            Commands::Browser(args) => crate::subcli::domains::platform::browser::run(args).await,
            Commands::Acp(args) => crate::subcli::domains::platform::acp::run(args).await,
            _ => unreachable!("platform router received non-platform command"),
        }
    }
}

mod developer {
    use super::*;

    pub async fn dispatch(command: &Commands) -> Result<(), CliError> {
        match command {
            Commands::Agents(args) => crate::commands::domains::developer::agents::run(args).await,
            Commands::Agent(args) => crate::commands::domains::developer::agent::run(args).await,
            Commands::Sessions(args) => {
                crate::commands::domains::developer::sessions::run(args).await
            }
            Commands::Memory(args) => crate::commands::domains::developer::memory::run(args).await,
            Commands::Dashboard(args) => {
                crate::commands::domains::developer::dashboard::run(args).await
            }
            Commands::Models(args) => crate::subcli::domains::developer::models::run(args).await,
            Commands::Plugins(args) => crate::subcli::domains::developer::plugins::run(args).await,
            Commands::Update(args) => crate::subcli::domains::developer::update::run(args).await,
            Commands::Completion(args) => {
                crate::subcli::domains::developer::completion::run(args).await
            }
            Commands::Skills(args) => crate::subcli::domains::developer::skills::run(args).await,
            Commands::Docs(args) => crate::subcli::domains::developer::docs::run(args).await,
            _ => unreachable!("developer router received non-developer command"),
        }
    }
}
