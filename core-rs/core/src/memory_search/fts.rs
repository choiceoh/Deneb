//! Full-text search query builder for `SQLite` FTS5.

use regex::Regex;
use std::sync::LazyLock;

#[allow(clippy::expect_used)]
static FTS_TOKEN_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[\p{L}\p{N}_]+").expect("valid regex"));

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
            format!("\"{cleaned}\"")
        })
        .collect();

    Some(quoted.join(" AND "))
}
