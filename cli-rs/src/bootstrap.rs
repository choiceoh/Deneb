use crate::cli::Cli;
use crate::env;

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
