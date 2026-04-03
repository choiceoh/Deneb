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
        .map(|name| LEGACY_STATE_DIRNAMES.contains(&name))
        .unwrap_or(false)
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

#[cfg(test)]
mod tests {
    use super::*;

    // ── resolve_state_dir_policy precedence table ─────────────────────────────

    struct StateDirCase {
        label: &'static str,
        state_dir_override: Option<&'static str>,
        fast_mode: bool,
        default_dir_exists: bool,
        first_existing_legacy: Option<&'static str>,
        expected_basename: &'static str,
    }

    #[test]
    fn state_dir_precedence_table() {
        let home = Path::new("/home/test");
        let cases = [
            StateDirCase {
                label: "explicit override wins over everything",
                state_dir_override: Some("/tmp/custom-state"),
                fast_mode: true,
                default_dir_exists: true,
                first_existing_legacy: Some("/home/test/.clawdbot"),
                expected_basename: "custom-state",
            },
            StateDirCase {
                label: "fast mode returns default without checking legacy",
                state_dir_override: None,
                fast_mode: true,
                default_dir_exists: false,
                first_existing_legacy: Some("/home/test/.clawdbot"),
                expected_basename: ".deneb",
            },
            StateDirCase {
                label: "existing default dir wins over legacy",
                state_dir_override: None,
                fast_mode: false,
                default_dir_exists: true,
                first_existing_legacy: Some("/home/test/.clawdbot"),
                expected_basename: ".deneb",
            },
            StateDirCase {
                label: "legacy dir used when default absent",
                state_dir_override: None,
                fast_mode: false,
                default_dir_exists: false,
                first_existing_legacy: Some("/home/test/.clawdbot"),
                expected_basename: ".clawdbot",
            },
            StateDirCase {
                label: "falls back to default when nothing exists",
                state_dir_override: None,
                fast_mode: false,
                default_dir_exists: false,
                first_existing_legacy: None,
                expected_basename: ".deneb",
            },
        ];

        for case in &cases {
            let result = resolve_state_dir_policy(
                home,
                case.state_dir_override.map(Path::new),
                case.fast_mode,
                case.default_dir_exists,
                case.first_existing_legacy.map(Path::new),
            );
            let basename = result
                .file_name()
                .and_then(|n| n.to_str())
                .unwrap_or_default();
            assert_eq!(
                basename, case.expected_basename,
                "case '{}': expected {:?}, got {:?}",
                case.label, case.expected_basename, basename
            );
        }
    }

    // ── resolve_config_path_policy precedence table ───────────────────────────

    struct ConfigPathCase {
        label: &'static str,
        config_path_override: Option<&'static str>,
        first_existing_candidate: Option<&'static str>,
        expected_basename: &'static str,
    }

    #[test]
    fn config_path_precedence_table() {
        let state_dir = Path::new("/tmp/state");
        let cases = [
            ConfigPathCase {
                label: "explicit override wins",
                config_path_override: Some("/tmp/custom.json"),
                first_existing_candidate: Some("/tmp/state/clawdbot.json"),
                expected_basename: "custom.json",
            },
            ConfigPathCase {
                label: "first existing candidate used when no override",
                config_path_override: None,
                first_existing_candidate: Some("/tmp/state/clawdbot.json"),
                expected_basename: "clawdbot.json",
            },
            ConfigPathCase {
                label: "defaults to deneb.json when no override and no existing candidate",
                config_path_override: None,
                first_existing_candidate: None,
                expected_basename: "deneb.json",
            },
        ];

        for case in &cases {
            let result = resolve_config_path_policy(
                state_dir,
                case.config_path_override.map(Path::new),
                case.first_existing_candidate.map(Path::new),
            );
            let basename = result
                .file_name()
                .and_then(|n| n.to_str())
                .unwrap_or_default();
            assert_eq!(
                basename, case.expected_basename,
                "case '{}': expected {:?}, got {:?}",
                case.label, case.expected_basename, basename
            );
        }
    }

    // ── resolve_gateway_port_policy precedence table ──────────────────────────

    struct PortCase {
        label: &'static str,
        env_port: Option<u16>,
        config_port: Option<u16>,
        expected: u16,
    }

    #[test]
    fn gateway_port_precedence_table() {
        let cases = [
            PortCase {
                label: "env port wins over config and default",
                env_port: Some(9001),
                config_port: Some(9003),
                expected: 9001,
            },
            PortCase {
                label: "config port used when no env override",
                env_port: None,
                config_port: Some(9003),
                expected: 9003,
            },
            PortCase {
                label: "default used when nothing is set",
                env_port: None,
                config_port: None,
                expected: DEFAULT_GATEWAY_PORT,
            },
            PortCase {
                label: "zero env port falls through to config",
                env_port: Some(0),
                config_port: Some(9003),
                expected: 9003,
            },
            PortCase {
                label: "zero config port falls back to default",
                env_port: None,
                config_port: Some(0),
                expected: DEFAULT_GATEWAY_PORT,
            },
        ];

        for case in &cases {
            let got = resolve_gateway_port_policy(case.config_port, case.env_port);
            assert_eq!(
                got, case.expected,
                "case '{}': expected {}, got {}",
                case.label, case.expected, got
            );
        }
    }

    // ── Focused unit tests (complement the table above) ───────────────────────

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
