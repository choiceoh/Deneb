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
    // Covers HTML5 most-used named entities beyond the mandatory five.
    let named: &[(&str, char)] = &[
        // Mandatory five + common aliases
        ("&nbsp;", '\u{00A0}'),
        ("&amp;", '&'),
        ("&quot;", '"'),
        ("&lt;", '<'),
        ("&gt;", '>'),
        ("&#39;", '\''),
        ("&apos;", '\''),
        // Typography
        ("&mdash;", '\u{2014}'),  // —
        ("&ndash;", '\u{2013}'),  // –
        ("&hellip;", '\u{2026}'), // …
        ("&laquo;", '\u{00AB}'),  // «
        ("&raquo;", '\u{00BB}'),  // »
        ("&lsquo;", '\u{2018}'),  // '
        ("&rsquo;", '\u{2019}'),  // '
        ("&ldquo;", '\u{201C}'),  // "
        ("&rdquo;", '\u{201D}'),  // "
        ("&bull;", '\u{2022}'),   // •
        ("&middot;", '\u{00B7}'), // ·
        ("&ensp;", '\u{2002}'),   // en space
        ("&emsp;", '\u{2003}'),   // em space
        ("&thinsp;", '\u{2009}'), // thin space
        // Symbols
        ("&copy;", '\u{00A9}'),   // ©
        ("&reg;", '\u{00AE}'),    // ®
        ("&trade;", '\u{2122}'),  // ™
        ("&deg;", '\u{00B0}'),    // °
        ("&plusmn;", '\u{00B1}'), // ±
        ("&times;", '\u{00D7}'),  // ×
        ("&divide;", '\u{00F7}'), // ÷
        ("&micro;", '\u{00B5}'),  // µ
        // Currency
        ("&euro;", '\u{20AC}'),  // €
        ("&pound;", '\u{00A3}'), // £
        ("&yen;", '\u{00A5}'),   // ¥
        ("&cent;", '\u{00A2}'),  // ¢
        // Arrows
        ("&larr;", '\u{2190}'), // ←
        ("&rarr;", '\u{2192}'), // →
        ("&uarr;", '\u{2191}'), // ↑
        ("&darr;", '\u{2193}'), // ↓
        // Math
        ("&ne;", '\u{2260}'),    // ≠
        ("&le;", '\u{2264}'),    // ≤
        ("&ge;", '\u{2265}'),    // ≥
        ("&infin;", '\u{221E}'), // ∞
        // Misc
        ("&para;", '\u{00B6}'),   // ¶
        ("&sect;", '\u{00A7}'),   // §
        ("&dagger;", '\u{2020}'), // †
        ("&loz;", '\u{25CA}'),    // ◊
    ];

    // Only lowercase a small bounded prefix — never the entire remaining input.
    // Find the nearest valid char boundary. Use 12 to cover longer entity names
    // like "&dagger;" (8) or "&hellip;" (8) with room to spare.
    let prefix_end = bounded_char_boundary(rest, 12);
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
