//! Pure path-policy functions for the Deneb CLI.
//!
//! Functions in this module accept all inputs as parameters — no env reads,
//! no filesystem access — so they can be tested deterministically without
//! process-level side effects.  Callers (see `config/mod.rs`) are responsible
//! for reading env vars and probing the filesystem before invoking the policy.

use std::path::{Path, PathBuf};

const NEW_STATE_DIRNAME: &str = ".deneb";
const CONFIG_FILENAME: &str = "deneb.json";

// Legacy name constants are canonical in `legacy_compat`; re-used here for
// `default_state_dir` / `config_candidates` helpers.
use crate::config::legacy_compat::{LEGACY_CONFIG_FILENAMES, LEGACY_STATE_DIRNAMES};

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

/// Resolve state directory using path-policy inputs only.
///
/// Precedence (first match wins):
/// 1. Explicit `state_dir_override`
/// 2. Default dir (when `fast_mode` is enabled — skips legacy probes)
/// 3. Default dir (when it already exists on disk)
/// 4. `first_existing_legacy_dir` (first legacy dir found by caller)
/// 5. Default dir (fallback — need not exist)
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

/// Return true when `state_dir` is itself a legacy-branded directory.
///
/// Legacy config filenames (clawdbot.json, etc.) inside the state dir are
/// only relevant when the dir was created by an old install.  On a standard
/// `.deneb` install those files never exist, so probing them wastes syscalls.
pub fn is_legacy_state_dir(state_dir: &Path) -> bool {
    state_dir
        .file_name()
        .and_then(|n| n.to_str())
        .is_some_and(|name| LEGACY_STATE_DIRNAMES.contains(&name))
}

/// Build the ordered list of config file candidates for a given state dir.
///
/// Legacy config filenames are only included when `state_dir` is itself a
/// legacy-branded path; on a standard `.deneb` install only `deneb.json` is
/// probed, saving three unnecessary `path.exists()` calls per resolution.
pub fn config_candidates(state_dir: &Path) -> Vec<PathBuf> {
    let mut candidates = Vec::with_capacity(4);
    candidates.push(state_dir.join(CONFIG_FILENAME));
    if is_legacy_state_dir(state_dir) {
        for name in LEGACY_CONFIG_FILENAMES {
            candidates.push(state_dir.join(name));
        }
    }
    candidates
}

/// Resolve config file path from path-policy inputs only.
///
/// Precedence (first match wins):
/// 1. Explicit `config_path_override`
/// 2. `first_existing_candidate` (first existing file found by caller)
/// 3. `{state_dir}/deneb.json` (default fallback — need not exist)
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

/// Resolve gateway port from policy inputs only.
///
/// Precedence (first match wins):
/// 1. `env_port` (already resolved by caller from env vars)
/// 2. `config_port`
/// 3. `DEFAULT_GATEWAY_PORT`
pub fn resolve_gateway_port_policy(config_port: Option<u16>, env_port: Option<u16>) -> u16 {
    if let Some(port) = env_port.filter(|p| *p > 0) {
        return port;
    }

    if let Some(port) = config_port.filter(|p| *p > 0) {
        return port;
    }

    DEFAULT_GATEWAY_PORT
}
