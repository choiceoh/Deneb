use std::path::PathBuf;

#[derive(Debug, Clone)]
pub struct ConfigEnv {
    pub home_dir: PathBuf,
    pub state_dir_override: Option<PathBuf>,
    pub config_path_override: Option<PathBuf>,
    pub gateway_port_override: Option<u16>,
    pub fast_mode: bool,
}

/// Resolve the home directory, respecting `DENEB_HOME` override.
#[allow(clippy::expect_used)]
pub fn resolve_home_dir() -> PathBuf {
    if let Ok(v) = std::env::var("DENEB_HOME") {
        let v = v.trim().to_string();
        if !v.is_empty() {
            return expand_tilde(&v);
        }
    }
    dirs::home_dir().expect("cannot determine home directory")
}

/// Expand a leading `~` to the user's home directory.
#[allow(clippy::expect_used)]
fn expand_tilde(path: &str) -> PathBuf {
    if let Some(rest) = path.strip_prefix("~/") {
        let home = dirs::home_dir().expect("cannot determine home directory");
        home.join(rest)
    } else if path == "~" {
        dirs::home_dir().expect("cannot determine home directory")
    } else {
        PathBuf::from(path)
    }
}

/// Expand a user-provided path that may start with `~`.
pub fn expand_user_path(path: &str) -> PathBuf {
    expand_tilde(path)
}

/// Check if an environment variable is set to a truthy value (1, true, yes).
#[allow(dead_code)]
pub fn is_truthy_env(key: &str) -> bool {
    std::env::var(key).ok().is_some_and(|v| {
        let normalized = v.trim();
        normalized == "1"
            || normalized.eq_ignore_ascii_case("true")
            || normalized.eq_ignore_ascii_case("yes")
    })
}

/// Get a trimmed, non-empty env var or `None`.
pub fn get_env_trimmed(key: &str) -> Option<String> {
    std::env::var(key).ok().and_then(|v| {
        let trimmed = v.trim();
        (!trimmed.is_empty()).then(|| trimmed.to_owned())
    })
}

/// Read all environment-derived config inputs.
pub fn read_config_env() -> ConfigEnv {
    let home_dir = resolve_home_dir();
    let state_dir_override = get_env_trimmed("DENEB_STATE_DIR").map(|v| expand_user_path(&v));
    let config_path_override = get_env_trimmed("DENEB_CONFIG_PATH").map(|v| expand_user_path(&v));
    let gateway_port_override = get_env_trimmed("DENEB_GATEWAY_PORT")
        .and_then(|raw| raw.parse::<u16>().ok())
        .filter(|port| *port > 0);
    let fast_mode = std::env::var("DENEB_TEST_FAST").ok().as_deref() == Some("1");

    ConfigEnv {
        home_dir,
        state_dir_override,
        config_path_override,
        gateway_port_override,
        fast_mode,
    }
}
