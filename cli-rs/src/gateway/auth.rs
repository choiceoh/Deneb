use crate::config::DenebConfig;
use crate::env::get_env_trimmed;

/// Resolved gateway authentication credentials.
#[derive(Debug, Default)]
pub struct GatewayAuth {
    pub token: Option<String>,
    pub password: Option<String>,
    /// Where the token came from (for diagnostics).
    #[allow(dead_code)]
    pub source: AuthSource,
}

#[derive(Debug, Default)]
pub enum AuthSource {
    CliFlag,
    EnvVar,
    Config,
    #[default]
    None,
}

/// Resolve gateway auth credentials.
///
/// Precedence:
/// 1. Explicit CLI flags (--token, --password)
/// 2. Environment variables (DENEB_GATEWAY_TOKEN, DENEB_GATEWAY_PASSWORD)
/// 3. Config file (gateway.auth.token, gateway.auth.password)
pub fn resolve_gateway_auth(
    cli_token: Option<&str>,
    cli_password: Option<&str>,
    config: &DenebConfig,
) -> GatewayAuth {
    // Token resolution
    let (token, source) = if let Some(t) = cli_token.filter(|s| !s.trim().is_empty()) {
        (Some(t.trim().to_string()), AuthSource::CliFlag)
    } else if let Some(t) =
        get_env_trimmed("DENEB_GATEWAY_TOKEN").or_else(|| get_env_trimmed("CLAWDBOT_GATEWAY_TOKEN"))
    {
        (Some(t), AuthSource::EnvVar)
    } else if let Some(t) = config.auth_token() {
        (Some(t.to_string()), AuthSource::Config)
    } else {
        (None, AuthSource::None)
    };

    // Password resolution
    let password = if let Some(p) = cli_password.filter(|s| !s.trim().is_empty()) {
        Some(p.trim().to_string())
    } else if let Some(p) = get_env_trimmed("DENEB_GATEWAY_PASSWORD")
        .or_else(|| get_env_trimmed("CLAWDBOT_GATEWAY_PASSWORD"))
    {
        Some(p)
    } else {
        config
            .gateway
            .as_ref()
            .and_then(|g| g.auth.as_ref())
            .and_then(|a| a.password.as_deref())
            .filter(|p| !p.trim().is_empty())
            .map(|p| p.trim().to_string())
    };

    GatewayAuth {
        token,
        password,
        source,
    }
}
