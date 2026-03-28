//! Agent ID, account ID, and main key normalization.
//!
//! Mirrors `src/agents/agent-scope.ts` and `src/routing/account-id.ts` (pure-logic subset).
//! Keep in sync.

use once_cell::sync::Lazy;
use regex::Regex;

/// Default agent identifier when none is configured.
pub const DEFAULT_AGENT_ID: &str = "main";

/// Default main session key.
pub const DEFAULT_MAIN_KEY: &str = "main";

/// Default account identifier.
pub const DEFAULT_ACCOUNT_ID: &str = "default";

// Pre-compiled regexes for agent ID normalization.
pub(super) static VALID_ID_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$").expect("valid regex"));
pub(super) static INVALID_CHARS_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"[^a-z0-9_-]+").expect("valid regex"));
pub(super) static LEADING_DASH_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"^-+").expect("valid regex"));
pub(super) static TRAILING_DASH_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"-+$").expect("valid regex"));

static VALID_ACCOUNT_ID_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$").expect("valid regex"));

/// Normalize an agent ID to a path-safe, shell-friendly form.
pub fn normalize_agent_id(value: &str) -> String {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return DEFAULT_AGENT_ID.to_string();
    }
    if VALID_ID_RE.is_match(trimmed) {
        return trimmed.to_lowercase();
    }
    // Best-effort fallback: collapse invalid characters to "-".
    let lowered = trimmed.to_lowercase();
    let collapsed = INVALID_CHARS_RE.replace_all(&lowered, "-");
    let no_leading = LEADING_DASH_RE.replace(&collapsed, "");
    let no_trailing = TRAILING_DASH_RE.replace(&no_leading, "");
    let result = if no_trailing.len() > 64 {
        &no_trailing[..64]
    } else {
        &no_trailing
    };
    if result.is_empty() {
        DEFAULT_AGENT_ID.to_string()
    } else {
        result.to_string()
    }
}

/// Normalize a main key to lowercase with default fallback.
pub fn normalize_main_key(value: &str) -> String {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        DEFAULT_MAIN_KEY.to_string()
    } else {
        trimmed.to_lowercase()
    }
}

/// Check if an agent ID is syntactically valid.
pub fn is_valid_agent_id(value: &str) -> bool {
    let trimmed = value.trim();
    !trimmed.is_empty() && VALID_ID_RE.is_match(trimmed)
}

/// Sanitize an agent ID (alias for normalize_agent_id).
pub fn sanitize_agent_id(value: &str) -> String {
    normalize_agent_id(value)
}

/// Normalize an account ID to a path-safe form. Defaults to DEFAULT_ACCOUNT_ID.
/// Mirrors `src/routing/account-id.ts#normalizeAccountId`. Keep in sync.
pub fn normalize_account_id(value: &str) -> String {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return DEFAULT_ACCOUNT_ID.to_string();
    }
    if VALID_ACCOUNT_ID_RE.is_match(trimmed) {
        return trimmed.to_lowercase();
    }
    let lowered = trimmed.to_lowercase();
    let collapsed = INVALID_CHARS_RE.replace_all(&lowered, "-");
    let no_leading = LEADING_DASH_RE.replace(&collapsed, "");
    let no_trailing = TRAILING_DASH_RE.replace(&no_leading, "");
    let result = if no_trailing.len() > 64 {
        &no_trailing[..64]
    } else {
        &no_trailing
    };
    if result.is_empty() {
        DEFAULT_ACCOUNT_ID.to_string()
    } else {
        result.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalize_agent_id_basic() {
        assert_eq!(normalize_agent_id("main"), "main");
        assert_eq!(normalize_agent_id("  Main  "), "main");
        assert_eq!(normalize_agent_id(""), DEFAULT_AGENT_ID);
        assert_eq!(normalize_agent_id("my-agent_1"), "my-agent_1");
    }

    #[test]
    fn normalize_agent_id_invalid_chars() {
        assert_eq!(normalize_agent_id("my agent!"), "my-agent");
        assert_eq!(normalize_agent_id("---"), DEFAULT_AGENT_ID);
    }

    #[test]
    fn normalize_main_key_basic() {
        assert_eq!(normalize_main_key(""), DEFAULT_MAIN_KEY);
        assert_eq!(normalize_main_key("  Custom  "), "custom");
    }

    #[test]
    fn is_valid_agent_id_basic() {
        assert!(is_valid_agent_id("main"));
        assert!(is_valid_agent_id("my-agent_1"));
        assert!(!is_valid_agent_id(""));
        assert!(!is_valid_agent_id("invalid chars!"));
    }

    #[test]
    fn normalize_account_id_basic() {
        assert_eq!(normalize_account_id(""), DEFAULT_ACCOUNT_ID);
        assert_eq!(normalize_account_id("MyAccount"), "myaccount");
        assert_eq!(normalize_account_id("account-123"), "account-123");
    }
}
