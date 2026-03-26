//! Full-text search query builder for SQLite FTS5.

use once_cell::sync::Lazy;
use regex::Regex;

static FTS_TOKEN_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"[\p{L}\p{N}_]+").unwrap());

/// Build an FTS5 query from raw text.
///
/// Tokenizes on Unicode letter/number/underscore sequences, quotes each token,
/// and joins with AND. Returns `None` if no tokens are found.
pub fn build_fts_query(raw: &str) -> Option<String> {
    let tokens: Vec<&str> = FTS_TOKEN_RE
        .find_iter(raw)
        .map(|m| m.as_str().trim())
        .filter(|t| !t.is_empty())
        .collect();

    if tokens.is_empty() {
        return None;
    }

    let quoted: Vec<String> = tokens
        .iter()
        .map(|t| {
            let cleaned: String = t.chars().filter(|&c| c != '"').collect();
            format!("\"{}\"", cleaned)
        })
        .collect();

    Some(quoted.join(" AND "))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_simple_query() {
        assert_eq!(
            build_fts_query("hello world"),
            Some("\"hello\" AND \"world\"".to_string())
        );
    }

    #[test]
    fn test_empty_query() {
        assert_eq!(build_fts_query(""), None);
    }

    #[test]
    fn test_punctuation_only() {
        assert_eq!(build_fts_query("!@#$%"), None);
    }

    #[test]
    fn test_unicode_tokens() {
        let result = build_fts_query("你好 世界");
        assert_eq!(result, Some("\"你好\" AND \"世界\"".to_string()));
    }

    #[test]
    fn test_mixed_scripts() {
        let result = build_fts_query("API 설계");
        assert_eq!(result, Some("\"API\" AND \"설계\"".to_string()));
    }

    #[test]
    fn test_removes_quotes() {
        let result = build_fts_query(r#"hello "world""#);
        assert_eq!(result, Some("\"hello\" AND \"world\"".to_string()));
    }

    #[test]
    fn test_underscores_preserved() {
        let result = build_fts_query("my_var another_var");
        assert_eq!(result, Some("\"my_var\" AND \"another_var\"".to_string()));
    }
}
