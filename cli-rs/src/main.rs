mod cli;
mod command_router;
mod commands;
mod config;
mod env;
mod errors;
mod gateway;
mod subcli;
mod terminal;
mod version;

use clap::Parser;

use cli::Cli;

#[tokio::main]
async fn main() {
    let cli = Cli::parse();

    // Handle --no-color flag
    if cli.no_color {
        std::env::set_var("NO_COLOR", "1");
    }

    // Handle --dev profile
    if cli.dev {
        std::env::set_var("DENEB_STATE_DIR", {
            let home = env::resolve_home_dir();
            home.join(".deneb-dev").to_string_lossy().to_string()
        });
    }

    // Handle --profile flag
    if let Some(ref profile) = cli.profile {
        std::env::set_var("DENEB_STATE_DIR", {
            let home = env::resolve_home_dir();
            home.join(format!(".deneb-{profile}"))
                .to_string_lossy()
                .to_string()
        });
    }

    let result = command_router::dispatch(&cli.command).await;

    if let Err(e) = result {
        let error_style = terminal::Palette::error();
        eprintln!(
            "  {}  {}",
            error_style.apply_to(terminal::Symbols::ERROR),
            e.user_message()
        );
        std::process::exit(1);
    }
}
