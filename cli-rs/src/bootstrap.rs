use crate::cli::Cli;
use crate::env;
use crate::errors::CliError;
use crate::router;
use crate::terminal;

/// run executes the full CLI lifecycle and returns an OS exit code (0 = success, 1 = error).
///
/// Phases:
///  1. Env: apply CLI flag overrides to the process environment.
///  2. Dispatch: route the command to its handler.
///  3. Error display: format and print any failure to stderr.
pub async fn run(cli: Cli) -> i32 {
    apply_env_overrides(&cli);
    match router::dispatch(&cli.command).await {
        Ok(()) => 0,
        Err(e) => {
            print_error(&e);
            1
        }
    }
}

fn print_error(e: &CliError) {
    let error_style = terminal::Palette::error();
    eprintln!(
        "  {}  {}",
        error_style.apply_to(terminal::Symbols::ERROR),
        e.user_message()
    );
}

pub fn apply_env_overrides(cli: &Cli) {
    apply_no_color(cli.no_color);
    apply_state_dir_profile(cli.dev, cli.profile.as_deref());
}

fn apply_no_color(no_color: bool) {
    if no_color {
        std::env::set_var("NO_COLOR", "1");
    }
}

fn apply_state_dir_profile(dev: bool, profile: Option<&str>) {
    if dev {
        set_state_dir(".deneb-dev");
    }

    if let Some(profile_name) = profile {
        set_state_dir(&format!(".deneb-{profile_name}"));
    }
}

fn set_state_dir(suffix: &str) {
    let home = env::resolve_home_dir();
    let state_dir = home.join(suffix).to_string_lossy().to_string();
    std::env::set_var("DENEB_STATE_DIR", state_dir);
}
