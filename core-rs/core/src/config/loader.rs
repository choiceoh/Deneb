//! Configuration loading, validation, and defaults.
//!
//! 1:1 port of `gateway-go/internal/config/loader.go`.

use sha2::{Digest, Sha256};
use std::fs;
use std::path::Path;

use crate::config::paths::{resolve_config_path, DEFAULT_GATEWAY_PORT};
use crate::config::types::*;

/// Holds the result of loading and validating a config file.
#[derive(Debug, Clone)]
pub struct ConfigSnapshot {
    pub path: String,
    pub exists: bool,
    pub raw: String,
    pub config: DenebConfig,
    pub hash: String,
    pub valid: bool,
    pub issues: Vec<ConfigIssue>,
    pub warnings: Vec<String>,
}

/// Represents a config validation error.
#[derive(Debug, Clone)]
pub struct ConfigIssue {
    pub path: String,
    pub message: String,
}

impl std::fmt::Display for ConfigIssue {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if self.path.is_empty() {
            write!(f, "{}", self.message)
        } else {
            write!(f, "{}: {}", self.path, self.message)
        }
    }
}

/// Read and parse the Deneb config file, returning a snapshot.
pub fn load_config(config_path: &Path) -> Result<ConfigSnapshot, String> {
    let path_str = config_path.to_string_lossy().to_string();

    let raw = match fs::read(config_path) {
        Ok(data) => data,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
            let mut cfg = DenebConfig::default();
            apply_defaults(&mut cfg);
            return Ok(ConfigSnapshot {
                path: path_str,
                exists: false,
                raw: String::new(),
                config: cfg,
                hash: hash_raw(b""),
                valid: true,
                issues: vec![],
                warnings: vec![],
            });
        }
        Err(e) => return Err(format!("reading config {path_str}: {e}")),
    };

    let raw_str = String::from_utf8_lossy(&raw).to_string();
    let hash = hash_raw(&raw);

    let mut cfg: DenebConfig = match serde_json::from_slice(&raw) {
        Ok(c) => c,
        Err(e) => {
            return Ok(ConfigSnapshot {
                path: path_str,
                exists: true,
                raw: raw_str,
                config: DenebConfig::default(),
                hash,
                valid: false,
                issues: vec![ConfigIssue {
                    path: String::new(),
                    message: format!("JSON parse failed: {e}"),
                }],
                warnings: vec![],
            });
        }
    };

    let (issues, warnings) = validate_config(&cfg);
    let valid = issues.is_empty();

    apply_defaults(&mut cfg);

    Ok(ConfigSnapshot {
        path: path_str,
        exists: true,
        raw: raw_str,
        config: cfg,
        hash,
        valid,
        issues,
        warnings,
    })
}

/// Load config from the auto-resolved path.
pub fn load_config_from_default_path() -> Result<ConfigSnapshot, String> {
    load_config(&resolve_config_path())
}

/// Perform basic structural validation on the config.
pub fn validate_config(cfg: &DenebConfig) -> (Vec<ConfigIssue>, Vec<String>) {
    let mut issues = Vec::new();
    let warnings = Vec::new();

    if let Some(gw) = &cfg.gateway {
        // Validate bind mode.
        if let Some(bind) = &gw.bind {
            match bind.as_str() {
                "auto" | "lan" | "loopback" | "custom" | "tailnet" => {}
                _ => {
                    issues.push(ConfigIssue {
                        path: "gateway.bind".to_string(),
                        message: format!(
                            "invalid bind mode {bind:?} (expected auto|lan|loopback|custom|tailnet)"
                        ),
                    });
                }
            }
        }

        // Validate custom bind host.
        if gw.bind.as_deref() == Some("custom") {
            let host = gw
                .custom_bind_host
                .as_deref()
                .unwrap_or("")
                .trim();
            if host.is_empty() {
                issues.push(ConfigIssue {
                    path: "gateway.customBindHost".to_string(),
                    message: "gateway.bind=custom requires gateway.customBindHost".to_string(),
                });
            }
        }

        // Validate port range.
        if let Some(port) = gw.port {
            if !(1..=65535).contains(&port) {
                issues.push(ConfigIssue {
                    path: "gateway.port".to_string(),
                    message: format!("port {port} out of range (1-65535)"),
                });
            }
        }

        // Validate auth mode.
        if let Some(auth) = &gw.auth {
            if let Some(mode) = &auth.mode {
                match mode.as_str() {
                    "none" | "token" | "password" | "trusted-proxy" => {}
                    _ => {
                        issues.push(ConfigIssue {
                            path: "gateway.auth.mode".to_string(),
                            message: format!("invalid auth mode {mode:?}"),
                        });
                    }
                }
            }
        }

        // Validate tailscale mode.
        if let Some(ts) = &gw.tailscale {
            if let Some(mode) = &ts.mode {
                match mode.as_str() {
                    "off" | "serve" | "funnel" => {}
                    _ => {
                        issues.push(ConfigIssue {
                            path: "gateway.tailscale.mode".to_string(),
                            message: format!("invalid tailscale mode {mode:?}"),
                        });
                    }
                }
            }
        }

        // Validate reload mode.
        if let Some(reload) = &gw.reload {
            if let Some(mode) = &reload.mode {
                match mode.as_str() {
                    "off" | "restart" | "hot" | "hybrid" => {}
                    _ => {
                        issues.push(ConfigIssue {
                            path: "gateway.reload.mode".to_string(),
                            message: format!("invalid reload mode {mode:?}"),
                        });
                    }
                }
            }
        }
    }

    (issues, warnings)
}

/// Fill in default values for missing config fields.
pub fn apply_defaults(cfg: &mut DenebConfig) {
    // Gateway defaults.
    let gw = cfg.gateway.get_or_insert_with(GatewayConfig::default);
    if gw.port.is_none() {
        gw.port = Some(DEFAULT_GATEWAY_PORT);
    }
    if gw.bind.is_none() {
        gw.bind = Some("loopback".to_string());
    }

    let auth = gw.auth.get_or_insert_with(GatewayAuthConfig::default);
    if auth.mode.is_none() {
        auth.mode = Some("token".to_string());
    }

    let control_ui = gw
        .control_ui
        .get_or_insert_with(GatewayControlUIConfig::default);
    if control_ui.enabled.is_none() {
        control_ui.enabled = Some(true);
    }

    let ts = gw
        .tailscale
        .get_or_insert_with(GatewayTailscaleConfig::default);
    if ts.mode.is_none() {
        ts.mode = Some("off".to_string());
    }

    if gw.channel_health_check_minutes.is_none() {
        gw.channel_health_check_minutes = Some(5);
    }
    if gw.channel_stale_event_threshold_minutes.is_none() {
        gw.channel_stale_event_threshold_minutes = Some(30);
    }
    if gw.channel_max_restarts_per_hour.is_none() {
        gw.channel_max_restarts_per_hour = Some(10);
    }

    let reload = gw
        .reload
        .get_or_insert_with(GatewayReloadConfig::default);
    if reload.mode.is_none() {
        reload.mode = Some("hybrid".to_string());
    }
    if reload.debounce_ms.is_none() {
        reload.debounce_ms = Some(300);
    }
    if reload.deferral_timeout_ms.is_none() {
        reload.deferral_timeout_ms = Some(300_000);
    }

    // Auth rate limit defaults.
    let rl = auth
        .rate_limit
        .get_or_insert_with(GatewayAuthRateLimitConfig::default);
    if rl.max_attempts.is_none() {
        rl.max_attempts = Some(10);
    }
    if rl.window_ms.is_none() {
        rl.window_ms = Some(60_000);
    }
    if rl.lockout_ms.is_none() {
        rl.lockout_ms = Some(300_000);
    }
    if rl.exempt_loopback.is_none() {
        rl.exempt_loopback = Some(true);
    }

    // Session defaults.
    let session = cfg.session.get_or_insert_with(SessionConfig::default);
    if session.main_key.is_none() {
        session.main_key = Some("main".to_string());
    }

    // Agent defaults.
    let agents = cfg.agents.get_or_insert_with(AgentsConfig::default);
    if agents.max_concurrent.is_none() {
        agents.max_concurrent = Some(8);
    }
    if agents.subagent_max_concurrent.is_none() {
        agents.subagent_max_concurrent = Some(2);
    }

    // Logging defaults.
    let logging = cfg.logging.get_or_insert_with(LoggingConfig::default);
    if logging.redact_sensitive.is_none() {
        logging.redact_sensitive = Some("tools".to_string());
    }
}

/// Compute a SHA-256 hex hash of raw bytes.
pub fn hash_raw(data: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(data);
    hex::encode(hasher.finalize())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn load_missing_config_returns_defaults() {
        let snap = load_config(Path::new("/tmp/nonexistent-deneb-test-config.json"))
            .expect("load");
        assert!(!snap.exists);
        assert!(snap.valid);
        // Defaults should be applied.
        let gw = snap.config.gateway.as_ref().expect("gateway");
        assert_eq!(gw.port, Some(DEFAULT_GATEWAY_PORT));
        assert_eq!(gw.bind.as_deref(), Some("loopback"));
    }

    #[test]
    fn load_valid_config() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("deneb.json");
        fs::write(
            &path,
            r#"{"gateway": {"port": 9999, "bind": "lan"}}"#,
        )
        .expect("write");

        let snap = load_config(&path).expect("load");
        assert!(snap.exists);
        assert!(snap.valid);
        let gw = snap.config.gateway.as_ref().expect("gateway");
        assert_eq!(gw.port, Some(9999));
        assert_eq!(gw.bind.as_deref(), Some("lan"));
    }

    #[test]
    fn load_invalid_json() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("deneb.json");
        fs::write(&path, "not json{{{").expect("write");

        let snap = load_config(&path).expect("load");
        assert!(snap.exists);
        assert!(!snap.valid);
        assert!(!snap.issues.is_empty());
        assert!(snap.issues[0].message.contains("JSON parse failed"));
    }

    #[test]
    fn load_invalid_bind_mode() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("deneb.json");
        fs::write(&path, r#"{"gateway": {"bind": "bogus"}}"#).expect("write");

        let snap = load_config(&path).expect("load");
        assert!(snap.exists);
        assert!(!snap.valid);
        assert!(snap.issues.iter().any(|i| i.path == "gateway.bind"));
    }

    #[test]
    fn validate_port_range() {
        let json = r#"{"gateway": {"port": 99999}}"#;
        let cfg: DenebConfig = serde_json::from_str(json).expect("parse");
        let (issues, _) = validate_config(&cfg);
        assert!(issues.iter().any(|i| i.path == "gateway.port"));
    }

    #[test]
    fn validate_auth_mode() {
        let json = r#"{"gateway": {"auth": {"mode": "invalid"}}}"#;
        let cfg: DenebConfig = serde_json::from_str(json).expect("parse");
        let (issues, _) = validate_config(&cfg);
        assert!(issues.iter().any(|i| i.path == "gateway.auth.mode"));
    }

    #[test]
    fn validate_tailscale_mode() {
        let json = r#"{"gateway": {"tailscale": {"mode": "bogus"}}}"#;
        let cfg: DenebConfig = serde_json::from_str(json).expect("parse");
        let (issues, _) = validate_config(&cfg);
        assert!(issues.iter().any(|i| i.path == "gateway.tailscale.mode"));
    }

    #[test]
    fn validate_reload_mode() {
        let json = r#"{"gateway": {"reload": {"mode": "bogus"}}}"#;
        let cfg: DenebConfig = serde_json::from_str(json).expect("parse");
        let (issues, _) = validate_config(&cfg);
        assert!(issues.iter().any(|i| i.path == "gateway.reload.mode"));
    }

    #[test]
    fn apply_defaults_fills_missing() {
        let mut cfg = DenebConfig::default();
        apply_defaults(&mut cfg);

        let gw = cfg.gateway.as_ref().expect("gateway");
        assert_eq!(gw.port, Some(DEFAULT_GATEWAY_PORT));
        assert_eq!(gw.bind.as_deref(), Some("loopback"));
        assert_eq!(
            gw.auth.as_ref().and_then(|a| a.mode.as_deref()),
            Some("token")
        );
        assert_eq!(gw.control_ui.as_ref().and_then(|c| c.enabled), Some(true));
        assert_eq!(
            gw.tailscale.as_ref().and_then(|t| t.mode.as_deref()),
            Some("off")
        );
        assert_eq!(gw.channel_health_check_minutes, Some(5));
        assert_eq!(gw.channel_stale_event_threshold_minutes, Some(30));
        assert_eq!(gw.channel_max_restarts_per_hour, Some(10));

        let reload = gw.reload.as_ref().expect("reload");
        assert_eq!(reload.mode.as_deref(), Some("hybrid"));
        assert_eq!(reload.debounce_ms, Some(300));
        assert_eq!(reload.deferral_timeout_ms, Some(300_000));

        let rl = gw
            .auth
            .as_ref()
            .and_then(|a| a.rate_limit.as_ref())
            .expect("rate_limit");
        assert_eq!(rl.max_attempts, Some(10));
        assert_eq!(rl.window_ms, Some(60_000));
        assert_eq!(rl.lockout_ms, Some(300_000));
        assert_eq!(rl.exempt_loopback, Some(true));

        assert_eq!(
            cfg.session.as_ref().and_then(|s| s.main_key.as_deref()),
            Some("main")
        );
        assert_eq!(cfg.agents.as_ref().and_then(|a| a.max_concurrent), Some(8));
        assert_eq!(
            cfg.agents.as_ref().and_then(|a| a.subagent_max_concurrent),
            Some(2)
        );
        assert_eq!(
            cfg.logging
                .as_ref()
                .and_then(|l| l.redact_sensitive.as_deref()),
            Some("tools")
        );
    }

    #[test]
    fn hash_raw_deterministic() {
        let h1 = hash_raw(b"hello");
        let h2 = hash_raw(b"hello");
        assert_eq!(h1, h2);
        assert_ne!(h1, hash_raw(b"world"));
    }

    #[test]
    fn hash_raw_empty() {
        let h = hash_raw(b"");
        // SHA-256 of empty string.
        assert_eq!(
            h,
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        );
    }

    #[test]
    fn config_issue_display() {
        let issue = ConfigIssue {
            path: "gateway.port".to_string(),
            message: "out of range".to_string(),
        };
        assert_eq!(issue.to_string(), "gateway.port: out of range");

        let issue = ConfigIssue {
            path: String::new(),
            message: "general error".to_string(),
        };
        assert_eq!(issue.to_string(), "general error");
    }
}
