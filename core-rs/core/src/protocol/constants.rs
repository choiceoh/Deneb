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
