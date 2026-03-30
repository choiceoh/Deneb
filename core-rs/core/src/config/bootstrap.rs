//! Gateway config bootstrap sequence.
//!
//! 1:1 port of `gateway-go/internal/config/bootstrap.go`.

use rand::RngCore;
use std::env;
use std::fs;
use std::path::Path;

use crate::config::loader::{load_config, ConfigSnapshot};
use crate::config::paths::resolve_config_path;
use crate::config::types::*;

/// Number of random bytes for auto-generated tokens (24 bytes = 48 hex chars).
const GENERATED_TOKEN_BYTES: usize = 24;

/// Maximum media retention window (7 days).
const MAX_MEDIA_TTL_HOURS: i32 = 24 * 7;

/// Result of the gateway config bootstrap sequence.
#[derive(Debug, Clone)]
pub struct BootstrapResult {
    pub config: DenebConfig,
    pub snapshot: ConfigSnapshot,
    pub auth: ResolvedGatewayAuth,
    pub generated_token: String,
    pub persisted_generated_token: bool,
}

/// Fully resolved gateway authentication state.
#[derive(Debug, Clone, Default)]
pub struct ResolvedGatewayAuth {
    /// "none" | "token" | "password" | "trusted-proxy"
    pub mode: String,
    pub token: String,
    pub password: String,
    pub trusted_proxy: Option<GatewayTrustedProxyConfig>,
    pub allow_tailscale: bool,
}

impl ResolvedGatewayAuth {
    /// Returns true if the auth has a non-empty token or password.
    pub fn has_shared_secret(&self) -> bool {
        match self.mode.as_str() {
            "token" => !self.token.trim().is_empty(),
            "password" => !self.password.trim().is_empty(),
            _ => false,
        }
    }
}

/// Log callback type for bootstrap diagnostics.
pub type LogFn = Box<dyn Fn(&str)>;

/// Options for the bootstrap process.
#[derive(Default)]
pub struct BootstrapOptions {
    pub config_path: Option<String>,
    pub auth_override: Option<GatewayAuthConfig>,
    pub tailscale_override: Option<GatewayTailscaleConfig>,
    pub persist: bool,
    pub log_fn: Option<LogFn>,
}

/// Perform the full gateway startup config bootstrap:
///  1. Load and validate the config snapshot.
///  2. Resolve auth (auto-generate token if needed).
///  3. Resolve environment-based secret overrides.
///  4. Optionally persist generated token to config file.
#[allow(clippy::needless_pass_by_value)]
pub fn bootstrap_gateway_config(opts: BootstrapOptions) -> Result<BootstrapResult, String> {
    let log = |msg: &str| {
        if let Some(f) = &opts.log_fn {
            f(msg);
        }
    };

    // Step 1: Load config.
    let config_path = match &opts.config_path {
        Some(p) => std::path::PathBuf::from(p),
        None => resolve_config_path(),
    };

    let snap = load_config(&config_path)?;

    // Step 2: Validate snapshot.
    if !snap.valid {
        let issue_lines: Vec<String> = snap.issues.iter().map(ToString::to_string).collect();
        return Err(format!(
            "invalid config at {}:\n{}\nRun \"deneb doctor\" to repair",
            snap.path,
            issue_lines.join("\n")
        ));
    }

    let mut cfg = snap.config.clone();

    // Step 3: Apply auth overrides from CLI/env.
    if let Some(auth_override) = &opts.auth_override {
        let base = cfg.gateway.as_mut().and_then(|g| g.auth.take());
        if let Some(g) = cfg.gateway.as_mut() {
            g.auth = Some(merge_auth_config(base.as_ref(), auth_override));
        }
    }
    if let Some(ts_override) = &opts.tailscale_override {
        let base = cfg.gateway.as_mut().and_then(|g| g.tailscale.take());
        if let Some(g) = cfg.gateway.as_mut() {
            g.tailscale = Some(merge_tailscale_config(base.as_ref(), ts_override));
        }
    }

    // Step 4: Resolve auth (env fallback + auto-generate).
    let (resolved, generated_token) = resolve_startup_auth(&cfg, &log)?;

    // Step 5: Persist generated token if applicable.
    let mut persisted = false;
    if !generated_token.is_empty() && opts.persist && opts.auth_override.is_none() {
        match persist_generated_token(&config_path, &generated_token) {
            Ok(()) => {
                persisted = true;
                log("auto-generated gateway auth token persisted to config");
            }
            Err(e) => {
                log(&format!(
                    "failed to persist generated gateway token: {e}"
                ));
            }
        }
    }

    // Step 6: Validate hooks token is not the same as gateway auth token.
    if let Some(hooks) = &cfg.hooks {
        if let Some(hooks_token) = &hooks.token {
            if !hooks_token.is_empty() && !resolved.token.is_empty() && hooks_token == &resolved.token {
                log("hooks.token should differ from gateway.auth.token for security isolation");
            }
        }
    }

    Ok(BootstrapResult {
        config: cfg,
        snapshot: snap,
        auth: resolved,
        generated_token,
        persisted_generated_token: persisted,
    })
}

/// Resolve the gateway auth mode, token/password from config and env.
/// If mode=token and no token is configured, generates a random token.
fn resolve_startup_auth(
    cfg: &DenebConfig,
    log: &dyn Fn(&str),
) -> Result<(ResolvedGatewayAuth, String), String> {
    let auth_cfg = cfg
        .gateway
        .as_ref()
        .and_then(|g| g.auth.as_ref());

    let mode = auth_cfg
        .and_then(|a| a.mode.as_deref())
        .unwrap_or("token");

    let mut resolved = ResolvedGatewayAuth {
        mode: mode.to_string(),
        ..Default::default()
    };
    let mut generated_token = String::new();

    match mode {
        "none" => {}
        "token" => {
            let config_token = auth_cfg.and_then(|a| a.token.as_deref()).unwrap_or("");
            let token = resolve_secret_value(
                config_token,
                &["DENEB_GATEWAY_TOKEN", "CLAWDBOT_GATEWAY_TOKEN"],
            );
            if token.is_empty() {
                let gen = generate_random_token()
                    .map_err(|e| format!("failed to generate gateway token: {e}"))?;
                generated_token = gen.clone();
                resolved.token = gen;
                log("auto-generated gateway auth token (no token configured)");
            } else {
                resolved.token = token;
            }
        }
        "password" => {
            let config_pw = auth_cfg
                .and_then(|a| a.password.as_deref())
                .unwrap_or("");
            let password = resolve_secret_value(
                config_pw,
                &["DENEB_GATEWAY_PASSWORD", "CLAWDBOT_GATEWAY_PASSWORD"],
            );
            if password.is_empty() {
                return Err(
                    "gateway auth mode=password requires a password (set gateway.auth.password or DENEB_GATEWAY_PASSWORD)".to_string()
                );
            }
            resolved.password = password;
        }
        "trusted-proxy" => {
            let trusted_proxy = auth_cfg.and_then(|a| a.trusted_proxy.as_ref());
            let has_user_header = trusted_proxy
                .and_then(|tp| tp.user_header.as_deref())
                .is_some_and(|h| !h.is_empty());
            if !has_user_header {
                return Err(
                    "gateway auth mode=trusted-proxy requires gateway.auth.trustedProxy.userHeader"
                        .to_string(),
                );
            }
            resolved.trusted_proxy = trusted_proxy.cloned();
        }
        _ => {}
    }

    // Resolve allowTailscale.
    if let Some(allow) = auth_cfg.and_then(|a| a.allow_tailscale) {
        resolved.allow_tailscale = allow;
    }

    Ok((resolved, generated_token))
}

/// Resolve a secret from config value, then env var fallbacks.
fn resolve_secret_value(config_value: &str, env_keys: &[&str]) -> String {
    if !config_value.trim().is_empty() {
        return config_value.to_string();
    }
    for key in env_keys {
        if let Ok(val) = env::var(key) {
            let val = val.trim().to_string();
            if !val.is_empty() {
                return val;
            }
        }
    }
    String::new()
}

/// Generate a cryptographically random hex token.
pub fn generate_random_token() -> Result<String, String> {
    let mut buf = vec![0u8; GENERATED_TOKEN_BYTES];
    rand::rng().fill_bytes(&mut buf);
    Ok(hex::encode(&buf))
}

/// Write the generated token into the config file.
fn persist_generated_token(config_path: &Path, token: &str) -> Result<(), String> {
    // Ensure parent directory exists.
    if let Some(parent) = config_path.parent() {
        fs::create_dir_all(parent)
            .map_err(|e| format!("creating config directory: {e}"))?;
    }

    // Read existing config or start with empty object.
    let mut raw: serde_json::Map<String, serde_json::Value> = match fs::read(config_path) {
        Ok(data) => serde_json::from_slice(&data)
            .map_err(|e| format!("parsing config: {e}"))?,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => serde_json::Map::new(),
        Err(e) => return Err(format!("reading config: {e}")),
    };

    // Set gateway.auth.token.
    let gw = raw
        .entry("gateway")
        .or_insert_with(|| serde_json::json!({}))
        .as_object_mut()
        .ok_or("gateway is not an object")?;

    let auth = gw
        .entry("auth")
        .or_insert_with(|| serde_json::json!({}))
        .as_object_mut()
        .ok_or("auth is not an object")?;

    auth.insert("token".to_string(), serde_json::json!(token));

    // Update meta.
    let meta = raw
        .entry("meta")
        .or_insert_with(|| serde_json::json!({}))
        .as_object_mut()
        .ok_or("meta is not an object")?;

    let now = chrono::Utc::now().to_rfc3339();
    meta.insert("lastTouchedAt".to_string(), serde_json::json!(now));

    let out = serde_json::to_string_pretty(&raw)
        .map_err(|e| format!("encoding config: {e}"))?;

    fs::write(config_path, format!("{out}\n"))
        .map_err(|e| format!("writing config: {e}"))?;

    Ok(())
}

/// Merge an override auth config into the base.
pub fn merge_auth_config(
    base: Option<&GatewayAuthConfig>,
    overrides: &GatewayAuthConfig,
) -> GatewayAuthConfig {
    let base = match base {
        Some(b) => b.clone(),
        None => return overrides.clone(),
    };

    GatewayAuthConfig {
        mode: overrides.mode.clone().or(base.mode),
        token: overrides.token.clone().or(base.token),
        password: overrides.password.clone().or(base.password),
        allow_tailscale: overrides.allow_tailscale.or(base.allow_tailscale),
        rate_limit: overrides.rate_limit.clone().or(base.rate_limit),
        trusted_proxy: overrides.trusted_proxy.clone().or(base.trusted_proxy),
    }
}

/// Merge an override tailscale config into the base.
pub fn merge_tailscale_config(
    base: Option<&GatewayTailscaleConfig>,
    overrides: &GatewayTailscaleConfig,
) -> GatewayTailscaleConfig {
    let base = match base {
        Some(b) => b.clone(),
        None => return overrides.clone(),
    };

    GatewayTailscaleConfig {
        mode: overrides.mode.clone().or(base.mode),
        reset_on_exit: overrides.reset_on_exit.or(base.reset_on_exit),
    }
}

/// Resolve the media cleanup TTL from hours to milliseconds.
/// Bounds: 1 hour minimum, 168 hours (7 days) maximum.
pub fn resolve_media_cleanup_ttl_ms(ttl_hours: i32) -> Result<i64, String> {
    let clamped = ttl_hours.clamp(1, MAX_MEDIA_TTL_HOURS);
    let ttl_ms = i64::from(clamped) * 60 * 60_000;
    if ttl_ms <= 0 {
        return Err(format!("invalid media.ttlHours: {ttl_hours}"));
    }
    Ok(ttl_ms)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bootstrap_missing_file() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("nonexistent.json");
        let result = bootstrap_gateway_config(BootstrapOptions {
            config_path: Some(path.to_string_lossy().to_string()),
            persist: false,
            ..Default::default()
        })
        .expect("bootstrap");

        // Should auto-generate a token.
        assert!(!result.generated_token.is_empty());
        assert_eq!(result.auth.mode, "token");
        assert!(!result.auth.token.is_empty());
    }

    #[test]
    fn bootstrap_with_token() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("deneb.json");
        fs::write(
            &path,
            r#"{"gateway": {"auth": {"mode": "token", "token": "my-secret"}}}"#,
        )
        .expect("write");

        let result = bootstrap_gateway_config(BootstrapOptions {
            config_path: Some(path.to_string_lossy().to_string()),
            ..Default::default()
        })
        .expect("bootstrap");

        assert_eq!(result.auth.mode, "token");
        assert_eq!(result.auth.token, "my-secret");
        assert!(result.generated_token.is_empty());
    }

    #[test]
    fn bootstrap_password_mode() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("deneb.json");
        fs::write(
            &path,
            r#"{"gateway": {"auth": {"mode": "password", "password": "s3cret"}}}"#,
        )
        .expect("write");

        let result = bootstrap_gateway_config(BootstrapOptions {
            config_path: Some(path.to_string_lossy().to_string()),
            ..Default::default()
        })
        .expect("bootstrap");

        assert_eq!(result.auth.mode, "password");
        assert_eq!(result.auth.password, "s3cret");
    }

    #[test]
    fn bootstrap_password_mode_no_password() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("deneb.json");
        fs::write(
            &path,
            r#"{"gateway": {"auth": {"mode": "password"}}}"#,
        )
        .expect("write");

        let result = bootstrap_gateway_config(BootstrapOptions {
            config_path: Some(path.to_string_lossy().to_string()),
            ..Default::default()
        });

        assert!(result.is_err());
        assert!(result.err().expect("err").contains("requires a password"));
    }

    #[test]
    fn bootstrap_persist_token() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("deneb.json");

        let result = bootstrap_gateway_config(BootstrapOptions {
            config_path: Some(path.to_string_lossy().to_string()),
            persist: true,
            ..Default::default()
        })
        .expect("bootstrap");

        assert!(result.persisted_generated_token);

        // Verify the persisted file.
        let data = fs::read_to_string(&path).expect("read");
        let parsed: serde_json::Value = serde_json::from_str(&data).expect("parse");
        let token = parsed["gateway"]["auth"]["token"]
            .as_str()
            .expect("token");
        assert_eq!(token, result.generated_token);
    }

    #[test]
    fn bootstrap_auth_override() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let path = tmp.path().join("deneb.json");
        fs::write(
            &path,
            r#"{"gateway": {"auth": {"mode": "token", "token": "original"}}}"#,
        )
        .expect("write");

        let result = bootstrap_gateway_config(BootstrapOptions {
            config_path: Some(path.to_string_lossy().to_string()),
            auth_override: Some(GatewayAuthConfig {
                token: Some("override-token".to_string()),
                ..Default::default()
            }),
            ..Default::default()
        })
        .expect("bootstrap");

        assert_eq!(result.auth.token, "override-token");
    }

    #[test]
    fn generate_random_token_format() {
        let token = generate_random_token().expect("generate");
        assert_eq!(token.len(), GENERATED_TOKEN_BYTES * 2); // hex encoding
        assert!(token.chars().all(|c| c.is_ascii_hexdigit()));
    }

    #[test]
    fn resolve_media_cleanup_ttl_ms_bounds() {
        // Minimum clamp.
        assert_eq!(resolve_media_cleanup_ttl_ms(0).expect("ok"), 3_600_000);
        assert_eq!(resolve_media_cleanup_ttl_ms(-1).expect("ok"), 3_600_000);

        // Normal value.
        assert_eq!(resolve_media_cleanup_ttl_ms(24).expect("ok"), 86_400_000);

        // Maximum clamp (7 days).
        assert_eq!(
            resolve_media_cleanup_ttl_ms(999).expect("ok"),
            168 * 60 * 60_000
        );
    }

    #[test]
    fn merge_auth_config_override() {
        let base = GatewayAuthConfig {
            mode: Some("token".to_string()),
            token: Some("base-token".to_string()),
            ..Default::default()
        };
        let overrides = GatewayAuthConfig {
            token: Some("new-token".to_string()),
            ..Default::default()
        };
        let merged = merge_auth_config(Some(&base), &overrides);
        assert_eq!(merged.mode.as_deref(), Some("token"));
        assert_eq!(merged.token.as_deref(), Some("new-token"));
    }

    #[test]
    fn merge_tailscale_config_override() {
        let base = GatewayTailscaleConfig {
            mode: Some("off".to_string()),
            ..Default::default()
        };
        let overrides = GatewayTailscaleConfig {
            mode: Some("serve".to_string()),
            ..Default::default()
        };
        let merged = merge_tailscale_config(Some(&base), &overrides);
        assert_eq!(merged.mode.as_deref(), Some("serve"));
    }

    #[test]
    fn resolved_auth_has_shared_secret() {
        let auth = ResolvedGatewayAuth {
            mode: "token".to_string(),
            token: "abc".to_string(),
            ..Default::default()
        };
        assert!(auth.has_shared_secret());

        let auth = ResolvedGatewayAuth {
            mode: "token".to_string(),
            token: "  ".to_string(),
            ..Default::default()
        };
        assert!(!auth.has_shared_secret());

        let auth = ResolvedGatewayAuth {
            mode: "password".to_string(),
            password: "secret".to_string(),
            ..Default::default()
        };
        assert!(auth.has_shared_secret());

        let auth = ResolvedGatewayAuth {
            mode: "none".to_string(),
            ..Default::default()
        };
        assert!(!auth.has_shared_secret());
    }
}
