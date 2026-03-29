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

#[tokio::main(flavor = "current_thread")]
async fn main() {
    let cli = Cli::parse();
    std::process::exit(bootstrap::run(cli).await);
}
