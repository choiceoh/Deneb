//! HMAC-SHA256 token signing, validation, and device management.
//!
//! Token format: `hex(hmac-sha256(payload, secret)):payload`
//! where payload is: `deviceId:role:scopes:issuedAtUnix`

use std::collections::HashMap;
use std::time::{SystemTime, UNIX_EPOCH};

use hmac::{Hmac, Mac};
use parking_lot::RwLock;
use sha2::Sha256;

use super::{Role, Scope};

type HmacSha256 = Hmac<Sha256>;

// ---------------------------------------------------------------------------
// TokenClaims
// ---------------------------------------------------------------------------

/// Claims extracted from a validated token.
#[derive(Debug, Clone)]
pub struct TokenClaims {
    pub device_id: String,
    pub role: Role,
    pub scopes: Vec<Scope>,
    pub issued_at: u64,   // unix seconds
    pub expires_at: u64,  // unix seconds; 0 = no expiry
}

impl TokenClaims {
    /// Whether the token has expired relative to `now_unix`.
    pub fn is_expired(&self, now_unix: u64) -> bool {
        if self.expires_at == 0 {
            return false;
        }
        now_unix > self.expires_at
    }

    /// Whether the claims include the given scope.
    pub fn has_scope(&self, scope: Scope) -> bool {
        self.scopes.contains(&scope)
    }
}

// ---------------------------------------------------------------------------
// DeviceRecord
// ---------------------------------------------------------------------------

/// A paired device record.
#[derive(Debug, Clone)]
pub struct DeviceRecord {
    pub id: String,
    pub public_key: String,
    pub name: String,
    pub paired_at: u64,  // unix seconds
    pub last_seen: u64,  // unix seconds
}

// ---------------------------------------------------------------------------
// Validator
// ---------------------------------------------------------------------------

/// HMAC-SHA256 token manager with concurrent device tracking.
pub struct Validator {
    secret: Vec<u8>,
    devices: RwLock<HashMap<String, DeviceRecord>>,
}

impl Validator {
    pub fn new(secret: &[u8]) -> Self {
        Self {
            secret: secret.to_vec(),
            devices: RwLock::new(HashMap::new()),
        }
    }

    /// Validate a token string and return the claims if valid.
    pub fn validate_token(&self, token: &str) -> Result<TokenClaims, String> {
        // HMAC hex is always 64 chars (sha256 = 32 bytes = 64 hex).
        if token.len() < 65 || token.as_bytes()[64] != b':' {
            return Err("invalid token format".into());
        }

        let sig = hex::decode(&token[..64]).map_err(|_| "invalid token signature encoding")?;
        let payload = &token[65..];
        let expected = self.compute_hmac(payload.as_bytes());

        if !constant_time_eq(&sig, &expected) {
            return Err("invalid token signature".into());
        }

        let claims = parse_payload(payload)?;

        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_err(|e| format!("system time error: {e}"))?
            .as_secs();

        if claims.is_expired(now) {
            return Err("token expired".into());
        }

        Ok(claims)
    }

    /// Issue a signed token for a device.
    pub fn issue_token(
        &self,
        device_id: &str,
        role: Role,
        scopes: &[Scope],
    ) -> Result<String, String> {
        if device_id.is_empty() {
            return Err("deviceID is required".into());
        }
        if device_id.contains(':') {
            return Err("deviceID must not contain ':'".into());
        }

        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_err(|e| format!("system time error: {e}"))?
            .as_secs();

        let scope_str = join_scopes(scopes);
        let payload = format!("{device_id}:{role}:{scope_str}:{now}");
        let sig = hex::encode(self.compute_hmac(payload.as_bytes()));
        Ok(format!("{sig}:{payload}"))
    }

    // -- Device management --------------------------------------------------

    pub fn register_device(&self, device: DeviceRecord) {
        self.devices.write().insert(device.id.clone(), device);
    }

    pub fn get_device(&self, id: &str) -> Option<DeviceRecord> {
        self.devices.read().get(id).cloned()
    }

    pub fn remove_device(&self, id: &str) -> bool {
        self.devices.write().remove(id).is_some()
    }

    pub fn list_devices(&self) -> Vec<DeviceRecord> {
        self.devices.read().values().cloned().collect()
    }

    pub fn touch_device(&self, id: &str) {
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();
        if let Some(d) = self.devices.write().get_mut(id) {
            d.last_seen = now;
        }
    }

    // -- Internal -----------------------------------------------------------

    fn compute_hmac(&self, data: &[u8]) -> Vec<u8> {
        // HMAC-SHA256 accepts any key length, so new_from_slice never fails.
        let Ok(mut mac) = HmacSha256::new_from_slice(&self.secret) else {
            return Vec::new();
        };
        mac.update(data);
        mac.finalize().into_bytes().to_vec()
    }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn parse_payload(payload: &str) -> Result<TokenClaims, String> {
    // payload: deviceId:role:scopes:issuedAtUnix
    let parts: Vec<&str> = payload.splitn(4, ':').collect();
    if parts.len() != 4 {
        return Err("invalid token payload".into());
    }

    let device_id = parts[0];
    if device_id.is_empty() {
        return Err("empty device ID in token".into());
    }

    let role =
        Role::from_str_exact(parts[1]).ok_or_else(|| format!("unknown role: {}", parts[1]))?;

    let scopes: Vec<Scope> = parts[2]
        .split(',')
        .filter_map(|s| {
            let s = s.trim();
            if s.is_empty() {
                None
            } else {
                Scope::from_str_exact(s)
            }
        })
        .collect();

    let issued_at: u64 = parts[3]
        .parse()
        .map_err(|_| format!("invalid issuedAt: {}", parts[3]))?;

    Ok(TokenClaims {
        device_id: device_id.to_string(),
        role,
        scopes,
        issued_at,
        expires_at: 0,
    })
}

fn join_scopes(scopes: &[Scope]) -> String {
    scopes
        .iter()
        .map(|s| s.as_str())
        .collect::<Vec<_>>()
        .join(",")
}

/// Constant-time byte comparison (prevents timing attacks on signatures).
fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for (x, y) in a.iter().zip(b.iter()) {
        diff |= x ^ y;
    }
    diff == 0
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn issue_and_validate_token() {
        let v = Validator::new(b"test-secret");

        let token = v
            .issue_token("device-1", Role::Operator, &[Scope::Read, Scope::Write])
            .expect("issue");
        assert!(!token.is_empty());

        let claims = v.validate_token(&token).expect("validate");
        assert_eq!(claims.device_id, "device-1");
        assert_eq!(claims.role, Role::Operator);
        assert_eq!(claims.scopes.len(), 2);
    }

    #[test]
    fn issue_token_empty_device_id() {
        let v = Validator::new(b"secret");
        assert!(v.issue_token("", Role::Operator, &[]).is_err());
    }

    #[test]
    fn issue_token_device_id_with_colon() {
        let v = Validator::new(b"secret");
        assert!(v.issue_token("device:bad", Role::Operator, &[]).is_err());
    }

    #[test]
    fn validate_token_invalid_signature() {
        let v = Validator::new(b"test-secret");
        let bad = "0000000000000000000000000000000000000000000000000000000000000000:device-1:operator:operator.read:12345";
        assert!(v.validate_token(bad).is_err());
    }

    #[test]
    fn validate_token_invalid_format() {
        let v = Validator::new(b"test-secret");
        assert!(v.validate_token("not-a-token").is_err());
    }

    #[test]
    fn validate_token_too_short() {
        let v = Validator::new(b"test-secret");
        assert!(v.validate_token("abcd:payload").is_err());
    }

    #[test]
    fn validate_token_different_secret() {
        let v1 = Validator::new(b"secret-1");
        let v2 = Validator::new(b"secret-2");

        let token = v1
            .issue_token("dev", Role::Viewer, &[Scope::Read])
            .expect("issue");
        assert!(v2.validate_token(&token).is_err());
    }

    #[test]
    fn token_claims_is_expired() {
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("time")
            .as_secs();

        let claims = TokenClaims {
            device_id: "d".into(),
            role: Role::Viewer,
            scopes: vec![],
            issued_at: now,
            expires_at: now.saturating_sub(3600), // 1 hour ago
        };
        assert!(claims.is_expired(now));

        let claims2 = TokenClaims {
            expires_at: now + 3600, // 1 hour from now
            ..claims.clone()
        };
        assert!(!claims2.is_expired(now));

        let claims3 = TokenClaims {
            expires_at: 0, // no expiry
            ..claims
        };
        assert!(!claims3.is_expired(now));
    }

    #[test]
    fn token_claims_has_scope() {
        let claims = TokenClaims {
            device_id: "d".into(),
            role: Role::Viewer,
            scopes: vec![Scope::Read, Scope::Write],
            issued_at: 0,
            expires_at: 0,
        };
        assert!(claims.has_scope(Scope::Read));
        assert!(!claims.has_scope(Scope::Admin));
    }

    #[test]
    fn device_management() {
        let v = Validator::new(b"secret");

        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("time")
            .as_secs();

        let dev = DeviceRecord {
            id: "dev-1".into(),
            public_key: String::new(),
            name: "Test Device".into(),
            paired_at: now,
            last_seen: now,
        };
        v.register_device(dev);

        let got = v.get_device("dev-1").expect("device");
        assert_eq!(got.name, "Test Device");

        let devices = v.list_devices();
        assert_eq!(devices.len(), 1);
        assert_eq!(devices[0].name, "Test Device");

        v.touch_device("dev-1");

        assert!(v.remove_device("dev-1"));
        assert!(v.get_device("dev-1").is_none());
    }
}
