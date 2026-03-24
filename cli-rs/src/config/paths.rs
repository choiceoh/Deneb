use std::path::{Path, PathBuf};

use crate::env::{get_env_trimmed, resolve_home_dir};

const NEW_STATE_DIRNAME: &str = ".deneb";
const CONFIG_FILENAME: &str = "deneb.json";
const LEGACY_STATE_DIRNAMES: &[&str] = &[".clawdbot", ".moldbot", ".moltbot"];
const LEGACY_CONFIG_FILENAMES: &[&str] = &["clawdbot.json", "moldbot.json", "moltbot.json"];

pub const DEFAULT_GATEWAY_PORT: u16 = 18789;

/// Resolve the state directory.
///
/// Precedence:
/// 1. `DENEB_STATE_DIR` / `CLAWDBOT_STATE_DIR` env
/// 2. `~/.deneb` (if exists)
/// 3. First existing legacy dir (`~/.clawdbot`, etc.)
/// 4. `~/.deneb` (default)
pub fn resolve_state_dir() -> PathBuf {
    let home = resolve_home_dir();

    // Env override
    if let Some(dir) =
        get_env_trimmed("DENEB_STATE_DIR").or_else(|| get_env_trimmed("CLAWDBOT_STATE_DIR"))
    {
        return crate::env::resolve_home_dir()
            .parent()
            .map(|_| expand_user_path(&dir))
            .unwrap_or_else(|| PathBuf::from(&dir));
    }

    let new_dir = home.join(NEW_STATE_DIRNAME);

    // Fast-test mode: skip legacy checks
    if std::env::var("DENEB_TEST_FAST").ok().as_deref() == Some("1") {
        return new_dir;
    }

    if new_dir.exists() {
        return new_dir;
    }

    // Check legacy dirs
    for legacy in LEGACY_STATE_DIRNAMES {
        let dir = home.join(legacy);
        if dir.exists() {
            return dir;
        }
    }

    new_dir
}

/// Resolve the config file path.
///
/// Precedence:
/// 1. `DENEB_CONFIG_PATH` / `CLAWDBOT_CONFIG_PATH` env
/// 2. First existing config candidate in state dir
/// 3. `{state_dir}/deneb.json`
pub fn resolve_config_path() -> PathBuf {
    let state_dir = resolve_state_dir();

    // Env override
    if let Some(path) =
        get_env_trimmed("DENEB_CONFIG_PATH").or_else(|| get_env_trimmed("CLAWDBOT_CONFIG_PATH"))
    {
        return expand_user_path(&path);
    }

    // Check candidates in order
    let candidates = config_candidates(&state_dir);
    for candidate in &candidates {
        if candidate.exists() {
            return candidate.clone();
        }
    }

    state_dir.join(CONFIG_FILENAME)
}

/// Build the list of config file candidates for a given state dir.
fn config_candidates(state_dir: &Path) -> Vec<PathBuf> {
    let mut candidates = Vec::with_capacity(4);
    candidates.push(state_dir.join(CONFIG_FILENAME));
    for name in LEGACY_CONFIG_FILENAMES {
        candidates.push(state_dir.join(name));
    }
    candidates
}

/// Resolve the gateway port from config and environment.
pub fn resolve_gateway_port(config_port: Option<u16>) -> u16 {
    // Env override
    if let Some(raw) =
        get_env_trimmed("DENEB_GATEWAY_PORT").or_else(|| get_env_trimmed("CLAWDBOT_GATEWAY_PORT"))
    {
        if let Ok(port) = raw.parse::<u16>() {
            if port > 0 {
                return port;
            }
        }
    }

    if let Some(port) = config_port {
        if port > 0 {
            return port;
        }
    }

    DEFAULT_GATEWAY_PORT
}

/// Expand `~` prefix in a path string to the user's home directory.
fn expand_user_path(input: &str) -> PathBuf {
    if let Some(rest) = input.strip_prefix("~/") {
        resolve_home_dir().join(rest)
    } else if input == "~" {
        resolve_home_dir()
    } else {
        PathBuf::from(input)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_gateway_port_value() {
        assert_eq!(DEFAULT_GATEWAY_PORT, 18789);
    }

    #[test]
    fn resolve_gateway_port_uses_config() {
        // Clear env to ensure clean test
        std::env::remove_var("DENEB_GATEWAY_PORT");
        std::env::remove_var("CLAWDBOT_GATEWAY_PORT");
        assert_eq!(resolve_gateway_port(Some(9999)), 9999);
    }

    #[test]
    fn resolve_gateway_port_falls_back_to_default() {
        std::env::remove_var("DENEB_GATEWAY_PORT");
        std::env::remove_var("CLAWDBOT_GATEWAY_PORT");
        assert_eq!(resolve_gateway_port(None), DEFAULT_GATEWAY_PORT);
    }
}
