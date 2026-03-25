//! Base64 validation and size estimation.
//!
//! Ports `src/media/base64.ts` — `estimateBase64DecodedBytes` and
//! `canonicalizeBase64`.

/// Estimate the number of decoded bytes from a base64 string without
/// allocating or decoding. Whitespace characters (ASCII ≤ 0x20) are
/// skipped; padding `=` is detected by scanning from the end.
pub fn estimate_base64_decoded_bytes(input: &str) -> usize {
    let bytes = input.as_bytes();
    let mut effective_len: usize = 0;

    for &b in bytes {
        if b <= 0x20 {
            continue;
        }
        effective_len += 1;
    }

    if effective_len == 0 {
        return 0;
    }

    // Find padding by scanning from the end, skipping whitespace.
    let mut padding: usize = 0;
    let mut end = bytes.len();
    loop {
        if end == 0 {
            break;
        }
        end -= 1;
        if bytes[end] <= 0x20 {
            continue;
        }
        if bytes[end] == b'=' {
            padding = 1;
            // Check for second padding.
            loop {
                if end == 0 {
                    break;
                }
                end -= 1;
                if bytes[end] <= 0x20 {
                    continue;
                }
                if bytes[end] == b'=' {
                    padding = 2;
                }
                break;
            }
        }
        break;
    }

    let estimated = (effective_len * 3) / 4;
    estimated.saturating_sub(padding)
}

/// Validate and canonicalize a base64 string.
///
/// Strips whitespace, validates that:
/// - The result is non-empty
/// - Length is a multiple of 4
/// - All characters are `[A-Za-z0-9+/]` with up to 2 trailing `=`
///
/// Returns `Some(canonical)` on success, `None` on invalid input.
pub fn canonicalize_base64(input: &str) -> Option<String> {
    // Strip whitespace.
    let mut cleaned = String::with_capacity(input.len());
    for &b in input.as_bytes() {
        if b.is_ascii_whitespace() {
            continue;
        }
        cleaned.push(b as char);
    }

    if cleaned.is_empty() || !cleaned.len().is_multiple_of(4) {
        return None;
    }

    // Validate characters.
    let bytes = cleaned.as_bytes();
    let len = bytes.len();

    // Find where padding starts.
    let mut data_end = len;
    if data_end > 0 && bytes[data_end - 1] == b'=' {
        data_end -= 1;
        if data_end > 0 && bytes[data_end - 1] == b'=' {
            data_end -= 1;
        }
    }

    // Check that padding is at most 2.
    let padding_count = len - data_end;
    if padding_count > 2 {
        return None;
    }

    // Validate data characters.
    for &b in &bytes[..data_end] {
        if !is_base64_char(b) {
            return None;
        }
    }

    // Validate that padding chars are only '='.
    for &b in &bytes[data_end..] {
        if b != b'=' {
            return None;
        }
    }

    Some(cleaned)
}

#[inline]
fn is_base64_char(b: u8) -> bool {
    b.is_ascii_alphanumeric() || b == b'+' || b == b'/'
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn estimate_empty() {
        assert_eq!(estimate_base64_decoded_bytes(""), 0);
        assert_eq!(estimate_base64_decoded_bytes("   "), 0);
    }

    #[test]
    fn estimate_no_padding() {
        // "AAAA" = 4 base64 chars → 3 decoded bytes.
        assert_eq!(estimate_base64_decoded_bytes("AAAA"), 3);
    }

    #[test]
    fn estimate_single_padding() {
        // "AAA=" = 4 chars, 1 padding → 2 decoded bytes.
        assert_eq!(estimate_base64_decoded_bytes("AAA="), 2);
    }

    #[test]
    fn estimate_double_padding() {
        // "AA==" = 4 chars, 2 padding → 1 decoded byte.
        assert_eq!(estimate_base64_decoded_bytes("AA=="), 1);
    }

    #[test]
    fn estimate_with_whitespace() {
        assert_eq!(
            estimate_base64_decoded_bytes("  AA AA  "),
            estimate_base64_decoded_bytes("AAAA")
        );
    }

    #[test]
    fn canonicalize_valid() {
        assert_eq!(canonicalize_base64("AAAA").as_deref(), Some("AAAA"));
        assert_eq!(canonicalize_base64("AA==").as_deref(), Some("AA=="));
        assert_eq!(canonicalize_base64(" A A A A ").as_deref(), Some("AAAA"));
    }

    #[test]
    fn canonicalize_invalid_length() {
        assert!(canonicalize_base64("AAA").is_none()); // not multiple of 4
    }

    #[test]
    fn canonicalize_invalid_chars() {
        assert!(canonicalize_base64("A@A=").is_none());
    }

    #[test]
    fn canonicalize_empty() {
        assert!(canonicalize_base64("").is_none());
        assert!(canonicalize_base64("   ").is_none());
    }

    #[test]
    fn canonicalize_triple_padding() {
        assert!(canonicalize_base64("A===").is_none());
    }
}
