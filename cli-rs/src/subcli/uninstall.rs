use clap::Args;

use crate::config;
use crate::errors::CliError;
use crate::terminal::Palette;

#[derive(Args, Debug)]
pub struct UninstallArgs {
    /// Skip confirmation.
    #[arg(long)]
    pub force: bool,

    /// Also remove state directory (~/.deneb).
    #[arg(long)]
    pub purge: bool,
}

pub async fn run(args: &UninstallArgs) -> Result<(), CliError> {
    if !args.force {
        let msg = if args.purge {
            "Uninstall Deneb and remove all data? This cannot be undone."
        } else {
            "Uninstall Deneb? Config and data will be preserved."
        };
        let confirmed = dialoguer::Confirm::new()
            .with_prompt(msg)
            .default(false)
            .interact()
            .map_err(|e| CliError::User(format!("prompt failed: {e}")))?;

        if !confirmed {
            println!("Cancelled.");
            return Ok(());
        }
    }

    if args.purge {
        let state_dir = config::resolve_state_dir();
        if state_dir.exists() {
            std::fs::remove_dir_all(&state_dir)?;
            let muted = Palette::muted();
            println!("  Removed {}", muted.apply_to(state_dir.to_string_lossy()));
        }
    }

    // Delegate npm uninstall to the Node.js package manager
    let muted = Palette::muted();
    println!(
        "{}",
        muted.apply_to("To complete uninstall, run: npm uninstall -g deneb")
    );
    println!("{}", Palette::success().apply_to("Uninstall complete."));

    Ok(())
}
