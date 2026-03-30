//! Protocol constants mirrored from TypeScript sources.
//!
//! These values MUST stay in sync with their TypeScript counterparts.
//! A CI check (`scripts/check-protocol-constants.ts`) validates consistency.

// ---------------------------------------------------------------------------
// Gateway client IDs — src/gateway/protocol/client-info.ts
// ---------------------------------------------------------------------------

pub const GATEWAY_CLIENT_IDS: &[&str] = &[
    "deneb-control-ui",
    "cli",
    "gateway-client",
    "node-host",
    "test",
    "fingerprint",
    "deneb-probe",
];

// ---------------------------------------------------------------------------
// Gateway client modes — src/gateway/protocol/client-info.ts
// ---------------------------------------------------------------------------

pub const GATEWAY_CLIENT_MODES: &[&str] =
    &["cli", "ui", "backend", "node", "probe", "test"];

// ---------------------------------------------------------------------------
// Input provenance kinds — src/sessions/input-provenance.ts
// ---------------------------------------------------------------------------

pub const INPUT_PROVENANCE_KINDS: &[&str] = &["external_user", "inter_session", "internal_system"];

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

/// `SECRET_PROVIDER_ALIAS_PATTERN` — src/secrets/ref-contract.ts
pub const SECRET_PROVIDER_ALIAS_PATTERN: &str = r"^[a-z][a-z0-9_-]{0,63}$";

/// `ENV_SECRET_REF_ID_RE` — src/config/types.secrets.ts
pub const ENV_SECRET_REF_ID_PATTERN: &str = r"^[A-Z][A-Z0-9_]{0,127}$";

/// `FILE_SECRET_REF_ID_PATTERN` — src/secrets/ref-contract.ts
pub const FILE_SECRET_REF_ID_PATTERN: &str = r"^(?:value|/(?:[^~]|~0|~1)*(?:/(?:[^~]|~0|~1)*)*)$";

/// `EXEC_SECRET_REF_ID_JSON_SCHEMA_PATTERN` — src/secrets/ref-contract.ts
pub const EXEC_SECRET_REF_ID_PATTERN: &str =
    r"^(?!.*(?:^|/)\.{1,2}(?:/|$))[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$";

// ---------------------------------------------------------------------------
// Protocol version — src/gateway/protocol/schema/protocol-schemas.ts
// ---------------------------------------------------------------------------

pub const PROTOCOL_VERSION: u32 = 3;

// ---------------------------------------------------------------------------
// Network limits — mirrors gateway-go/pkg/protocol/constants.go
// ---------------------------------------------------------------------------

/// Maximum size of an authenticated message (25 MB).
pub const MAX_PAYLOAD_BYTES: usize = 25 * 1024 * 1024;

/// Per-connection send buffer limit (50 MB).
pub const MAX_BUFFERED_BYTES: usize = 50 * 1024 * 1024;

/// Maximum size of a pre-handshake message (64 KB).
pub const MAX_PRE_AUTH_PAYLOAD_BYTES: usize = 64 * 1024;

/// Default handshake timeout in milliseconds.
pub const HANDSHAKE_TIMEOUT_MS: u64 = 3_000;

/// Server heartbeat interval in milliseconds.
pub const TICK_INTERVAL_MS: u64 = 30_000;

/// Health snapshot refresh interval in milliseconds.
pub const HEALTH_REFRESH_INTERVAL_MS: u64 = 60_000;

/// Idempotency window in milliseconds (5 minutes).
pub const DEDUPE_TTL_MS: u64 = 5 * 60_000;

/// Maximum number of dedupe entries before cleanup.
pub const DEDUPE_MAX: usize = 1000;

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
        // EXEC pattern uses negative lookahead — requires fancy-regex.
        assert!(
            fancy_regex::Regex::new(EXEC_SECRET_REF_ID_PATTERN).is_ok(),
            "exec pattern should compile with fancy-regex"
        );
    }
}
