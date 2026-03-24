use clap::Args;

use crate::errors::CliError;
use crate::terminal::Palette;

#[derive(Args, Debug)]
pub struct UpdateArgs {
    /// Release channel: stable, beta, dev.
    #[arg(long, default_value = "stable")]
    pub channel: String,

    /// Specific version tag.
    #[arg(long)]
    pub tag: Option<String>,

    /// Dry run (show what would be done).
    #[arg(long)]
    pub dry_run: bool,

    /// Skip restart after update.
    #[arg(long)]
    pub no_restart: bool,
}

pub async fn run(args: &UpdateArgs) -> Result<(), CliError> {
    let muted = Palette::muted();

    // The Rust CLI delegates update to the Node.js CLI
    // since it manages git/npm operations
    let mut cmd = std::process::Command::new("deneb");
    cmd.arg("update");
    cmd.arg("--channel").arg(&args.channel);

    if let Some(ref tag) = args.tag {
        cmd.arg("--tag").arg(tag);
    }
    if args.dry_run {
        cmd.arg("--dry-run");
    }
    if args.no_restart {
        cmd.arg("--no-restart");
    }

    println!(
        "{}",
        muted.apply_to("Delegating to Node.js CLI for update...")
    );

    let status = cmd
        .stdin(std::process::Stdio::inherit())
        .stdout(std::process::Stdio::inherit())
        .stderr(std::process::Stdio::inherit())
        .status()
        .map_err(|e| {
            CliError::User(format!(
                "Failed to run 'deneb update'. Is the Node.js CLI installed? Error: {e}"
            ))
        })?;

    if !status.success() {
        return Err(CliError::User(format!(
            "Update exited with code {}",
            status.code().unwrap_or(-1)
        )));
    }

    Ok(())
}
