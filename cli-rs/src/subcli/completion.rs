use clap::Args;
use clap_complete::{generate, Shell};

use crate::cli::Cli;
use crate::errors::CliError;

#[derive(Args, Debug)]
pub struct CompletionArgs {
    /// Shell to generate completions for.
    #[arg(long, value_enum)]
    pub shell: Shell,
}

pub async fn run(args: &CompletionArgs) -> Result<(), CliError> {
    let mut cmd = <Cli as clap::CommandFactory>::command();
    generate(args.shell, &mut cmd, "deneb", &mut std::io::stdout());
    Ok(())
}
