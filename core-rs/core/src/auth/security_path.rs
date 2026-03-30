//! Security path canonicalization (multi-pass URL decode).
//!
//! 1:1 port of `gateway-go/internal/auth/security_path.go`.

use std::collections::HashSet;

const MAX_PATH_DECODE_PASSES: usize = 32;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/// Result of path canonicalization for security checks.
#[derive(Debug, Clone)]
pub struct SecurityPathCanonicalization {
    /// Final fully-decoded and normalized path.
    pub canonical_path: String,
    /// All normalized intermediate paths (for security checks).
    pub candidates: Vec<String>,
    /// Number of successful decode iterations.
    pub decode_passes: usize,
    /// True if more decoding was possible beyond the limit.
    pub decode_pass_limit_reached: bool,
    /// True if decoding failed at some point.
    pub malformed_encoding: bool,
    /// Path with separators/dots normalized but not decoded.
    pub raw_normalized_path: String,
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/// Perform multi-pass URL decode with normalization.
/// Used for fail-closed security checks on request paths.
pub fn canonicalize_path_for_security(pathname: &str) -> SecurityPathCanonicalization {
    let raw_normalized = normalize_path(pathname);

    let mut candidates = Vec::new();
    let mut seen = HashSet::new();
    let mut current = pathname.to_string();
    let mut decode_passes = 0;
    let mut decode_pass_limit_reached = false;
    let mut malformed_encoding = false;

    for i in 0..MAX_PATH_DECODE_PASSES {
        let normalized = normalize_path(&current);
        if seen.insert(normalized.clone()) {
            candidates.push(normalized);
        }

        match percent_decode(&current) {
            Some(decoded) if decoded == current => {
                // No more decoding possible.
                break;
            }
            Some(decoded) => {
                current = decoded;
                decode_passes = i + 1;

                if i == MAX_PATH_DECODE_PASSES - 1 {
                    // Check if more decoding would be possible.
                    if let Some(next) = percent_decode(&current) {
                        if next != current {
                            decode_pass_limit_reached = true;
                        }
                    }
                }
            }
            None => {
                malformed_encoding = true;
                break;
            }
        }
    }

    let canonical_path = if let Some(last) = candidates.last() {
        last.clone()
    } else {
        normalize_path(pathname)
    };

    SecurityPathCanonicalization {
        canonical_path,
        candidates,
        decode_passes,
        decode_pass_limit_reached,
        malformed_encoding,
        raw_normalized_path: raw_normalized,
    }
}

/// Check if any canonicalized version of the path matches any protected prefix.
/// Fail-closed: returns true on edge cases (decode limit reached).
pub fn is_path_protected_by_prefixes(pathname: &str, prefixes: &[&str]) -> bool {
    if prefixes.is_empty() {
        return false;
    }

    let canon = canonicalize_path_for_security(pathname);

    // Fail-closed: if decode pass limit reached, assume protected.
    if canon.decode_pass_limit_reached {
        return true;
    }

    // Normalize prefixes.
    let normalized_prefixes: Vec<String> = prefixes.iter().map(|p| normalize_path(p)).collect();

    // Check all candidates against all prefixes.
    for candidate in &canon.candidates {
        for prefix in &normalized_prefixes {
            if matches_prefix(candidate, prefix) {
                return true;
            }
        }
    }

    // Also check raw normalized path.
    for prefix in &normalized_prefixes {
        if matches_prefix(&canon.raw_normalized_path, prefix) {
            return true;
        }
    }

    false
}

/// Check if a path targets protected plugin routes.
/// Protected prefixes: `["/api/channels"]`.
pub fn is_protected_plugin_route_path(pathname: &str) -> bool {
    is_path_protected_by_prefixes(pathname, &["/api/channels"])
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

/// Lowercase, collapse multiple slashes, remove trailing slash, resolve dot segments.
fn normalize_path(p: &str) -> String {
    let mut s = p.to_lowercase();

    // Collapse multiple slashes.
    while s.contains("//") {
        s = s.replace("//", "/");
    }

    // Remove trailing slash (except root).
    if s.len() > 1 && s.ends_with('/') {
        s.pop();
    }

    // Resolve dot segments (simplified path.Clean equivalent).
    resolve_dot_segments(&s)
}

/// Resolve `.` and `..` segments in a path.
fn resolve_dot_segments(path: &str) -> String {
    if path.is_empty() {
        return "/".to_string();
    }

    let mut segments: Vec<&str> = Vec::new();
    let is_absolute = path.starts_with('/');

    for seg in path.split('/') {
        match seg {
            "" | "." => {}
            ".." => {
                segments.pop();
            }
            _ => segments.push(seg),
        }
    }

    let joined = segments.join("/");
    if is_absolute {
        format!("/{joined}")
    } else if joined.is_empty() {
        "/".to_string()
    } else {
        joined
    }
}

/// Check if a path matches a prefix exactly or starts with prefix/.
fn matches_prefix(pathname: &str, prefix: &str) -> bool {
    if pathname == prefix {
        return true;
    }
    if pathname.starts_with(&format!("{prefix}/")) {
        return true;
    }
    // Fail-closed: check for encoded separator after prefix.
    if pathname.starts_with(&format!("{prefix}%")) {
        return true;
    }
    false
}

/// Single-pass percent decode. Returns `None` on malformed encoding.
fn percent_decode(input: &str) -> Option<String> {
    let bytes = input.as_bytes();
    let mut result = Vec::with_capacity(bytes.len());
    let mut i = 0;

    while i < bytes.len() {
        if bytes[i] == b'%' && i + 2 < bytes.len() {
            let hi = hex_val(bytes[i + 1])?;
            let lo = hex_val(bytes[i + 2])?;
            result.push(hi << 4 | lo);
            i += 3;
        } else {
            result.push(bytes[i]);
            i += 1;
        }
    }

    String::from_utf8(result).ok()
}

fn hex_val(b: u8) -> Option<u8> {
    match b {
        b'0'..=b'9' => Some(b - b'0'),
        b'a'..=b'f' => Some(b - b'a' + 10),
        b'A'..=b'F' => Some(b - b'A' + 10),
        _ => None,
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn canonicalize_path_cases() {
        let cases = [
            ("/", "/"),
            ("/api/v1", "/api/v1"),
            ("/api/v1/", "/api/v1"),
            ("//api//v1", "/api/v1"),
            ("/api/../v1", "/v1"),
            ("/API/V1", "/api/v1"),
            ("/api%2Fv1", "/api/v1"),
            ("/api%252Fv1", "/api/v1"),
        ];
        for (input, want) in cases {
            let result = canonicalize_path_for_security(input);
            assert_eq!(
                result.canonical_path, want,
                "CanonicalPath for {input:?}"
            );
        }
    }

    #[test]
    fn is_path_protected_by_prefixes_cases() {
        let prefixes = &["/api/channels"];
        let cases = [
            ("/api/channels", true),
            ("/api/channels/telegram", true),
            ("/api/v1/rpc", false),
            ("/API/CHANNELS", true),
            ("/api%2Fchannels", true),
            ("%2Fapi%2Fchannels", true),
            ("/api/foo/../channels", true),
            ("/health", false),
        ];
        for (path, want) in cases {
            let got = is_path_protected_by_prefixes(path, prefixes);
            assert_eq!(got, want, "IsPathProtectedByPrefixes({path:?})");
        }
    }

    #[test]
    fn is_protected_plugin_route_path_cases() {
        assert!(is_protected_plugin_route_path("/api/channels/telegram"));
        assert!(!is_protected_plugin_route_path("/health"));
    }
}
