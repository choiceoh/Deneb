//! HTML entity decoding.
//!
//! Decodes named entities (`&amp;`), decimal numeric (`&#65;`),
//! and hex numeric (`&#x41;`) to their character equivalents.

/// Try to decode a single HTML entity starting at `pos` in `input`.
///
/// Returns `Some((decoded_char, bytes_consumed))` on success.
pub(crate) fn try_decode_entity(input: &str, pos: usize) -> Option<(char, usize)> {
    let rest = input.get(pos..)?;

    // Named entities (case-insensitive).
    let named: &[(&str, char)] = &[
        ("&nbsp;", '\u{00A0}'),
        ("&amp;", '&'),
        ("&quot;", '"'),
        ("&lt;", '<'),
        ("&gt;", '>'),
        ("&#39;", '\''),
        ("&apos;", '\''),
        ("&mdash;", '—'),
        ("&ndash;", '–'),
        ("&hellip;", '…'),
        ("&laquo;", '«'),
        ("&raquo;", '»'),
        ("&copy;", '©'),
        ("&reg;", '®'),
        ("&trade;", '™'),
        ("&bull;", '•'),
        ("&middot;", '·'),
    ];

    // Only lowercase a small bounded prefix — never the entire remaining input.
    // Find the nearest valid char boundary at or before byte 10.
    let prefix_end = bounded_char_boundary(rest, 10);
    let rest_lower = rest.get(..prefix_end)?.to_ascii_lowercase();

    for &(entity, ch) in named {
        if rest_lower.starts_with(entity) {
            return Some((ch, entity.len()));
        }
    }

    // Hex numeric: &#xHH; — cap search to first 12 bytes (covers realistic entities).
    if rest_lower.starts_with("&#x") {
        let after = rest.get(3..)?;
        // Only search for ';' within a reasonable range to avoid scanning megabytes.
        let search_limit = bounded_char_boundary(after, 12);
        if let Some(semi) = after.get(..search_limit)?.find(';') {
            let hex_str = after.get(..semi)?;
            if let Ok(code) = u32::from_str_radix(hex_str, 16) {
                if let Some(ch) = char::from_u32(code) {
                    return Some((ch, 3 + semi + 1));
                }
            }
        }
        return None;
    }

    // Decimal numeric: &#DDD;
    if rest_lower.starts_with("&#") {
        let after = rest.get(2..)?;
        let search_limit = bounded_char_boundary(after, 12);
        if let Some(semi) = after.get(..search_limit)?.find(';') {
            let dec_str = after.get(..semi)?;
            if let Ok(code) = dec_str.parse::<u32>() {
                if let Some(ch) = char::from_u32(code) {
                    return Some((ch, 2 + semi + 1));
                }
            }
        }
    }

    None
}

/// Find the largest valid char boundary at or before `max_byte`.
/// Returns 0 if the string is empty.
pub(crate) fn bounded_char_boundary(s: &str, max_byte: usize) -> usize {
    let mut end = max_byte.min(s.len());
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    end
}
