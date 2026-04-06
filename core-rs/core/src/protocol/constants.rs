//! Protocol constants mirrored from TypeScript sources.
//!
//! These values MUST stay in sync with their TypeScript counterparts.
//! A CI check (`scripts/check-protocol-constants.ts`) validates consistency.

// ---------------------------------------------------------------------------
// Session label — src/sessions/session-label.ts
// ---------------------------------------------------------------------------

pub const SESSION_LABEL_MAX_LENGTH: usize = 512;

// ---------------------------------------------------------------------------
// Chat send session key — src/gateway/protocol/schema/primitives.ts
// ---------------------------------------------------------------------------

pub const CHAT_SEND_SESSION_KEY_MAX_LENGTH: usize = 512;

// ---------------------------------------------------------------------------
// Secret ref patterns — src/secrets/ref-contract.ts, src/config/types.secrets.ts
// ---------------------------------------------------------------------------

/// `EXEC_SECRET_REF_ID_JSON_SCHEMA_PATTERN` — src/secrets/ref-contract.ts
pub const EXEC_SECRET_REF_ID_PATTERN: &str =
    r"^(?!.*(?:^|/)\.{1,2}(?:/|$))[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$";

// ---------------------------------------------------------------------------
// Test-only constants: TypeScript parity checking
// These are only used in tests to verify sync with TypeScript counterparts.
// ---------------------------------------------------------------------------

#[cfg(test)]
pub const GATEWAY_CLIENT_IDS: &[&str] = &[
    "deneb-control-ui",
    "cli",
    "gateway-client",
    "node-host",
    "test",
    "fingerprint",
    "deneb-probe",
];

#[cfg(test)]
pub const GATEWAY_CLIENT_MODES: &[&str] = &["cli", "ui", "backend", "node", "probe", "test"];

#[cfg(test)]
pub const INPUT_PROVENANCE_KINDS: &[&str] = &["external_user", "inter_session", "internal_system"];

#[cfg(test)]
pub const SECRET_PROVIDER_ALIAS_PATTERN: &str = r"^[a-z][a-z0-9_-]{0,63}$";

#[cfg(test)]
pub const ENV_SECRET_REF_ID_PATTERN: &str = r"^[A-Z][A-Z0-9_]{0,127}$";

#[cfg(test)]
pub const FILE_SECRET_REF_ID_PATTERN: &str = r"^(?:value|/(?:[^~]|~0|~1)*(?:/(?:[^~]|~0|~1)*)*)$";

#[cfg(test)]
pub const PROTOCOL_VERSION: u32 = 3;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_client_ids_non_empty() {
        assert!(!GATEWAY_CLIENT_IDS.is_empty());
        for id in GATEWAY_CLIENT_IDS {
            assert!(!id.is_empty(), "client ID must not be empty");
        }
    }

    #[test]
    fn test_client_modes_non_empty() {
        assert!(!GATEWAY_CLIENT_MODES.is_empty());
        for mode in GATEWAY_CLIENT_MODES {
            assert!(!mode.is_empty(), "client mode must not be empty");
        }
    }

    #[test]
    fn test_provenance_kinds_non_empty() {
        assert!(!INPUT_PROVENANCE_KINDS.is_empty());
    }

    #[test]
    fn test_regex_patterns_compile() {
        use regex::Regex;
        // Standard regex patterns (no lookahead).
        let std_patterns = [
            SECRET_PROVIDER_ALIAS_PATTERN,
            ENV_SECRET_REF_ID_PATTERN,
            FILE_SECRET_REF_ID_PATTERN,
        ];
        for p in std_patterns {
            assert!(Regex::new(p).is_ok(), "pattern should compile: {p}");
        }
        // EXEC pattern validation — uses pure Rust function (no fancy-regex).
        use super::super::validation::is_valid_exec_secret_ref_id;
        // Valid cases
        assert!(is_valid_exec_secret_ref_id("a"));
        assert!(is_valid_exec_secret_ref_id("A0"));
        assert!(is_valid_exec_secret_ref_id("cmd/run"));
        assert!(is_valid_exec_secret_ref_id("my.secret:v1/path-name"));
        // Invalid cases
        assert!(!is_valid_exec_secret_ref_id("")); // empty
        assert!(!is_valid_exec_secret_ref_id(".hidden")); // starts with dot
        assert!(!is_valid_exec_secret_ref_id("a/../b")); // path traversal
        assert!(!is_valid_exec_secret_ref_id("a/./b")); // dot segment
        assert!(!is_valid_exec_secret_ref_id("a/b/..")); // trailing ..
        assert!(!is_valid_exec_secret_ref_id("../a")); // leading ..
    }
}
