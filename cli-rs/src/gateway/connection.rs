use crate::config::DenebConfig;
use crate::env::get_env_trimmed;

/// Details about how we'll connect to the gateway.
#[derive(Debug, Clone)]
pub struct ConnectionDetails {
    pub url: String,
    pub url_source: String,
    #[allow(dead_code)]
    pub bind_detail: Option<String>,
    #[allow(dead_code)]
    pub remote_fallback_note: Option<String>,
}

/// Resolve the gateway WebSocket URL and connection details.
///
/// Precedence:
/// 1. Explicit `--url` CLI flag
/// 2. `DENEB_GATEWAY_URL` / `CLAWDBOT_GATEWAY_URL` env
/// 3. Config `gateway.remote.url` (when mode=remote)
/// 4. Local loopback: `ws://127.0.0.1:{port}`
pub fn resolve_connection_details(
    cli_url: Option<&str>,
    config: &DenebConfig,
    port: u16,
) -> ConnectionDetails {
    let tls_enabled = config.tls_enabled();
    let scheme = if tls_enabled { "wss" } else { "ws" };
    let local_url = format!("{scheme}://127.0.0.1:{port}");
    let bind_mode = config
        .gateway
        .as_ref()
        .and_then(|g| g.bind.as_deref())
        .unwrap_or("loopback");

    // CLI override
    let cli_override = cli_url
        .filter(|u| !u.trim().is_empty())
        .map(|u| u.trim().to_string());

    // Env override
    let env_override = if cli_override.is_none() {
        get_env_trimmed("DENEB_GATEWAY_URL").or_else(|| get_env_trimmed("CLAWDBOT_GATEWAY_URL"))
    } else {
        None
    };

    let url_override = cli_override.as_deref().or(env_override.as_deref());

    // Remote config URL
    let remote_url = if config.is_remote_mode() {
        config.remote_url().map(|u| u.to_string())
    } else {
        None
    };

    let remote_misconfigured =
        config.is_remote_mode() && url_override.is_none() && remote_url.is_none();

    let (url, url_source) = if let Some(u) = url_override {
        let source = if cli_override.is_some() {
            "cli --url".to_string()
        } else {
            "env DENEB_GATEWAY_URL".to_string()
        };
        (u.to_string(), source)
    } else if let Some(u) = &remote_url {
        (u.clone(), "config gateway.remote.url".to_string())
    } else if remote_misconfigured {
        (
            local_url.clone(),
            "missing gateway.remote.url (fallback local)".to_string(),
        )
    } else {
        (local_url, "local loopback".to_string())
    };

    let bind_detail = if url_override.is_none() && remote_url.is_none() {
        Some(format!("Bind: {bind_mode}"))
    } else {
        None
    };

    let remote_fallback_note = if remote_misconfigured {
        Some(
            "Warn: gateway.mode=remote but gateway.remote.url is missing; \
             set gateway.remote.url or switch gateway.mode=local."
                .to_string(),
        )
    } else {
        None
    };

    // Security: block plaintext ws:// to non-loopback addresses
    if !is_secure_or_loopback(&url) {
        eprintln!(
            "SECURITY ERROR: Gateway URL \"{url}\" uses plaintext ws:// to a non-loopback address.\n\
             Fix: Use wss:// for remote gateway URLs."
        );
        std::process::exit(1);
    }

    ConnectionDetails {
        url,
        url_source,
        bind_detail,
        remote_fallback_note,
    }
}

/// Check if a WebSocket URL is either wss:// or targeting loopback.
fn is_secure_or_loopback(url: &str) -> bool {
    if url.starts_with("wss://") {
        return true;
    }
    if !url.starts_with("ws://") {
        return true; // Not a WS URL at all, don't block
    }

    // Allow break-glass env
    if std::env::var("DENEB_ALLOW_INSECURE_PRIVATE_WS")
        .ok()
        .as_deref()
        == Some("1")
    {
        return true;
    }

    // Extract host from ws://host:port/...
    let after_scheme = &url[5..]; // skip "ws://"
    let authority = after_scheme.split('/').next().unwrap_or("");

    // Handle IPv6 bracketed addresses like [::1]:18789
    let host = if authority.starts_with('[') {
        // IPv6: extract content between brackets
        authority
            .split(']')
            .next()
            .unwrap_or("")
            .trim_start_matches('[')
    } else {
        // IPv4/hostname: split on last colon for port
        authority
            .rsplit_once(':')
            .map(|(h, _)| h)
            .unwrap_or(authority)
    };

    matches!(host, "127.0.0.1" | "localhost" | "::1")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn secure_or_loopback_checks() {
        assert!(is_secure_or_loopback("wss://example.com:443"));
        assert!(is_secure_or_loopback("ws://127.0.0.1:18789"));
        assert!(is_secure_or_loopback("ws://localhost:18789"));
        assert!(is_secure_or_loopback("ws://[::1]:18789"));
    }
}
