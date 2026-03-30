//! Hostname allowlist normalization.
//!
//! 1:1 port of `gateway-go/internal/auth/allowlist.go`.

/// Normalize a hostname allowlist for security validation.
/// Trims whitespace and filters empty entries. Returns `None` if all entries are empty.
pub fn normalize_input_hostname_allowlist(values: &[String]) -> Option<Vec<String>> {
    if values.is_empty() {
        return None;
    }
    let result: Vec<String> = values
        .iter()
        .map(|v| v.trim().to_string())
        .filter(|v| !v.is_empty())
        .collect();
    if result.is_empty() {
        None
    } else {
        Some(result)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalize_allowlist_cases() {
        // nil/empty input
        assert_eq!(normalize_input_hostname_allowlist(&[]), None);

        // whitespace only
        assert_eq!(
            normalize_input_hostname_allowlist(&[
                "  ".into(),
                "\t".into(),
                String::new(),
            ]),
            None
        );

        // trims entries
        assert_eq!(
            normalize_input_hostname_allowlist(&[
                "  example.com  ".into(),
                "test.io".into(),
            ]),
            Some(vec!["example.com".into(), "test.io".into()])
        );

        // filters empty
        assert_eq!(
            normalize_input_hostname_allowlist(&[
                "example.com".into(),
                String::new(),
                "test.io".into(),
            ]),
            Some(vec!["example.com".into(), "test.io".into()])
        );

        // single valid
        assert_eq!(
            normalize_input_hostname_allowlist(&["example.com".into()]),
            Some(vec!["example.com".into()])
        );
    }
}
