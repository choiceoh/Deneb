//! Authentication, device pairing, and RBAC for the gateway.
//!
//! 1:1 port of `gateway-go/internal/auth/`. Provides:
//! - HMAC-SHA256 token signing and validation
//! - Role-based access control with fine-grained scopes
//! - Device registration and tracking
//! - Auth middleware (authorize, rate limiting, loopback bypass)
//! - Credential resolution from env/config
//! - Browser origin validation
//! - Security path canonicalization (multi-pass URL decode)
//! - Hostname allowlist normalization

mod allowlist;
mod credentials;
mod middleware;
mod origin;
mod security_path;
mod token;

pub use allowlist::*;
pub use credentials::*;
pub use middleware::*;
pub use origin::*;
pub use security_path::*;
pub use token::*;

// ---------------------------------------------------------------------------
// Roles
// ---------------------------------------------------------------------------

/// Client role in the gateway RBAC system.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Role {
    Operator,
    Viewer,
    Agent,
    Probe,
}

impl Role {
    /// Parse a role string (case-sensitive, lowercase expected).
    pub fn from_str_exact(s: &str) -> Option<Self> {
        match s {
            "operator" => Some(Self::Operator),
            "viewer" => Some(Self::Viewer),
            "agent" => Some(Self::Agent),
            "probe" => Some(Self::Probe),
            _ => None,
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::Operator => "operator",
            Self::Viewer => "viewer",
            Self::Agent => "agent",
            Self::Probe => "probe",
        }
    }
}

impl std::fmt::Display for Role {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

// ---------------------------------------------------------------------------
// Scopes
// ---------------------------------------------------------------------------

/// Permission scope granted to a client.
/// Matches the `OperatorScope` values in the TypeScript codebase.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Scope {
    Admin,
    Read,
    Write,
    Approvals,
    Pairing,
}

impl Scope {
    pub fn from_str_exact(s: &str) -> Option<Self> {
        match s {
            "operator.admin" => Some(Self::Admin),
            "operator.read" => Some(Self::Read),
            "operator.write" => Some(Self::Write),
            "operator.approvals" => Some(Self::Approvals),
            "operator.pairing" => Some(Self::Pairing),
            _ => None,
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::Admin => "operator.admin",
            Self::Read => "operator.read",
            Self::Write => "operator.write",
            Self::Approvals => "operator.approvals",
            Self::Pairing => "operator.pairing",
        }
    }
}

impl std::fmt::Display for Scope {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

// ---------------------------------------------------------------------------
// Role -> Default Scopes mapping
// ---------------------------------------------------------------------------

/// Default scopes for a given role.
pub fn default_scopes(role: Role) -> &'static [Scope] {
    match role {
        Role::Operator => &[
            Scope::Admin,
            Scope::Read,
            Scope::Write,
            Scope::Approvals,
            Scope::Pairing,
        ],
        Role::Viewer => &[Scope::Read],
        Role::Agent => &[Scope::Read, Scope::Write],
        Role::Probe => &[Scope::Read],
    }
}

/// Default scopes as string slices.
pub fn default_scopes_strings(role: Role) -> Vec<&'static str> {
    default_scopes(role).iter().map(|s| s.as_str()).collect()
}

// ---------------------------------------------------------------------------
// Permission check
// ---------------------------------------------------------------------------

/// Check that a role+scopes combination grants the required scope.
/// Returns `Ok(())` on success, `Err(message)` on denial.
pub fn check_permission(role: Role, scopes: &[Scope], required: Scope) -> Result<(), String> {
    // Check explicit scopes first.
    for s in scopes {
        if *s == required || *s == Scope::Admin {
            return Ok(());
        }
    }
    // Fallback: check role defaults.
    for s in default_scopes(role) {
        if *s == required || *s == Scope::Admin {
            return Ok(());
        }
    }
    Err(format!("role \"{role}\" lacks scope \"{required}\""))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn role_roundtrip() {
        for role in [Role::Operator, Role::Viewer, Role::Agent, Role::Probe] {
            assert_eq!(Role::from_str_exact(role.as_str()), Some(role));
        }
        assert_eq!(Role::from_str_exact("unknown"), None);
    }

    #[test]
    fn scope_roundtrip() {
        for scope in [
            Scope::Admin,
            Scope::Read,
            Scope::Write,
            Scope::Approvals,
            Scope::Pairing,
        ] {
            assert_eq!(Scope::from_str_exact(scope.as_str()), Some(scope));
        }
        assert_eq!(Scope::from_str_exact("unknown"), None);
    }

    #[test]
    fn default_scopes_operator() {
        let scopes = default_scopes(Role::Operator);
        assert_eq!(scopes.len(), 5);
        assert!(scopes.contains(&Scope::Admin));
        assert!(scopes.contains(&Scope::Pairing));
    }

    #[test]
    fn default_scopes_viewer() {
        let scopes = default_scopes(Role::Viewer);
        assert_eq!(scopes, &[Scope::Read]);
    }

    #[test]
    fn default_scopes_strings_match() {
        let strs = default_scopes_strings(Role::Operator);
        assert_eq!(strs.len(), default_scopes(Role::Operator).len());
    }

    #[test]
    fn check_permission_operator_has_write() {
        assert!(check_permission(Role::Operator, &[], Scope::Write).is_ok());
    }

    #[test]
    fn check_permission_viewer_lacks_write() {
        assert!(check_permission(Role::Viewer, &[], Scope::Write).is_err());
    }

    #[test]
    fn check_permission_explicit_scope_override() {
        assert!(check_permission(Role::Viewer, &[Scope::Write], Scope::Write).is_ok());
    }

    #[test]
    fn check_permission_admin_grants_everything() {
        assert!(check_permission(Role::Viewer, &[Scope::Admin], Scope::Approvals).is_ok());
    }

    #[test]
    fn check_permission_operator_has_pairing() {
        assert!(check_permission(Role::Operator, &[], Scope::Pairing).is_ok());
    }

    #[test]
    fn check_permission_agent_lacks_admin() {
        assert!(check_permission(Role::Agent, &[], Scope::Admin).is_err());
    }
}
