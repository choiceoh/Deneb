//! Auth middleware: authorize, rate limiting, loopback bypass, constant-time compare.
//!
//! 1:1 port of `gateway-go/internal/auth/middleware.go`.

use std::collections::HashMap;
use std::net::IpAddr;
use std::time::{Duration, Instant};

use parking_lot::Mutex;

// ---------------------------------------------------------------------------
// AuthMode
// ---------------------------------------------------------------------------

/// How the gateway authenticates clients.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AuthMode {
    None,
    Token,
    Password,
}

impl AuthMode {
    pub fn from_str_exact(s: &str) -> Self {
        match s {
            "none" => Self::None,
            "password" => Self::Password,
            _ => Self::Token, // default
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::None => "none",
            Self::Token => "token",
            Self::Password => "password",
        }
    }
}

// ---------------------------------------------------------------------------
// ResolvedAuth
// ---------------------------------------------------------------------------

/// Resolved authentication configuration for the gateway.
#[derive(Debug, Clone)]
pub struct ResolvedAuth {
    pub mode: AuthMode,
    pub token: String,           // never serialize
    pub password: String,        // never serialize
    pub allow_tailscale: bool,
}

// ---------------------------------------------------------------------------
// AuthResult
// ---------------------------------------------------------------------------

/// Outcome of an authentication attempt.
#[derive(Debug, Clone)]
pub struct AuthResult {
    pub ok: bool,
    pub method: String,          // "none", "token", "password", "local"
    pub user: String,
    pub reason: String,
    pub rate_limited: bool,
    pub retry_after_ms: i64,
}

impl AuthResult {
    fn success(method: &str) -> Self {
        Self {
            ok: true,
            method: method.to_string(),
            user: String::new(),
            reason: String::new(),
            rate_limited: false,
            retry_after_ms: 0,
        }
    }

    fn failure(reason: &str) -> Self {
        Self {
            ok: false,
            method: String::new(),
            user: String::new(),
            reason: reason.to_string(),
            rate_limited: false,
            retry_after_ms: 0,
        }
    }

    fn rate_limited(retry_after_ms: i64) -> Self {
        Self {
            ok: false,
            method: String::new(),
            user: String::new(),
            reason: "rate limited".to_string(),
            rate_limited: true,
            retry_after_ms,
        }
    }
}

// ---------------------------------------------------------------------------
// AuthRateLimiter
// ---------------------------------------------------------------------------

/// Max entries to cap the failure tracking map (`DDoS` protection).
const MAX_RATE_LIMIT_ENTRIES: usize = 10_000;

struct IpFailures {
    count: usize,
    first_at: Instant,
    locked_at: Option<Instant>,
}

/// Sliding-window rate limiter for auth failures, keyed by IP.
pub struct AuthRateLimiter {
    inner: Mutex<HashMap<String, IpFailures>>,
    max_failures: usize,
    window: Duration,
    lockout: Duration,
}

impl AuthRateLimiter {
    pub fn new(max_failures: usize, window_ms: u64, lockout_ms: u64) -> Self {
        Self {
            inner: Mutex::new(HashMap::new()),
            max_failures,
            window: Duration::from_millis(window_ms),
            lockout: Duration::from_millis(lockout_ms),
        }
    }

    /// Check whether the IP is allowed to attempt auth.
    /// Returns `(allowed, retry_after_ms)`.
    pub fn check(&self, ip: &str) -> (bool, i64) {
        let mut map = self.inner.lock();
        let Some(f) = map.get(ip) else {
            return (true, 0);
        };
        let now = Instant::now();

        if let Some(locked_at) = f.locked_at {
            let elapsed = now.duration_since(locked_at);
            if elapsed < self.lockout {
                let remaining = (self.lockout - elapsed).as_millis() as i64;
                return (false, remaining);
            }
            // Lockout expired.
            map.remove(ip);
            return (true, 0);
        }

        // Window expired, reset.
        if now.duration_since(f.first_at) > self.window {
            map.remove(ip);
            return (true, 0);
        }

        (true, 0)
    }

    /// Record a failed auth attempt for an IP.
    pub fn record_failure(&self, ip: &str) {
        let mut map = self.inner.lock();
        let now = Instant::now();

        // Prevent unbounded map growth under DDoS.
        if map.len() >= MAX_RATE_LIMIT_ENTRIES && !map.contains_key(ip) {
            return; // silently drop new entries at capacity
        }

        let entry = map.entry(ip.to_string()).or_insert(IpFailures {
            count: 0,
            first_at: now,
            locked_at: None,
        });

        // Reset if window expired.
        if now.duration_since(entry.first_at) > self.window {
            entry.count = 1;
            entry.first_at = now;
            entry.locked_at = None;
            return;
        }

        entry.count += 1;
        if entry.count >= self.max_failures {
            entry.locked_at = Some(now);
        }
    }

    /// Clear failure tracking for an IP (e.g. after successful auth).
    pub fn reset(&self, ip: &str) {
        self.inner.lock().remove(ip);
    }

    /// Garbage-collect expired entries. Call periodically from a timer.
    pub fn gc(&self) {
        let mut map = self.inner.lock();
        let now = Instant::now();
        map.retain(|_, f| {
            if let Some(locked_at) = f.locked_at {
                now.duration_since(locked_at) <= self.lockout
            } else {
                now.duration_since(f.first_at) <= self.window
            }
        });
    }
}

// ---------------------------------------------------------------------------
// Authorize
// ---------------------------------------------------------------------------

/// Perform authentication check against resolved auth config.
pub fn authorize(
    resolved: &ResolvedAuth,
    bearer_token: &str,
    password: &str,
    remote_ip: &str,
    rate_limiter: Option<&AuthRateLimiter>,
) -> AuthResult {
    // Mode: none — allow all.
    if resolved.mode == AuthMode::None {
        return AuthResult::success("none");
    }

    // Local direct requests always pass.
    if is_loopback(remote_ip) {
        return AuthResult::success("local");
    }

    // Rate limit check.
    if let Some(rl) = rate_limiter {
        let (allowed, retry_ms) = rl.check(remote_ip);
        if !allowed {
            return AuthResult::rate_limited(retry_ms);
        }
    }

    // Token auth.
    if resolved.mode == AuthMode::Token
        && !resolved.token.is_empty()
        && !bearer_token.is_empty()
        && constant_time_equal(bearer_token, &resolved.token)
    {
        if let Some(rl) = rate_limiter {
            rl.reset(remote_ip);
        }
        return AuthResult::success("token");
    }

    // Password auth.
    if resolved.mode == AuthMode::Password
        && !resolved.password.is_empty()
        && !password.is_empty()
        && constant_time_equal(password, &resolved.password)
    {
        if let Some(rl) = rate_limiter {
            rl.reset(remote_ip);
        }
        return AuthResult::success("password");
    }

    // Failure.
    if let Some(rl) = rate_limiter {
        rl.record_failure(remote_ip);
    }
    AuthResult::failure("invalid credentials")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Whether the IP string is a loopback address.
pub fn is_loopback(ip: &str) -> bool {
    ip.parse::<IpAddr>().is_ok_and(|addr| addr.is_loopback())
}

/// Constant-time string comparison (timing-safe for secrets).
pub fn constant_time_equal(a: &str, b: &str) -> bool {
    let a = a.as_bytes();
    let b = b.as_bytes();
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for (x, y) in a.iter().zip(b.iter()) {
        diff |= x ^ y;
    }
    diff == 0
}

/// Extract a Bearer token from an Authorization header value.
/// Returns `None` if the header is empty or not a Bearer scheme.
pub fn get_bearer_token(auth_header: &str) -> Option<&str> {
    if auth_header.is_empty() {
        return None;
    }
    let prefix = "Bearer ";
    if auth_header.len() > prefix.len()
        && auth_header[..prefix.len()].eq_ignore_ascii_case(prefix)
    {
        let token = &auth_header[prefix.len()..];
        if token.is_empty() {
            None
        } else {
            Some(token)
        }
    } else {
        None
    }
}

/// Extract client IP from X-Forwarded-For header value or raw remote addr.
/// `xff` is the X-Forwarded-For header value (may be empty).
/// `remote_addr` is the raw socket addr (e.g. "192.168.1.1:12345").
pub fn remote_ip(xff: &str, remote_addr: &str) -> String {
    if !xff.is_empty() {
        if let Some(i) = xff.find(',') {
            return xff[..i].trim().to_string();
        }
        return xff.trim().to_string();
    }
    // Strip port from remote_addr.
    if let Some(i) = remote_addr.rfind(':') {
        // Distinguish IPv6 (contains multiple colons) from host:port.
        let before = &remote_addr[..i];
        if before.contains(':') {
            // IPv6 — return as-is if bracketed, otherwise whole string.
            return remote_addr.to_string();
        }
        return before.to_string();
    }
    remote_addr.to_string()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn authorize_mode_none() {
        let resolved = ResolvedAuth {
            mode: AuthMode::None,
            token: String::new(),
            password: String::new(),
            allow_tailscale: false,
        };
        let result = authorize(&resolved, "", "", "1.2.3.4", None);
        assert!(result.ok);
        assert_eq!(result.method, "none");
    }

    #[test]
    fn authorize_local_direct() {
        let resolved = ResolvedAuth {
            mode: AuthMode::Token,
            token: "secret".into(),
            password: String::new(),
            allow_tailscale: false,
        };
        let result = authorize(&resolved, "", "", "127.0.0.1", None);
        assert!(result.ok);
        assert_eq!(result.method, "local");
    }

    #[test]
    fn authorize_ipv6_loopback() {
        let resolved = ResolvedAuth {
            mode: AuthMode::Token,
            token: "secret".into(),
            password: String::new(),
            allow_tailscale: false,
        };
        let result = authorize(&resolved, "", "", "::1", None);
        assert!(result.ok);
    }

    #[test]
    fn authorize_valid_token() {
        let resolved = ResolvedAuth {
            mode: AuthMode::Token,
            token: "my-secret-token".into(),
            password: String::new(),
            allow_tailscale: false,
        };
        let result = authorize(&resolved, "my-secret-token", "", "10.0.0.1", None);
        assert!(result.ok);
        assert_eq!(result.method, "token");
    }

    #[test]
    fn authorize_invalid_token() {
        let resolved = ResolvedAuth {
            mode: AuthMode::Token,
            token: "my-secret-token".into(),
            password: String::new(),
            allow_tailscale: false,
        };
        let result = authorize(&resolved, "wrong-token", "", "10.0.0.1", None);
        assert!(!result.ok);
    }

    #[test]
    fn authorize_valid_password() {
        let resolved = ResolvedAuth {
            mode: AuthMode::Password,
            token: String::new(),
            password: "my-password".into(),
            allow_tailscale: false,
        };
        let result = authorize(&resolved, "", "my-password", "10.0.0.1", None);
        assert!(result.ok);
        assert_eq!(result.method, "password");
    }

    #[test]
    fn authorize_invalid_password() {
        let resolved = ResolvedAuth {
            mode: AuthMode::Password,
            token: String::new(),
            password: "my-password".into(),
            allow_tailscale: false,
        };
        let result = authorize(&resolved, "", "wrong", "10.0.0.1", None);
        assert!(!result.ok);
    }

    #[test]
    fn rate_limiter_lockout() {
        let rl = AuthRateLimiter::new(3, 60_000, 10_000);
        let ip = "1.2.3.4";

        rl.record_failure(ip);
        rl.record_failure(ip);
        rl.record_failure(ip); // triggers lockout

        let (allowed, retry_ms) = rl.check(ip);
        assert!(!allowed);
        assert!(retry_ms > 0);
    }

    #[test]
    fn rate_limiter_reset() {
        let rl = AuthRateLimiter::new(3, 60_000, 10_000);
        let ip = "1.2.3.4";

        rl.record_failure(ip);
        rl.record_failure(ip);
        rl.record_failure(ip);

        rl.reset(ip);
        let (allowed, _) = rl.check(ip);
        assert!(allowed);
    }

    #[test]
    fn rate_limiter_allowed_before_lockout() {
        let rl = AuthRateLimiter::new(5, 60_000, 10_000);
        let ip = "1.2.3.4";

        for _ in 0..4 {
            rl.record_failure(ip);
        }
        let (allowed, _) = rl.check(ip);
        assert!(allowed);
    }

    #[test]
    fn authorize_with_rate_limiter() {
        let resolved = ResolvedAuth {
            mode: AuthMode::Token,
            token: "secret".into(),
            password: String::new(),
            allow_tailscale: false,
        };
        let rl = AuthRateLimiter::new(2, 60_000, 10_000);

        let ip = "10.0.0.1";
        // Two failures should trigger lockout.
        authorize(&resolved, "wrong", "", ip, Some(&rl));
        authorize(&resolved, "wrong", "", ip, Some(&rl));

        let result = authorize(&resolved, "secret", "", ip, Some(&rl));
        assert!(!result.ok);
        assert!(result.rate_limited);
    }

    #[test]
    fn get_bearer_token_cases() {
        assert_eq!(get_bearer_token("Bearer my-token"), Some("my-token"));
        assert_eq!(get_bearer_token("bearer MY-TOKEN"), Some("MY-TOKEN"));
        assert_eq!(get_bearer_token("Basic abc123"), None);
        assert_eq!(get_bearer_token(""), None);
        assert_eq!(get_bearer_token("Bearer "), None);
    }

    #[test]
    fn remote_ip_cases() {
        assert_eq!(remote_ip("", "192.168.1.1:12345"), "192.168.1.1");
        assert_eq!(remote_ip("10.0.0.1, 192.168.1.1", ""), "10.0.0.1");
        assert_eq!(remote_ip("10.0.0.1", ""), "10.0.0.1");
    }

    #[test]
    fn constant_time_equal_cases() {
        assert!(constant_time_equal("hello", "hello"));
        assert!(!constant_time_equal("hello", "world"));
        assert!(!constant_time_equal("hello", "hell"));
    }

    #[test]
    fn test_is_loopback() {
        assert!(is_loopback("127.0.0.1"));
        assert!(is_loopback("::1"));
        assert!(!is_loopback("10.0.0.1"));
        assert!(!is_loopback("not-an-ip"));
    }
}
