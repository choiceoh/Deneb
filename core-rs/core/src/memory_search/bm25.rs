//! BM25 rank-to-score conversion for SQLite FTS5 results.

/// Convert a SQLite BM25 rank value to a [0, 1] score.
///
/// SQLite's `bm25()` returns negative values where more negative = more relevant.
/// Positive values indicate lower relevance.
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_negative_rank() {
        // BM25 returns negative for relevant docs
        let score = bm25_rank_to_score(-10.0);
        assert!((score - (10.0 / 11.0)).abs() < 1e-10);
    }

    #[test]
    fn test_zero_rank() {
        assert!((bm25_rank_to_score(0.0) - 1.0).abs() < 1e-10);
    }

    #[test]
    fn test_positive_rank() {
        let score = bm25_rank_to_score(1.0);
        assert!((score - 0.5).abs() < 1e-10);
    }

    #[test]
    fn test_nan() {
        let score = bm25_rank_to_score(f64::NAN);
        assert!((score - 1.0 / 1000.0).abs() < 1e-10);
    }

    #[test]
    fn test_infinity() {
        let score = bm25_rank_to_score(f64::INFINITY);
        assert!((score - 1.0 / 1000.0).abs() < 1e-10);
    }

    #[test]
    fn test_neg_infinity() {
        let score = bm25_rank_to_score(f64::NEG_INFINITY);
        assert!((score - 1.0 / 1000.0).abs() < 1e-10);
    }
}
