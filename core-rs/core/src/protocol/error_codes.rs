//! Gateway error codes — mirrors `src/gateway/protocol/schema/error-codes.ts`.

/// Gateway RPC error codes.
///
/// Each variant maps to a stable string code used on the wire.
/// The i32 discriminant provides a compact representation for FFI.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
#[repr(i32)]
pub enum ErrorCode {
    // Legacy codes (backward compatible)
    NotLinked = 1,
    NotPaired = 2,
    AgentTimeout = 3,
    InvalidRequest = 4,
    Unavailable = 5,

    // INVALID_REQUEST refinements
    MissingParam = 10,
    NotFound = 11,
    Unauthorized = 12,
    ValidationFailed = 13,
    Conflict = 14,
    Forbidden = 15,

    // UNAVAILABLE refinements
    NodeDisconnected = 20,
    DependencyFailed = 21,
    FeatureDisabled = 22,
}

impl ErrorCode {
    /// Wire-format string code (matches TypeScript `ErrorCodes` object keys).
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::NotLinked => "NOT_LINKED",
            Self::NotPaired => "NOT_PAIRED",
            Self::AgentTimeout => "AGENT_TIMEOUT",
            Self::InvalidRequest => "INVALID_REQUEST",
            Self::Unavailable => "UNAVAILABLE",
            Self::MissingParam => "MISSING_PARAM",
            Self::NotFound => "NOT_FOUND",
            Self::Unauthorized => "UNAUTHORIZED",
            Self::ValidationFailed => "VALIDATION_FAILED",
            Self::Conflict => "CONFLICT",
            Self::Forbidden => "FORBIDDEN",
            Self::NodeDisconnected => "NODE_DISCONNECTED",
            Self::DependencyFailed => "DEPENDENCY_FAILED",
            Self::FeatureDisabled => "FEATURE_DISABLED",
        }
    }

    /// Parse a wire-format string into an `ErrorCode`.
    pub fn parse(s: &str) -> Option<Self> {
        match s {
            "NOT_LINKED" => Some(Self::NotLinked),
            "NOT_PAIRED" => Some(Self::NotPaired),
            "AGENT_TIMEOUT" => Some(Self::AgentTimeout),
            "INVALID_REQUEST" => Some(Self::InvalidRequest),
            "UNAVAILABLE" => Some(Self::Unavailable),
            "MISSING_PARAM" => Some(Self::MissingParam),
            "NOT_FOUND" => Some(Self::NotFound),
            "UNAUTHORIZED" => Some(Self::Unauthorized),
            "VALIDATION_FAILED" => Some(Self::ValidationFailed),
            "CONFLICT" => Some(Self::Conflict),
            "FORBIDDEN" => Some(Self::Forbidden),
            "NODE_DISCONNECTED" => Some(Self::NodeDisconnected),
            "DEPENDENCY_FAILED" => Some(Self::DependencyFailed),
            "FEATURE_DISABLED" => Some(Self::FeatureDisabled),
            _ => None,
        }
    }

    /// Whether this code is retryable by default.
    pub fn is_retryable(&self) -> bool {
        matches!(
            self,
            Self::AgentTimeout
                | Self::Unavailable
                | Self::NodeDisconnected
                | Self::DependencyFailed
        )
    }
}

impl std::fmt::Display for ErrorCode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// All known error codes in declaration order.
pub const ALL_ERROR_CODES: &[ErrorCode] = &[
    ErrorCode::NotLinked,
    ErrorCode::NotPaired,
    ErrorCode::AgentTimeout,
    ErrorCode::InvalidRequest,
    ErrorCode::Unavailable,
    ErrorCode::MissingParam,
    ErrorCode::NotFound,
    ErrorCode::Unauthorized,
    ErrorCode::ValidationFailed,
    ErrorCode::Conflict,
    ErrorCode::Forbidden,
    ErrorCode::NodeDisconnected,
    ErrorCode::DependencyFailed,
    ErrorCode::FeatureDisabled,
];

/// Validate that an error code string is a known code.
pub fn is_valid_error_code(code: &str) -> bool {
    ErrorCode::parse(code).is_some()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_roundtrip() {
        for code in ALL_ERROR_CODES {
            let s = code.as_str();
            let parsed = ErrorCode::parse(s).expect("known code should round-trip through parse");
            assert_eq!(*code, parsed);
        }
    }

    #[test]
    fn test_unknown_code() {
        assert!(ErrorCode::parse("UNKNOWN_CODE").is_none());
        assert!(!is_valid_error_code("BOGUS"));
    }

    #[test]
    fn test_retryable() {
        assert!(ErrorCode::AgentTimeout.is_retryable());
        assert!(ErrorCode::NodeDisconnected.is_retryable());
        assert!(!ErrorCode::NotFound.is_retryable());
        assert!(!ErrorCode::Forbidden.is_retryable());
    }

    #[test]
    fn test_discriminants_unique() {
        let mut seen = std::collections::HashSet::new();
        for code in ALL_ERROR_CODES {
            assert!(
                seen.insert(*code as i32),
                "duplicate discriminant for {code}"
            );
        }
    }

    #[test]
    fn test_display() {
        assert_eq!(format!("{}", ErrorCode::NotFound), "NOT_FOUND");
    }
}
