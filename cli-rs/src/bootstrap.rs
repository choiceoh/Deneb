use crate::cli::Cli;
use crate::env;

pub fn apply_runtime_env(cli: &Cli) {
    if cli.no_color {
        std::env::set_var("NO_COLOR", "1");
    }

    if cli.dev {
        std::env::set_var("DENEB_STATE_DIR", {
            let home = env::resolve_home_dir();
            home.join(".deneb-dev").to_string_lossy().to_string()
        });
    }

    if let Some(profile) = &cli.profile {
        std::env::set_var("DENEB_STATE_DIR", {
            let home = env::resolve_home_dir();
            home.join(format!(".deneb-{profile}"))
                .to_string_lossy()
                .to_string()
        });
    }
}

#[cfg(test)]
mod tests {
    use super::apply_runtime_env;
    use crate::cli::{Cli, Commands};
    use clap::Parser;

    fn parse_cli(input: &[&str]) -> Cli {
        Cli::try_parse_from(input).expect("test CLI args should parse")
    }

    #[test]
    fn applies_no_color_env_when_flag_present() {
        let cli = parse_cli(&["deneb", "--no-color", "status"]);
        std::env::remove_var("NO_COLOR");

        apply_runtime_env(&cli);

        assert_eq!(std::env::var("NO_COLOR").ok().as_deref(), Some("1"));
        assert!(matches!(cli.command, Commands::Status(_)));
        std::env::remove_var("NO_COLOR");
    }
}
