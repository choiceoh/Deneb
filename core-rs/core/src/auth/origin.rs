//! Browser origin validation (CORS-like checks).
//!
//! 1:1 port of `gateway-go/internal/auth/origin.go`.

use std::collections::HashSet;
use std::net::IpAddr;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/// Outcome of a browser origin validation.
#[derive(Debug, Clone)]
pub struct OriginCheckResult {
    pub ok: bool,
    pub matched_by: String, // "allowlist", "host-header-fallback", "local-loopback"
    pub reason: String,
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/// Validate the Origin header against allowed origins.
/// Mirrors `checkBrowserOrigin` in the TypeScript/Go codebases.
pub fn check_browser_origin(
    request_host: &str,
    origin: &str,
    allowed_origins: &[String],
    allow_host_header_fallback: bool,
    is_local_client: bool,
) -> OriginCheckResult {
    let Some(parsed) = parse_origin(origin) else {
        return OriginCheckResult {
            ok: false,
            matched_by: String::new(),
            reason: "origin missing or invalid".into(),
        };
    };

    // Check allowlist.
    let allowlist: HashSet<String> = allowed_origins
        .iter()
        .map(|o| o.trim().to_lowercase())
        .filter(|o| !o.is_empty())
        .collect();

    if allowlist.contains("*") || allowlist.contains(&parsed.origin) {
        return OriginCheckResult {
            ok: true,
            matched_by: "allowlist".into(),
            reason: String::new(),
        };
    }

    // Host-header fallback.
    let normalized_host = normalize_host_header(request_host);
    if allow_host_header_fallback && !normalized_host.is_empty() && parsed.host == normalized_host {
        return OriginCheckResult {
            ok: true,
            matched_by: "host-header-fallback".into(),
            reason: String::new(),
        };
    }

    // Local loopback fallback (only for genuinely local socket clients).
    if is_local_client && is_loopback_host(&parsed.hostname) {
        return OriginCheckResult {
            ok: true,
            matched_by: "local-loopback".into(),
            reason: String::new(),
        };
    }

    OriginCheckResult {
        ok: false,
        matched_by: String::new(),
        reason: "origin not allowed".into(),
    }
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

struct ParsedOrigin {
    origin: String,   // scheme://host
    host: String,     // host (with port)
    hostname: String, // host (without port)
}

fn parse_origin(raw: &str) -> Option<ParsedOrigin> {
    let trimmed = raw.trim();
    if trimmed.is_empty() || trimmed == "null" {
        return None;
    }

    // Minimal URL parsing: split scheme and host.
    let scheme_end = trimmed.find("://")?;
    let scheme = trimmed[..scheme_end].to_lowercase();
    let rest = &trimmed[scheme_end + 3..];

    // Host is everything up to the first '/' or end of string.
    let host_str = if let Some(slash) = rest.find('/') {
        &rest[..slash]
    } else {
        rest
    };

    if host_str.is_empty() {
        return None;
    }

    let host = host_str.to_lowercase();
    // Strip port for hostname.
    let hostname = if let Some(bracket_end) = host.find(']') {
        // IPv6 literal: [::1]:port
        host[..=bracket_end].trim_matches('[').trim_matches(']').to_string()
    } else if let Some(colon) = host.rfind(':') {
        host[..colon].to_string()
    } else {
        host.clone()
    };

    Some(ParsedOrigin {
        origin: format!("{scheme}://{host}"),
        host,
        hostname,
    })
}

fn normalize_host_header(host: &str) -> String {
    let host = host.trim().to_lowercase();
    // Strip default ports.
    if host.ends_with(":80") || host.ends_with(":443") {
        if let Some(colon) = host.rfind(':') {
            return host[..colon].to_string();
        }
    }
    host
}

fn is_loopback_host(hostname: &str) -> bool {
    if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" || hostname == "[::1]"
    {
        return true;
    }
    hostname
        .parse::<IpAddr>()
        .is_ok_and(|ip| ip.is_loopback())
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn allowlist_match() {
        let result = check_browser_origin(
            "",
            "https://example.com",
            &["https://example.com".into()],
            false,
            false,
        );
        assert!(result.ok);
        assert_eq!(result.matched_by, "allowlist");
    }

    #[test]
    fn wildcard_match() {
        let result = check_browser_origin(
            "",
            "https://anything.com",
            &["*".into()],
            false,
            false,
        );
        assert!(result.ok);
        assert_eq!(result.matched_by, "allowlist");
    }

    #[test]
    fn missing_origin() {
        let result = check_browser_origin("", "", &[], false, false);
        assert!(!result.ok);
    }

    #[test]
    fn null_origin() {
        let result = check_browser_origin("", "null", &[], false, false);
        assert!(!result.ok);
    }

    #[test]
    fn host_header_fallback() {
        let result = check_browser_origin(
            "example.com",
            "https://example.com",
            &[],
            true,
            false,
        );
        assert!(result.ok);
        assert_eq!(result.matched_by, "host-header-fallback");
    }

    #[test]
    fn host_header_fallback_disabled() {
        let result = check_browser_origin(
            "example.com",
            "https://example.com",
            &[],
            false,
            false,
        );
        assert!(!result.ok);
    }

    #[test]
    fn local_loopback() {
        let result = check_browser_origin("", "http://localhost:3000", &[], false, true);
        assert!(result.ok);
        assert_eq!(result.matched_by, "local-loopback");
    }

    #[test]
    fn local_loopback_not_local_client() {
        let result = check_browser_origin("", "http://localhost:3000", &[], false, false);
        assert!(!result.ok);
    }

    #[test]
    fn loopback_127001() {
        let result = check_browser_origin("", "http://127.0.0.1:8080", &[], false, true);
        assert!(result.ok);
        assert_eq!(result.matched_by, "local-loopback");
    }

    #[test]
    fn not_allowed() {
        let result = check_browser_origin(
            "other.com",
            "https://evil.com",
            &["https://good.com".into()],
            true,
            false,
        );
        assert!(!result.ok);
        assert_eq!(result.reason, "origin not allowed");
    }

    #[test]
    fn case_insensitive() {
        let result = check_browser_origin(
            "",
            "HTTPS://EXAMPLE.COM",
            &["https://example.com".into()],
            false,
            false,
        );
        assert!(result.ok);
    }
}
