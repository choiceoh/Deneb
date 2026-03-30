//! Path and port resolution policies.
//!
//! 1:1 port of `gateway-go/internal/config/paths.go`.
//!
//! Each policy type encodes one resolution concern together with its
//! environment-variable names and hardcoded defaults.  The public free
//! functions (`resolve_state_dir`, `resolve_config_path`, `resolve_gateway_port`)
//! are thin wrappers that call the default policy.

use std::env;
use std::path::{Path, PathBuf};

use crate::config::legacy_compat::{
    find_legacy_state_dir, LEGACY_CONFIG_FILENAMES, LEGACY_STATE_DIRNAMES,
};

/// Default gateway server port.
pub const DEFAULT_GATEWAY_PORT: i32 = 18789;

/// Canonical state directory name.
pub const DEFAULT_STATE_DIRNAME: &str = ".deneb";

/// Canonical config file name.
pub const DEFAULT_CONFIG_FILENAME: &str = "deneb.json";

// ── StateDirPolicy ───────────────────────────────────────────────────────────

/// Encodes the precedence rules for resolving the state directory.
///
/// Precedence (first match wins):
///  1. `env_vars[0]` (`DENEB_STATE_DIR`)
///  2. `env_vars[1]` (`CLAWDBOT_STATE_DIR`, legacy)
///  3. `~/.deneb` if it already exists on disk
///  4. First existing legacy directory (.clawdbot, .moldbot, .moltbot)
///  5. `~/.deneb` (default fallback -- directory need not exist)
pub struct StateDirPolicy {
    pub env_vars: &'static [&'static str],
    pub dirname: &'static str,
}

impl StateDirPolicy {
    /// Returns the standard production policy.
    pub fn default_policy() -> Self {
        Self {
            env_vars: &["DENEB_STATE_DIR", "CLAWDBOT_STATE_DIR"],
            dirname: DEFAULT_STATE_DIRNAME,
        }
    }

    /// Resolve the state directory against an explicit home path.
    pub fn resolve_from(&self, home: &Path) -> PathBuf {
        // 1-2. Env override.
        for key in self.env_vars {
            if let Ok(val) = env::var(key) {
                let val = val.trim().to_string();
                if !val.is_empty() {
                    return expand_home_path(&val);
                }
            }
        }

        let new_dir = home.join(self.dirname);

        // 3. Canonical dir exists.
        if dir_exists(&new_dir) {
            return new_dir;
        }

        // 4. First existing legacy dir.
        if let Some(legacy) = find_legacy_state_dir(home) {
            return legacy;
        }

        // 5. Default.
        new_dir
    }

    /// Resolve using the current process home directory.
    pub fn resolve(&self) -> PathBuf {
        self.resolve_from(&resolve_home_dir())
    }
}

// ── ConfigPathPolicy ─────────────────────────────────────────────────────────

/// Encodes the precedence rules for resolving the config file path.
///
/// Precedence (first match wins):
///  1. `env_vars[0]` (`DENEB_CONFIG_PATH`)
///  2. `env_vars[1]` (`CLAWDBOT_CONFIG_PATH`, legacy)
///  3. First existing config candidate in `state_dir` (canonical, then legacy names)
///  4. `{state_dir}/deneb.json` (default fallback -- file need not exist)
pub struct ConfigPathPolicy {
    pub env_vars: &'static [&'static str],
    pub filename: &'static str,
}

impl ConfigPathPolicy {
    /// Returns the standard production policy.
    pub fn default_policy() -> Self {
        Self {
            env_vars: &["DENEB_CONFIG_PATH", "CLAWDBOT_CONFIG_PATH"],
            filename: DEFAULT_CONFIG_FILENAME,
        }
    }

    /// Resolve the config path given an explicit `state_dir`.
    pub fn resolve_from(&self, state_dir: &Path) -> PathBuf {
        // 1-2. Env override.
        for key in self.env_vars {
            if let Ok(val) = env::var(key) {
                let val = val.trim().to_string();
                if !val.is_empty() {
                    return expand_home_path(&val);
                }
            }
        }

        // 3. First existing candidate.
        for candidate in self.candidates(state_dir) {
            if file_exists(&candidate) {
                return candidate;
            }
        }

        // 4. Default.
        state_dir.join(self.filename)
    }

    /// Resolve using the auto-resolved state directory.
    pub fn resolve(&self) -> PathBuf {
        let state_dir = StateDirPolicy::default_policy().resolve();

        // Also check legacy config files stored directly inside legacy home dirs
        // when the resolved state dir is the canonical default.
        let home = resolve_home_dir();
        let default_state_dir = home.join(DEFAULT_STATE_DIRNAME);
        if clean_path(&state_dir) == clean_path(&default_state_dir) {
            for legacy_dir in LEGACY_STATE_DIRNAMES {
                let dir = home.join(legacy_dir);
                let mut all_names = vec![DEFAULT_CONFIG_FILENAME];
                all_names.extend(LEGACY_CONFIG_FILENAMES);
                for name in all_names {
                    let candidate = dir.join(name);
                    if file_exists(&candidate) {
                        return candidate;
                    }
                }
            }
        }

        self.resolve_from(&state_dir)
    }

    /// Returns the ordered list of config file paths to probe in `state_dir`.
    pub fn candidates(&self, state_dir: &Path) -> Vec<PathBuf> {
        let mut out = Vec::with_capacity(1 + LEGACY_CONFIG_FILENAMES.len());
        out.push(state_dir.join(self.filename));
        for name in LEGACY_CONFIG_FILENAMES {
            out.push(state_dir.join(name));
        }
        out
    }
}

// ── GatewayPortPolicy ────────────────────────────────────────────────────────

/// Encodes the precedence rules for resolving the gateway port.
///
/// Precedence (first match wins):
///  1. `env_vars[0]` (`DENEB_GATEWAY_PORT`)
///  2. `env_vars[1]` (`CLAWDBOT_GATEWAY_PORT`, legacy)
///  3. Caller-supplied config port
///  4. `default_port` (18789)
pub struct GatewayPortPolicy {
    pub env_vars: &'static [&'static str],
    pub default_port: i32,
}

impl GatewayPortPolicy {
    /// Returns the standard production policy.
    pub fn default_policy() -> Self {
        Self {
            env_vars: &["DENEB_GATEWAY_PORT", "CLAWDBOT_GATEWAY_PORT"],
            default_port: DEFAULT_GATEWAY_PORT,
        }
    }

    /// Resolve from env vars and an optional explicit config port.
    pub fn resolve_from(&self, config_port: Option<i32>) -> i32 {
        // 1-2. Env override.
        for key in self.env_vars {
            if let Ok(raw) = env::var(key) {
                let raw = raw.trim().to_string();
                if let Ok(port) = raw.parse::<i32>() {
                    if port > 0 {
                        return port;
                    }
                }
            }
        }

        // 3. Config value.
        if let Some(port) = config_port {
            if port > 0 {
                return port;
            }
        }

        // 4. Default.
        self.default_port
    }

    /// Extract the port from a `DenebConfig` and delegate to `resolve_from`.
    pub fn resolve(&self, cfg: &super::types::DenebConfig) -> i32 {
        let config_port = cfg
            .gateway
            .as_ref()
            .and_then(|g| g.port)
            .filter(|&p| p > 0);
        self.resolve_from(config_port)
    }
}

// ── Public free functions ────────────────────────────────────────────────────

/// Determine the Deneb state directory using the default policy.
pub fn resolve_state_dir() -> PathBuf {
    StateDirPolicy::default_policy().resolve()
}

/// Determine the config file path using the default policy.
pub fn resolve_config_path() -> PathBuf {
    ConfigPathPolicy::default_policy().resolve()
}

/// Determine the gateway port using the default policy.
pub fn resolve_gateway_port(cfg: &super::types::DenebConfig) -> i32 {
    GatewayPortPolicy::default_policy().resolve(cfg)
}

/// Determine the workspace directory for the default agent.
///
/// Priority:
///  1. `agents.list[]` entry with `default=true` -> workspace
///  2. `agents.defaults.workspace`
///  3. `~/.deneb/workspace` (built-in default)
pub fn resolve_agent_workspace_dir(cfg: &super::types::DenebConfig) -> PathBuf {
    if let Some(agents) = &cfg.agents {
        // Per-agent workspace (default=true) takes highest priority.
        if let Some(list) = &agents.list {
            for agent in list {
                if agent.default == Some(true) {
                    if let Some(ws) = &agent.workspace {
                        let ws = ws.trim();
                        if !ws.is_empty() {
                            return expand_home_path(ws);
                        }
                    }
                }
            }
        }
        // agents.defaults.workspace.
        if let Some(defaults) = &agents.defaults {
            if let Some(ws) = &defaults.workspace {
                let ws = ws.trim();
                if !ws.is_empty() {
                    return expand_home_path(ws);
                }
            }
        }
    }

    // Built-in default: ~/.deneb/workspace.
    let home = resolve_home_dir();
    let profile = env::var("DENEB_PROFILE").unwrap_or_default();
    let profile = profile.trim();
    if !profile.is_empty() && profile.to_lowercase() != "default" {
        return home
            .join(DEFAULT_STATE_DIRNAME)
            .join(format!("workspace-{profile}"));
    }
    home.join(DEFAULT_STATE_DIRNAME).join("workspace")
}

// ── Helpers ──────────────────────────────────────────────────────────────────

pub fn resolve_home_dir() -> PathBuf {
    if let Ok(home) = env::var("HOME") {
        if !home.is_empty() {
            return PathBuf::from(home);
        }
    }
    dirs::home_dir().unwrap_or_else(|| PathBuf::from("/tmp"))
}

pub fn expand_home_path(p: &str) -> PathBuf {
    if let Some(rest) = p.strip_prefix("~/") {
        resolve_home_dir().join(rest)
    } else {
        PathBuf::from(p)
    }
}

pub fn dir_exists(path: &Path) -> bool {
    path.is_dir()
}

pub fn file_exists(path: &Path) -> bool {
    path.is_file()
}

fn clean_path(p: &Path) -> PathBuf {
    // Simple canonicalization without resolving symlinks (mirrors filepath.Clean).
    let s = p.to_string_lossy();
    PathBuf::from(s.trim_end_matches('/'))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    // ── StateDirPolicy ───────────────────────────────────────────────────────

    #[test]
    fn state_dir_default_when_nothing_exists() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let policy = StateDirPolicy {
            env_vars: &[],
            dirname: ".deneb",
        };
        let result = policy.resolve_from(tmp.path());
        assert_eq!(result, tmp.path().join(".deneb"));
    }

    #[test]
    fn state_dir_canonical_exists() {
        let tmp = tempfile::tempdir().expect("tempdir");
        fs::create_dir_all(tmp.path().join(".deneb")).expect("mkdir");
        let policy = StateDirPolicy {
            env_vars: &[],
            dirname: ".deneb",
        };
        let result = policy.resolve_from(tmp.path());
        assert_eq!(result, tmp.path().join(".deneb"));
    }

    #[test]
    fn state_dir_legacy_fallback() {
        let tmp = tempfile::tempdir().expect("tempdir");
        fs::create_dir_all(tmp.path().join(".clawdbot")).expect("mkdir");
        let policy = StateDirPolicy {
            env_vars: &[],
            dirname: ".deneb",
        };
        let result = policy.resolve_from(tmp.path());
        assert_eq!(result, tmp.path().join(".clawdbot"));
    }

    #[test]
    fn state_dir_canonical_wins_over_legacy() {
        let tmp = tempfile::tempdir().expect("tempdir");
        fs::create_dir_all(tmp.path().join(".deneb")).expect("mkdir");
        fs::create_dir_all(tmp.path().join(".clawdbot")).expect("mkdir");
        let policy = StateDirPolicy {
            env_vars: &[],
            dirname: ".deneb",
        };
        let result = policy.resolve_from(tmp.path());
        assert_eq!(result, tmp.path().join(".deneb"));
    }

    // ── ConfigPathPolicy ─────────────────────────────────────────────────────

    #[test]
    fn config_path_default_fallback() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let policy = ConfigPathPolicy {
            env_vars: &[],
            filename: "deneb.json",
        };
        let result = policy.resolve_from(tmp.path());
        assert_eq!(result, tmp.path().join("deneb.json"));
    }

    #[test]
    fn config_path_first_existing_candidate() {
        let tmp = tempfile::tempdir().expect("tempdir");
        fs::write(tmp.path().join("clawdbot.json"), b"{}").expect("write");
        let policy = ConfigPathPolicy {
            env_vars: &[],
            filename: "deneb.json",
        };
        let result = policy.resolve_from(tmp.path());
        // deneb.json is checked first; since it does not exist, clawdbot.json wins.
        assert_eq!(result, tmp.path().join("clawdbot.json"));
    }

    #[test]
    fn config_path_canonical_wins_over_legacy() {
        let tmp = tempfile::tempdir().expect("tempdir");
        fs::write(tmp.path().join("deneb.json"), b"{}").expect("write");
        fs::write(tmp.path().join("clawdbot.json"), b"{}").expect("write");
        let policy = ConfigPathPolicy {
            env_vars: &[],
            filename: "deneb.json",
        };
        let result = policy.resolve_from(tmp.path());
        assert_eq!(result, tmp.path().join("deneb.json"));
    }

    // ── GatewayPortPolicy ────────────────────────────────────────────────────

    #[test]
    fn port_default() {
        let policy = GatewayPortPolicy {
            env_vars: &[],
            default_port: DEFAULT_GATEWAY_PORT,
        };
        assert_eq!(policy.resolve_from(None), DEFAULT_GATEWAY_PORT);
    }

    #[test]
    fn port_from_config() {
        let policy = GatewayPortPolicy {
            env_vars: &[],
            default_port: DEFAULT_GATEWAY_PORT,
        };
        assert_eq!(policy.resolve_from(Some(9999)), 9999);
    }

    #[test]
    fn port_zero_config_falls_back_to_default() {
        let policy = GatewayPortPolicy {
            env_vars: &[],
            default_port: DEFAULT_GATEWAY_PORT,
        };
        assert_eq!(policy.resolve_from(Some(0)), DEFAULT_GATEWAY_PORT);
    }

    // ── resolve_agent_workspace_dir ──────────────────────────────────────────

    #[test]
    fn workspace_default_with_empty_config() {
        let cfg = super::super::types::DenebConfig::default();
        let ws = resolve_agent_workspace_dir(&cfg);
        assert!(ws.ends_with("workspace") || ws.to_string_lossy().contains("workspace"));
    }

    #[test]
    fn workspace_from_agent_list() {
        let json = r#"{
            "agents": {
                "list": [
                    {"id": "other", "workspace": "/tmp/other"},
                    {"id": "main", "default": true, "workspace": "/tmp/main-ws"}
                ]
            }
        }"#;
        let cfg: super::super::types::DenebConfig = serde_json::from_str(json).expect("parse");
        let ws = resolve_agent_workspace_dir(&cfg);
        assert_eq!(ws, PathBuf::from("/tmp/main-ws"));
    }

    #[test]
    fn workspace_from_defaults() {
        let json = r#"{"agents": {"defaults": {"workspace": "/tmp/default-ws"}}}"#;
        let cfg: super::super::types::DenebConfig = serde_json::from_str(json).expect("parse");
        let ws = resolve_agent_workspace_dir(&cfg);
        assert_eq!(ws, PathBuf::from("/tmp/default-ws"));
    }

    // ── Helpers ──────────────────────────────────────────────────────────────

    #[test]
    fn expand_home_tilde() {
        let result = expand_home_path("~/foo/bar");
        assert!(result.ends_with("foo/bar"));
        assert!(!result.to_string_lossy().starts_with('~'));
    }

    #[test]
    fn expand_home_absolute() {
        let result = expand_home_path("/absolute/path");
        assert_eq!(result, PathBuf::from("/absolute/path"));
    }
}
