use std::collections::HashMap;

use super::mmr;
use super::temporal_decay;
use super::types::*;

/// Merge hybrid search results (vector + keyword) into a single scored list.
///
/// This is the pure-computation portion of `mergeHybridResults` from TypeScript.
/// Filesystem operations (stat for mtime) are NOT included — only path-based
/// date extraction is used for temporal decay.
pub fn merge_hybrid_results(params: &MergeParams) -> Vec<MergedResult> {
    let mut by_id: HashMap<&str, MergeEntry> = HashMap::new();

    // Collect vector results
    for r in &params.vector {
        by_id.insert(
            &r.id,
            MergeEntry {
                path: &r.path,
                start_line: r.start_line,
                end_line: r.end_line,
                source: &r.source,
                snippet: &r.snippet,
                vector_score: r.vector_score,
                text_score: 0.0,
            },
        );
    }

    // Merge keyword results
    for r in &params.keyword {
        if let Some(existing) = by_id.get_mut(r.id.as_str()) {
            existing.text_score = r.text_score;
            if !r.snippet.is_empty() {
                existing.snippet = &r.snippet;
            }
        } else {
            by_id.insert(
                &r.id,
                MergeEntry {
                    path: &r.path,
                    start_line: r.start_line,
                    end_line: r.end_line,
                    source: &r.source,
                    snippet: &r.snippet,
                    vector_score: 0.0,
                    text_score: r.text_score,
                },
            );
        }
    }

    // Compute weighted scores
    let mut merged: Vec<MergedResult> = by_id
        .values()
        .map(|entry| {
            let score =
                params.vector_weight * entry.vector_score + params.text_weight * entry.text_score;
            MergedResult {
                path: entry.path.to_string(),
                start_line: entry.start_line,
                end_line: entry.end_line,
                score,
                snippet: entry.snippet.to_string(),
                source: entry.source.to_string(),
            }
        })
        .collect();

    // Apply temporal decay (pure path-based only, no filesystem)
    let decay_config = params
        .temporal_decay
        .as_ref()
        .cloned()
        .unwrap_or_default();

    if decay_config.enabled {
        let now_ms = params.now_ms.unwrap_or_else(|| {
            chrono::Utc::now().timestamp_millis() as f64
        });
        for result in &mut merged {
            // Try to extract date from path
            if let Some((year, month, day)) = temporal_decay::parse_memory_date_from_path(&result.path) {
                let ts_ms = temporal_decay::date_to_ms(year, month, day);
                let age = temporal_decay::age_in_days_from_ms(ts_ms, now_ms);
                result.score = temporal_decay::apply_temporal_decay_to_score(
                    result.score,
                    age,
                    decay_config.half_life_days,
                );
            } else if result.source == "memory"
                && temporal_decay::is_evergreen_memory_path(&result.path)
            {
                // Evergreen files: no decay
            }
            // Non-dated, non-evergreen files: no decay (would need fs.stat in TS)
        }
    }

    // Sort by score descending, with path+startLine as tiebreaker for determinism
    merged.sort_by(|a, b| {
        b.score
            .partial_cmp(&a.score)
            .unwrap_or(std::cmp::Ordering::Equal)
            .then_with(|| a.path.cmp(&b.path))
            .then_with(|| a.start_line.cmp(&b.start_line))
    });

    // Apply MMR re-ranking if enabled
    let mmr_config = params.mmr.as_ref().cloned().unwrap_or_default();
    if mmr_config.enabled {
        let scores: Vec<f64> = merged.iter().map(|r| r.score).collect();
        let snippets: Vec<&str> = merged.iter().map(|r| r.snippet.as_str()).collect();
        let indices = mmr::mmr_rerank_hybrid(&scores, &snippets, &mmr_config);
        let original = merged;
        merged = indices.into_iter().map(|i| original[i].clone()).collect();
    }

    merged
}

struct MergeEntry<'a> {
    path: &'a str,
    start_line: u32,
    end_line: u32,
    source: &'a str,
    snippet: &'a str,
    vector_score: f64,
    text_score: f64,
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_vector(id: &str, path: &str, score: f64) -> HybridVectorResult {
        HybridVectorResult {
            id: id.to_string(),
            path: path.to_string(),
            start_line: 1,
            end_line: 10,
            source: "memory".to_string(),
            snippet: format!("snippet for {}", id),
            vector_score: score,
        }
    }

    fn make_keyword(id: &str, path: &str, score: f64) -> HybridKeywordResult {
        HybridKeywordResult {
            id: id.to_string(),
            path: path.to_string(),
            start_line: 1,
            end_line: 10,
            source: "memory".to_string(),
            snippet: format!("keyword snippet for {}", id),
            text_score: score,
        }
    }

    #[test]
    fn test_merge_vector_only() {
        let params = MergeParams {
            vector: vec![make_vector("a", "file.md", 0.9), make_vector("b", "file.md", 0.7)],
            keyword: vec![],
            vector_weight: 0.7,
            text_weight: 0.3,
            mmr: None,
            temporal_decay: None,
            now_ms: None,
        };
        let results = merge_hybrid_results(&params);
        assert_eq!(results.len(), 2);
        assert!((results[0].score - 0.63).abs() < 1e-10); // 0.7 * 0.9
        assert!((results[1].score - 0.49).abs() < 1e-10); // 0.7 * 0.7
    }

    #[test]
    fn test_merge_with_overlap() {
        let params = MergeParams {
            vector: vec![make_vector("a", "file.md", 0.9)],
            keyword: vec![make_keyword("a", "file.md", 0.8)],
            vector_weight: 0.7,
            text_weight: 0.3,
            mmr: None,
            temporal_decay: None,
            now_ms: None,
        };
        let results = merge_hybrid_results(&params);
        assert_eq!(results.len(), 1);
        // 0.7 * 0.9 + 0.3 * 0.8 = 0.63 + 0.24 = 0.87
        assert!((results[0].score - 0.87).abs() < 1e-10);
    }

    #[test]
    fn test_merge_sorted_by_score() {
        let params = MergeParams {
            vector: vec![
                make_vector("a", "file.md", 0.5),
                make_vector("b", "file.md", 0.9),
            ],
            keyword: vec![],
            vector_weight: 1.0,
            text_weight: 0.0,
            mmr: None,
            temporal_decay: None,
            now_ms: None,
        };
        let results = merge_hybrid_results(&params);
        assert!(results[0].score >= results[1].score);
    }

    #[test]
    fn test_merge_empty() {
        let params = MergeParams {
            vector: vec![],
            keyword: vec![],
            vector_weight: 0.7,
            text_weight: 0.3,
            mmr: None,
            temporal_decay: None,
            now_ms: None,
        };
        let results = merge_hybrid_results(&params);
        assert!(results.is_empty());
    }

    #[test]
    fn test_merge_with_temporal_decay() {
        let now_ms = temporal_decay::date_to_ms(2026, 3, 24);
        let params = MergeParams {
            vector: vec![make_vector("a", "memory/2026-02-22.md", 0.9)],
            keyword: vec![],
            vector_weight: 1.0,
            text_weight: 0.0,
            mmr: None,
            temporal_decay: Some(TemporalDecayConfig {
                enabled: true,
                half_life_days: 30.0,
            }),
            now_ms: Some(now_ms),
        };
        let results = merge_hybrid_results(&params);
        assert_eq!(results.len(), 1);
        // ~30 days old, so score should be roughly halved
        assert!(results[0].score < 0.9);
        assert!(results[0].score > 0.3);
    }

    #[test]
    fn test_merge_evergreen_no_decay() {
        let now_ms = temporal_decay::date_to_ms(2026, 3, 24);
        let params = MergeParams {
            vector: vec![make_vector("a", "memory/topics.md", 0.9)],
            keyword: vec![],
            vector_weight: 1.0,
            text_weight: 0.0,
            mmr: None,
            temporal_decay: Some(TemporalDecayConfig {
                enabled: true,
                half_life_days: 30.0,
            }),
            now_ms: Some(now_ms),
        };
        let results = merge_hybrid_results(&params);
        // Evergreen file should not decay
        assert!((results[0].score - 0.9).abs() < 1e-10);
    }
}
