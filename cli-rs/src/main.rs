mod bootstrap;
mod cli;
mod commands;
mod config;
mod env;
mod errors;
mod gateway;
mod router;
mod subcli;
mod terminal;
mod version;

use clap::Parser;

use cli::Cli;

#[tokio::main]
async fn main() {
    let cli = Cli::parse();

    bootstrap::apply_env_overrides(&cli);

    let result = router::dispatch(&cli.command).await;

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
