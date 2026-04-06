//! BM25 rank-to-score conversion for `SQLite` FTS5 results.
//!
//! `SQLite` FTS5's `bm25()` function returns negative values where a more-negative
//! value indicates higher relevance. This module normalizes those raw ranks into
//! a [0, 1] score suitable for weighted fusion with vector similarity scores.

/// Convert a `SQLite` BM25 rank value to a [0, 1] score.
///
/// `SQLite`'s `bm25()` returns negative values where more negative = more relevant.
/// Positive values indicate lower relevance.
///
/// Formula:
/// - Negative rank (relevant): `|rank| / (1 + |rank|)` — approaches 1.0 for highly relevant docs.
/// - Zero/positive rank: `1 / (1 + rank)` — approaches 0.0 for irrelevant docs.
/// - Non-finite (NaN/Inf): returns a near-zero fallback score (1/1000).
pub fn bm25_rank_to_score(rank: f64) -> f64 {
    if !rank.is_finite() {
        return 1.0 / (1.0 + 999.0);
    }
    if rank < 0.0 {
        let relevance = -rank;
        relevance / (1.0 + relevance)
    } else {
        1.0 / (1.0 + rank)
    }
}
