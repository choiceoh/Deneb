//! Maximal Marginal Relevance (MMR) re-ranking for search result diversity.

#[cfg(feature = "parallel")]
use rayon::prelude::*;
use regex::Regex;
use std::collections::HashSet;
use std::sync::LazyLock;

use super::types::{MmrConfig, MmrItem};

#[allow(clippy::expect_used)]
static MMR_TOKEN_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[a-z0-9_]+").expect("valid regex"));

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


/// Tokenize text for Jaccard similarity (test-only convenience wrapper).

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

    // Pre-tokenize all items. Uses TokenSet to avoid per-token String allocations.
    #[cfg(feature = "parallel")]
    let token_cache: Vec<TokenSet> = items
        .par_iter()
        .map(|item| TokenSet::new(&item.content))
        .collect();
    #[cfg(not(feature = "parallel"))]
    let token_cache: Vec<TokenSet> = items
        .iter()
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

    // Greedy MMR loop with incremental similarity cache.
    //
    // Key insight: instead of recomputing max_sim(candidate, selected) from scratch
    // each round (O(remaining × selected) per round → O(n³/6) total), we maintain
    // a per-candidate cache of the maximum similarity seen so far.  When an item is
    // newly selected, we update only the pairings with that one item — reducing total
    // Jaccard calls from O(n³/6) to O(n²/2), a ~(n/3)× improvement for large n.
    let mut selected: Vec<usize> = Vec::with_capacity(items.len());
    let mut remaining: Vec<usize> = (0..items.len()).collect();

    // best_sim_cache[i] = max Jaccard similarity between item i and any selected item.
    // Initialised to 0.0 (no selected items yet).
    let mut best_sim_cache: Vec<f64> = vec![0.0; items.len()];

    // Threshold for parallelising the cache-update sweep (O(remaining) Jaccard calls).
    #[cfg(feature = "parallel")]
    const PAR_THRESHOLD: usize = 32;

    let pick_best = |remaining: &[usize], best_sim: &[f64]| {
        remaining
            .iter()
            .map(|&i| {
                let mmr = compute_mmr_score(normalize(items[i].score), best_sim[i], clamped_lambda);
                (i, mmr)
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

    while !remaining.is_empty() {
        let Some((idx, _)) = pick_best(&remaining, &best_sim_cache) else {
            break;
        };

        selected.push(idx);
        remaining.retain(|&x| x != idx);

        // Update cache: compute Jaccard between the newly selected item and each
        // remaining candidate, raising the cached max if the new similarity is higher.
        #[cfg(feature = "parallel")]
        if remaining.len() >= PAR_THRESHOLD {
            // Parallel update when the sweep is large enough to benefit from rayon.
            let new_sims: Vec<f64> = remaining
                .par_iter()
                .map(|&r| jaccard_similarity_sets(&set_cache[r], &set_cache[idx]))
                .collect();
            for (&r, sim) in remaining.iter().zip(new_sims) {
                if sim > best_sim_cache[r] {
                    best_sim_cache[r] = sim;
                }
            }
        } else {
            for &r in &remaining {
                let sim = jaccard_similarity_sets(&set_cache[r], &set_cache[idx]);
                if sim > best_sim_cache[r] {
                    best_sim_cache[r] = sim;
                }
            }
        }
        #[cfg(not(feature = "parallel"))]
        for &r in &remaining {
            let sim = jaccard_similarity_sets(&set_cache[r], &set_cache[idx]);
            if sim > best_sim_cache[r] {
                best_sim_cache[r] = sim;
            }
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
