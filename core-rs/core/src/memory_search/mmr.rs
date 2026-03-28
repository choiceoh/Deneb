//! Maximal Marginal Relevance (MMR) re-ranking for search result diversity.

use once_cell::sync::Lazy;
use rayon::prelude::*;
use regex::Regex;
use std::collections::HashSet;

use super::types::{MmrConfig, MmrItem};

static MMR_TOKEN_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"[a-z0-9_]+").expect("valid regex"));

/// Pre-tokenized text that avoids per-token heap allocations.
/// Stores a single lowercase copy and byte ranges into it.
pub struct TokenSet {
    lowered: String,
    ranges: Vec<(usize, usize)>,
}

impl TokenSet {
    /// Tokenize text: lowercase once, collect match byte ranges.
    pub fn new(text: &str) -> Self {
        let lowered = text.to_lowercase();
        let ranges: Vec<(usize, usize)> = MMR_TOKEN_RE
            .find_iter(&lowered)
            .map(|m| (m.start(), m.end()))
            .collect();
        Self { lowered, ranges }
    }

    /// Iterate over token slices (borrows from the internal lowercase string).
    fn tokens(&self) -> impl Iterator<Item = &str> {
        self.ranges.iter().map(|&(s, e)| &self.lowered[s..e])
    }

    /// Build a borrowed `HashSet` for Jaccard computation.
    fn as_set(&self) -> HashSet<&str> {
        self.tokens().collect()
    }

}

#[cfg(test)]
impl TokenSet {
    /// Whether the token set has no tokens.
    fn is_empty(&self) -> bool {
        self.ranges.is_empty()
    }
}

/// Tokenize text for Jaccard similarity (test-only convenience wrapper).
#[cfg(test)]
fn tokenize(text: &str) -> HashSet<String> {
    let ts = TokenSet::new(text);
    ts.tokens().map(|s| s.to_string()).collect()
}

/// Jaccard similarity between two token sets (borrowed).
fn jaccard_similarity_sets(set_a: &HashSet<&str>, set_b: &HashSet<&str>) -> f64 {
    if set_a.is_empty() && set_b.is_empty() {
        return 1.0;
    }
    if set_a.is_empty() || set_b.is_empty() {
        return 0.0;
    }

    let (smaller, larger) = if set_a.len() <= set_b.len() {
        (set_a, set_b)
    } else {
        (set_b, set_a)
    };

    let intersection_size = smaller.iter().filter(|t| larger.contains(*t)).count();
    let union_size = set_a.len() + set_b.len() - intersection_size;

    if union_size == 0 {
        0.0
    } else {
        intersection_size as f64 / union_size as f64
    }
}

/// Jaccard similarity between two owned token sets (test-only).
#[cfg(test)]
fn jaccard_similarity(set_a: &HashSet<String>, set_b: &HashSet<String>) -> f64 {
    if set_a.is_empty() && set_b.is_empty() {
        return 1.0;
    }
    if set_a.is_empty() || set_b.is_empty() {
        return 0.0;
    }

    let (smaller, larger) = if set_a.len() <= set_b.len() {
        (set_a, set_b)
    } else {
        (set_b, set_a)
    };

    let intersection_size = smaller.iter().filter(|t| larger.contains(t.as_str())).count();
    let union_size = set_a.len() + set_b.len() - intersection_size;

    if union_size == 0 {
        0.0
    } else {
        intersection_size as f64 / union_size as f64
    }
}

/// Text similarity using Jaccard on tokens.
pub fn text_similarity(content_a: &str, content_b: &str) -> f64 {
    let ts_a = TokenSet::new(content_a);
    let ts_b = TokenSet::new(content_b);
    jaccard_similarity_sets(&ts_a.as_set(), &ts_b.as_set())
}

/// Compute MMR score: lambda * relevance - (1-lambda) * `max_similarity`.
pub fn compute_mmr_score(relevance: f64, max_similarity: f64, lambda: f64) -> f64 {
    lambda * relevance - (1.0 - lambda) * max_similarity
}

/// Re-rank items using Maximal Marginal Relevance (MMR).
///
/// Greedy selection: at each step, pick the candidate that maximizes
/// `λ × relevance − (1−λ) × max_similarity_to_selected`. This balances
/// relevance against diversity — higher λ favors relevance, lower λ favors diversity.
///
/// Returns indices into the original `items` slice in MMR order.
pub fn mmr_rerank(items: &[MmrItem], config: &MmrConfig) -> Vec<usize> {
    if !config.enabled || items.len() <= 1 {
        return (0..items.len()).collect();
    }

    let clamped_lambda = config.lambda.clamp(0.0, 1.0);

    // Lambda 1.0 => pure relevance, just sort by score descending
    if clamped_lambda == 1.0 {
        let mut indices: Vec<usize> = (0..items.len()).collect();
        indices.sort_by(|&a, &b| {
            items[b]
                .score
                .partial_cmp(&items[a].score)
                .unwrap_or(std::cmp::Ordering::Equal)
        });
        return indices;
    }

    // Pre-tokenize all items in parallel (benefits from 20-core DGX Spark).
    // Uses TokenSet to avoid per-token String allocations.
    let token_cache: Vec<TokenSet> = items
        .par_iter()
        .map(|item| TokenSet::new(&item.content))
        .collect();

    // Build borrowed HashSets for Jaccard computation.
    let set_cache: Vec<HashSet<&str>> = token_cache.iter().map(TokenSet::as_set).collect();

    // Normalize scores to [0, 1], filtering NaN values
    let max_score = items
        .iter()
        .map(|i| i.score)
        .filter(|s| s.is_finite())
        .fold(f64::NEG_INFINITY, f64::max);
    let min_score = items
        .iter()
        .map(|i| i.score)
        .filter(|s| s.is_finite())
        .fold(f64::INFINITY, f64::min);
    let score_range = if max_score.is_finite() && min_score.is_finite() {
        max_score - min_score
    } else {
        0.0
    };

    let normalize = |score: f64| -> f64 {
        if !score.is_finite() || score_range == 0.0 {
            1.0
        } else {
            ((score - min_score) / score_range).clamp(0.0, 1.0)
        }
    };

    // Greedy MMR loop: select items one at a time, always picking the candidate
    // with the highest MMR score relative to the already-selected set.
    let mut selected: Vec<usize> = Vec::with_capacity(items.len());
    let mut remaining: Vec<usize> = (0..items.len()).collect();

    // Threshold for parallel inner loop: when remaining × selected exceeds this,
    // the O(n²) Jaccard work benefits from multi-core distribution.
    const PAR_THRESHOLD: usize = 64;

    while !remaining.is_empty() {
        let best = if remaining.len() * selected.len().max(1) >= PAR_THRESHOLD {
            // Parallel: distribute MMR scoring across cores.
            remaining
                .par_iter()
                .map(|&candidate_idx| {
                    let normalized_relevance = normalize(items[candidate_idx].score);
                    let max_sim = selected
                        .iter()
                        .map(|&sel_idx| {
                            jaccard_similarity_sets(
                                &set_cache[candidate_idx],
                                &set_cache[sel_idx],
                            )
                        })
                        .fold(0.0_f64, f64::max);
                    let mmr_score =
                        compute_mmr_score(normalized_relevance, max_sim, clamped_lambda);
                    (candidate_idx, mmr_score)
                })
                .max_by(|a, b| {
                    a.1.partial_cmp(&b.1)
                        .unwrap_or(std::cmp::Ordering::Equal)
                        .then_with(|| {
                            items[a.0]
                                .score
                                .partial_cmp(&items[b.0].score)
                                .unwrap_or(std::cmp::Ordering::Equal)
                        })
                })
        } else {
            // Sequential: avoid rayon overhead for small sets.
            remaining
                .iter()
                .map(|&candidate_idx| {
                    let normalized_relevance = normalize(items[candidate_idx].score);
                    let max_sim = selected
                        .iter()
                        .map(|&sel_idx| {
                            jaccard_similarity_sets(
                                &set_cache[candidate_idx],
                                &set_cache[sel_idx],
                            )
                        })
                        .fold(0.0_f64, f64::max);
                    let mmr_score =
                        compute_mmr_score(normalized_relevance, max_sim, clamped_lambda);
                    (candidate_idx, mmr_score)
                })
                .max_by(|a, b| {
                    a.1.partial_cmp(&b.1)
                        .unwrap_or(std::cmp::Ordering::Equal)
                        .then_with(|| {
                            items[a.0]
                                .score
                                .partial_cmp(&items[b.0].score)
                                .unwrap_or(std::cmp::Ordering::Equal)
                        })
                })
        };

        match best {
            Some((idx, _)) => {
                selected.push(idx);
                remaining.retain(|&x| x != idx);
            }
            None => break,
        }
    }

    selected
}

/// Apply MMR re-ranking to hybrid search results.
/// Returns reordered indices based on path:startLine:index IDs.
pub fn mmr_rerank_hybrid(scores: &[f64], snippets: &[&str], config: &MmrConfig) -> Vec<usize> {
    if scores.is_empty() {
        return vec![];
    }

    let items: Vec<MmrItem> = scores
        .iter()
        .zip(snippets.iter())
        .enumerate()
        .map(|(i, (&score, &snippet))| MmrItem {
            id: i.to_string(),
            score,
            content: snippet.to_string(),
        })
        .collect();

    mmr_rerank(&items, config)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_tokenize() {
        let tokens = tokenize("Hello World 123");
        assert!(tokens.contains("hello"));
        assert!(tokens.contains("world"));
        assert!(tokens.contains("123"));
    }

    #[test]
    fn test_tokenize_empty() {
        assert!(tokenize("").is_empty());
        assert!(tokenize("!@#").is_empty());
    }

    #[test]
    fn test_token_set_basic() {
        let ts = TokenSet::new("Hello World 123");
        let set = ts.as_set();
        assert!(set.contains("hello"));
        assert!(set.contains("world"));
        assert!(set.contains("123"));
        assert_eq!(set.len(), 3);
    }

    #[test]
    fn test_token_set_empty() {
        let ts = TokenSet::new("");
        assert!(ts.is_empty());
        let ts2 = TokenSet::new("!@#");
        assert!(ts2.is_empty());
    }

    #[test]
    fn test_jaccard_identical() {
        let a = tokenize("hello world");
        let b = tokenize("hello world");
        assert!((jaccard_similarity(&a, &b) - 1.0).abs() < 1e-10);
    }

    #[test]
    fn test_jaccard_disjoint() {
        let a = tokenize("hello world");
        let b = tokenize("foo bar");
        assert!(jaccard_similarity(&a, &b).abs() < 1e-10);
    }

    #[test]
    fn test_jaccard_empty() {
        let empty: HashSet<String> = HashSet::new();
        assert!((jaccard_similarity(&empty, &empty) - 1.0).abs() < 1e-10);
        let a = tokenize("hello");
        assert!(jaccard_similarity(&a, &empty).abs() < 1e-10);
    }

    #[test]
    fn test_jaccard_partial_overlap() {
        let a = tokenize("hello world foo");
        let b = tokenize("hello world bar");
        // intersection=2, union=4 => 0.5
        assert!((jaccard_similarity(&a, &b) - 0.5).abs() < 1e-10);
    }

    #[test]
    fn test_jaccard_sets_borrowed() {
        let ts_a = TokenSet::new("hello world foo");
        let ts_b = TokenSet::new("hello world bar");
        let sim = jaccard_similarity_sets(&ts_a.as_set(), &ts_b.as_set());
        assert!((sim - 0.5).abs() < 1e-10);
    }

    #[test]
    fn test_mmr_disabled() {
        let items = vec![
            MmrItem {
                id: "a".into(),
                score: 0.9,
                content: "hello world".into(),
            },
            MmrItem {
                id: "b".into(),
                score: 0.8,
                content: "hello world".into(),
            },
        ];
        let config = MmrConfig {
            enabled: false,
            lambda: 0.7,
        };
        let result = mmr_rerank(&items, &config);
        assert_eq!(result, vec![0, 1]);
    }

    #[test]
    fn test_mmr_promotes_diversity() {
        let items = vec![
            MmrItem {
                id: "a".into(),
                score: 0.9,
                content: "rust programming language".into(),
            },
            MmrItem {
                id: "b".into(),
                score: 0.85,
                content: "rust programming tutorial".into(),
            },
            MmrItem {
                id: "c".into(),
                score: 0.8,
                content: "python machine learning".into(),
            },
        ];
        let config = MmrConfig {
            enabled: true,
            lambda: 0.5,
        };
        let result = mmr_rerank(&items, &config);
        // First should be highest score, but "c" (diverse) should be promoted over "b" (similar to "a")
        assert_eq!(result[0], 0); // "a" first (highest relevance)
        assert_eq!(result[1], 2); // "c" second (diverse from "a")
        assert_eq!(result[2], 1); // "b" last (similar to "a")
    }

    #[test]
    fn test_mmr_lambda_one() {
        let items = vec![
            MmrItem {
                id: "a".into(),
                score: 0.5,
                content: "hello".into(),
            },
            MmrItem {
                id: "b".into(),
                score: 0.9,
                content: "world".into(),
            },
        ];
        let config = MmrConfig {
            enabled: true,
            lambda: 1.0,
        };
        let result = mmr_rerank(&items, &config);
        assert_eq!(result, vec![1, 0]); // sorted by score desc
    }

    #[test]
    fn test_compute_mmr_score() {
        assert!((compute_mmr_score(1.0, 0.0, 0.7) - 0.7).abs() < 1e-10);
        assert!((compute_mmr_score(1.0, 1.0, 0.7) - 0.4).abs() < 1e-10);
    }

    #[test]
    fn test_mmr_nan_scores_no_panic() {
        let items = vec![
            MmrItem {
                id: "a".into(),
                score: f64::NAN,
                content: "hello world".into(),
            },
            MmrItem {
                id: "b".into(),
                score: 0.8,
                content: "foo bar".into(),
            },
        ];
        let config = MmrConfig {
            enabled: true,
            lambda: 0.7,
        };
        let result = mmr_rerank(&items, &config);
        assert_eq!(result.len(), 2);
    }

    #[test]
    fn test_mmr_all_same_score() {
        let items = vec![
            MmrItem {
                id: "a".into(),
                score: 0.5,
                content: "hello".into(),
            },
            MmrItem {
                id: "b".into(),
                score: 0.5,
                content: "world".into(),
            },
        ];
        let config = MmrConfig {
            enabled: true,
            lambda: 0.7,
        };
        let result = mmr_rerank(&items, &config);
        assert_eq!(result.len(), 2);
    }

    #[test]
    fn test_mmr_single_item() {
        let items = vec![MmrItem {
            id: "a".into(),
            score: 0.9,
            content: "hello".into(),
        }];
        let config = MmrConfig {
            enabled: true,
            lambda: 0.7,
        };
        let result = mmr_rerank(&items, &config);
        assert_eq!(result, vec![0]);
    }

    #[test]
    fn test_mmr_empty() {
        let items: Vec<MmrItem> = vec![];
        let config = MmrConfig {
            enabled: true,
            lambda: 0.7,
        };
        let result = mmr_rerank(&items, &config);
        assert!(result.is_empty());
    }
}
