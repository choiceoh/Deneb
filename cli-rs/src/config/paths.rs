use std::path::{Path, PathBuf};

const NEW_STATE_DIRNAME: &str = ".deneb";
const CONFIG_FILENAME: &str = "deneb.json";
const LEGACY_STATE_DIRNAMES: &[&str] = &[".clawdbot", ".moldbot", ".moltbot"];
const LEGACY_CONFIG_FILENAMES: &[&str] = &["clawdbot.json", "moldbot.json", "moltbot.json"];

pub const DEFAULT_GATEWAY_PORT: u16 = 18789;

/// Canonical default state directory path under a given home directory.
pub fn default_state_dir(home_dir: &Path) -> PathBuf {
    home_dir.join(NEW_STATE_DIRNAME)
}

/// Ordered list of legacy state directories under a given home directory.
pub fn legacy_state_dirs(home_dir: &Path) -> Vec<PathBuf> {
    LEGACY_STATE_DIRNAMES
        .iter()
        .map(|legacy| home_dir.join(legacy))
        .collect()
}

/// Resolve state directory using path policy inputs only.
///
/// Precedence:
/// 1. explicit override
/// 2. default dir (when fast mode is enabled)
/// 3. existing default dir
/// 4. first existing legacy dir
/// 5. default dir
pub fn resolve_state_dir_policy(
    home_dir: &Path,
    state_dir_override: Option<&Path>,
    fast_mode: bool,
    default_dir_exists: bool,
    first_existing_legacy_dir: Option<&Path>,
) -> PathBuf {
    if let Some(override_dir) = state_dir_override {
        return override_dir.to_path_buf();
    }

    let default_dir = default_state_dir(home_dir);

    if fast_mode {
        return default_dir;
    }

    if default_dir_exists {
        return default_dir;
    }

    if let Some(legacy_dir) = first_existing_legacy_dir {
        return legacy_dir.to_path_buf();
    }

    default_dir
}

/// Build the list of config file candidates for a given state dir.
pub fn config_candidates(state_dir: &Path) -> Vec<PathBuf> {
    let mut candidates = Vec::with_capacity(4);
    candidates.push(state_dir.join(CONFIG_FILENAME));
    for name in LEGACY_CONFIG_FILENAMES {
        candidates.push(state_dir.join(name));
    }
    candidates
}

/// Resolve config file path from path-policy inputs only.
pub fn resolve_config_path_policy(
    state_dir: &Path,
    config_path_override: Option<&Path>,
    first_existing_candidate: Option<&Path>,
) -> PathBuf {
    if let Some(path) = config_path_override {
        return path.to_path_buf();
    }

    if let Some(existing_candidate) = first_existing_candidate {
        return existing_candidate.to_path_buf();
    }

    state_dir.join(CONFIG_FILENAME)
}

/// Resolve gateway port from env and config inputs.
pub fn resolve_gateway_port_policy(config_port: Option<u16>, env_port: Option<u16>) -> u16 {
    if let Some(port) = env_port.filter(|port| *port > 0) {
        return port;
    }

    if let Some(port) = config_port.filter(|port| *port > 0) {
        return port;
    }

    DEFAULT_GATEWAY_PORT
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_gateway_port_value() {
        assert_eq!(DEFAULT_GATEWAY_PORT, 18789);
    }

    #[test]
    fn resolve_gateway_port_prefers_env_override() {
        assert_eq!(resolve_gateway_port_policy(Some(9999), Some(7777)), 7777);
    }

    #[test]
    fn resolve_gateway_port_uses_config() {
        assert_eq!(resolve_gateway_port_policy(Some(9999), None), 9999);
    }

    #[test]
    fn resolve_gateway_port_falls_back_to_default() {
        assert_eq!(
            resolve_gateway_port_policy(None, None),
            DEFAULT_GATEWAY_PORT
        );
    }

    #[test]
    fn resolve_state_dir_prefers_override() {
        let home = Path::new("/home/test");
        let override_dir = Path::new("/tmp/custom-state");

        assert_eq!(
            resolve_state_dir_policy(home, Some(override_dir), false, false, None),
            override_dir
        );
    }

    #[test]
    fn resolve_state_dir_prefers_legacy_when_default_missing() {
        let home = Path::new("/home/test");
        let legacy = Path::new("/home/test/.clawdbot");

        assert_eq!(
            resolve_state_dir_policy(home, None, false, false, Some(legacy)),
            legacy
        );
    }

    #[test]
    fn resolve_config_path_prefers_override_then_existing_candidate_then_default() {
        let state_dir = Path::new("/tmp/state");
        let override_path = Path::new("/tmp/custom.json");
        let existing_candidate = Path::new("/tmp/state/clawdbot.json");

        assert_eq!(
            resolve_config_path_policy(state_dir, Some(override_path), Some(existing_candidate)),
            override_path
        );
        assert_eq!(
            resolve_config_path_policy(state_dir, None, Some(existing_candidate)),
            existing_candidate
        );
        assert_eq!(
            resolve_config_path_policy(state_dir, None, None),
            state_dir.join("deneb.json")
        );
    }
}
