//! Gateway credential resolution for single-user deployment.
//!
//! 1:1 port of `gateway-go/internal/auth/credentials.go`.
//! Simplified from the TypeScript version (no remote mode, no Tailscale, no secret refs).

use std::env;

use super::middleware::AuthMode;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/// Where a credential was resolved from.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CredentialSource {
    Env,
    Config,
    Generated,
    None,
}

impl CredentialSource {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Env => "env",
            Self::Config => "config",
            Self::Generated => "generated",
            Self::None => "none",
        }
    }
}

/// Resolved gateway authentication credentials.
#[derive(Debug, Clone)]
pub struct CredentialPlan {
    pub mode: AuthMode,
    pub token: String,    // never serialize
    pub password: String, // never serialize
    pub source: CredentialSource,
}

/// Config-file credential values used for resolution.
#[derive(Debug, Clone, Default)]
pub struct CredentialConfig {
    pub auth_mode: String, // gateway.auth.mode
    pub token: String,     // gateway.auth.token
    pub password: String,  // gateway.auth.password
}

// ---------------------------------------------------------------------------
// Resolution
// ---------------------------------------------------------------------------

/// Resolve gateway authentication credentials from environment and config.
/// Resolution order for token: `DENEB_GATEWAY_TOKEN` env -> config token.
/// Resolution order for password: `DENEB_GATEWAY_PASSWORD` env -> config password.
pub fn resolve_credentials(cfg: &CredentialConfig) -> CredentialPlan {
    let mode_str = cfg.auth_mode.trim();

    match mode_str {
        "none" => CredentialPlan {
            mode: AuthMode::None,
            token: String::new(),
            password: String::new(),
            source: CredentialSource::None,
        },
        "password" => {
            // Resolve password.
            if let Some(env_pw) = read_env_credential("DENEB_GATEWAY_PASSWORD") {
                CredentialPlan {
                    mode: AuthMode::Password,
                    token: String::new(),
                    password: env_pw,
                    source: CredentialSource::Env,
                }
            } else if !cfg.password.is_empty() {
                CredentialPlan {
                    mode: AuthMode::Password,
                    token: String::new(),
                    password: cfg.password.clone(),
                    source: CredentialSource::Config,
                }
            } else {
                CredentialPlan {
                    mode: AuthMode::Password,
                    token: String::new(),
                    password: String::new(),
                    source: CredentialSource::None,
                }
            }
        }
        _ => {
            // Default: token mode.
            if let Some(env_token) = read_env_credential("DENEB_GATEWAY_TOKEN") {
                CredentialPlan {
                    mode: AuthMode::Token,
                    token: env_token,
                    password: String::new(),
                    source: CredentialSource::Env,
                }
            } else if !cfg.token.is_empty() {
                CredentialPlan {
                    mode: AuthMode::Token,
                    token: cfg.token.clone(),
                    password: String::new(),
                    source: CredentialSource::Config,
                }
            } else {
                CredentialPlan {
                    mode: AuthMode::Token,
                    token: String::new(),
                    password: String::new(),
                    source: CredentialSource::None,
                }
            }
        }
    }
}

/// Resolve lightweight read-only credentials for gateway health probes.
/// Same resolution as `resolve_credentials` but intended for probe-role access.
pub fn resolve_probe_auth(cfg: &CredentialConfig) -> CredentialPlan {
    resolve_credentials(cfg)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Read a credential from an environment variable.
/// Returns `None` if not set, empty/whitespace, or contains shell expansion patterns.
fn read_env_credential(key: &str) -> Option<String> {
    let val = env::var(key).ok()?;
    let val = val.trim();
    if val.is_empty() {
        return None;
    }
    if contains_shell_expansion(val) {
        return None;
    }
    Some(val.to_string())
}

/// Check whether a string contains shell variable or command substitution
/// patterns: `$VAR`, `${VAR}`, or `$(cmd)`.
fn contains_shell_expansion(s: &str) -> bool {
    let bytes = s.as_bytes();
    for i in 0..bytes.len().saturating_sub(1) {
        if bytes[i] != b'$' {
            continue;
        }
        let next = bytes[i + 1];
        if next == b'{'
            || next == b'('
            || next == b'_'
            || next.is_ascii_uppercase()
            || next.is_ascii_lowercase()
        {
            return true;
        }
    }
    false
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    // Helper to set/unset env vars safely in tests.
    // NOTE: env var tests must not run in parallel — use serial or accept minor flakiness.

    #[test]
    fn resolve_credentials_token_from_env() {
        env::set_var("DENEB_GATEWAY_TOKEN", "env-token-123");
        let plan = resolve_credentials(&CredentialConfig {
            auth_mode: "token".into(),
            ..Default::default()
        });
        env::remove_var("DENEB_GATEWAY_TOKEN");

        assert_eq!(plan.mode, AuthMode::Token);
        assert_eq!(plan.token, "env-token-123");
        assert_eq!(plan.source, CredentialSource::Env);
    }

    #[test]
    fn resolve_credentials_token_from_config() {
        env::remove_var("DENEB_GATEWAY_TOKEN");
        let plan = resolve_credentials(&CredentialConfig {
            auth_mode: "token".into(),
            token: "config-token-456".into(),
            ..Default::default()
        });
        assert_eq!(plan.token, "config-token-456");
        assert_eq!(plan.source, CredentialSource::Config);
    }

    #[test]
    fn resolve_credentials_env_precedence_over_config() {
        env::set_var("DENEB_GATEWAY_TOKEN", "env-wins");
        let plan = resolve_credentials(&CredentialConfig {
            auth_mode: "token".into(),
            token: "config-loses".into(),
            ..Default::default()
        });
        env::remove_var("DENEB_GATEWAY_TOKEN");

        assert_eq!(plan.token, "env-wins");
        assert_eq!(plan.source, CredentialSource::Env);
    }

    #[test]
    fn resolve_credentials_password_mode() {
        env::set_var("DENEB_GATEWAY_PASSWORD", "pw-123");
        let plan = resolve_credentials(&CredentialConfig {
            auth_mode: "password".into(),
            ..Default::default()
        });
        env::remove_var("DENEB_GATEWAY_PASSWORD");

        assert_eq!(plan.mode, AuthMode::Password);
        assert_eq!(plan.password, "pw-123");
    }

    #[test]
    fn resolve_credentials_none_mode() {
        let plan = resolve_credentials(&CredentialConfig {
            auth_mode: "none".into(),
            ..Default::default()
        });
        assert_eq!(plan.mode, AuthMode::None);
        assert_eq!(plan.source, CredentialSource::None);
    }

    #[test]
    fn resolve_credentials_rejects_shell_expansion() {
        env::set_var("DENEB_GATEWAY_TOKEN", "${SOME_VAR}");
        let plan = resolve_credentials(&CredentialConfig {
            auth_mode: "token".into(),
            ..Default::default()
        });
        env::remove_var("DENEB_GATEWAY_TOKEN");

        assert!(plan.token.is_empty());
    }

    #[test]
    fn resolve_credentials_defaults_to_token() {
        env::remove_var("DENEB_GATEWAY_TOKEN");
        let plan = resolve_credentials(&CredentialConfig::default());
        assert_eq!(plan.mode, AuthMode::Token);
    }

    #[test]
    fn resolve_probe_auth_works() {
        env::set_var("DENEB_GATEWAY_TOKEN", "probe-tok");
        let plan = resolve_probe_auth(&CredentialConfig {
            auth_mode: "token".into(),
            ..Default::default()
        });
        env::remove_var("DENEB_GATEWAY_TOKEN");

        assert_eq!(plan.token, "probe-tok");
    }

    #[test]
    fn contains_shell_expansion_cases() {
        assert!(contains_shell_expansion("${VAR}"));
        assert!(contains_shell_expansion("$HOME"));
        assert!(contains_shell_expansion("$(whoami)"));
        assert!(contains_shell_expansion("$_var"));
        assert!(!contains_shell_expansion("plain-value"));
        assert!(!contains_shell_expansion("just$"));
        assert!(!contains_shell_expansion("$1"));
    }
}
