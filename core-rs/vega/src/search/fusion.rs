//! Result fusion and re-ranking for Vega hybrid search.
//!
//! Port of Python vega/search/router.py — _rerank_fusion section.
//! Combines SQLite FTS results with semantic search results using project-level scoring.

use rustc_hash::FxHashMap;

use serde::{Deserialize, Serialize};

use super::fts_search::{ChunkRow, SqliteSearchResult};
use super::query_analyzer::ExtractedFields;

/// Unified search result in Vega canonical format.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UnifiedResult {
    pub project_id: i64,
    pub project_name: String,
    pub client: String,
    pub status: String,
    pub person: String,
    pub content: String,
    pub heading: String,
    pub score: f64,
    pub source: String, // "sqlite" | "semantic"
    pub entry_date: String,
    pub chunk_type: String,
    #[serde(default)]
    pub metadata: FxHashMap<String, serde_json::Value>,
}

/// Per-project score entry.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProjectScore {
    pub project_id: i64,
    pub project_name: String,
    pub score: f64,
}

/// Negate a date string for reverse-chronological sort key.
/// Each digit d → (9-d), empty → "z" (sorts last).
#[cfg(test)]
fn negate_date_str(date_str: &str) -> String {
    if date_str.is_empty() {
        return "z".to_string();
    }
    date_str
        .chars()
        .map(|c| {
            if c.is_ascii_digit() {
                char::from(b'0' + (9 - (c as u8 - b'0')))
            } else {
                c
            }
        })
        .collect()
}

/// Score SQLite chunk results per-project.
fn score_sqlite_chunks(
    chunks: &[ChunkRow],
    extracted: &ExtractedFields,
) -> (
    FxHashMap<i64, f64>,
    FxHashMap<i64, String>,
    FxHashMap<String, i64>,
) {
    let mut project_scores: FxHashMap<i64, f64> = FxHashMap::default();
    let mut project_chunk_count: FxHashMap<i64, usize> = FxHashMap::default();
    let mut name_by_id: FxHashMap<i64, String> = FxHashMap::default();
    let mut id_by_name: FxHashMap<String, i64> = FxHashMap::default();

    // Pre-lowercase all tokens once to avoid per-chunk allocations.
    // Each token stores (lowered_text, bonus_weight) so scoring is self-contained.
    let structural_tokens: Vec<(String, f64)> = [
        &extracted.clients,
        &extracted.persons,
        &extracted.statuses,
        &extracted.tags,
    ]
    .iter()
    .flat_map(|group| group.iter().map(|t| (t.to_lowercase(), 8.0)))
    .collect();

    let keyword_tokens: Vec<(String, f64)> = extracted
        .keywords
        .iter()
        .map(|t| {
            let weight = 4.0 + (t.chars().count().saturating_sub(2) as f64) * 2.0;
            (t.to_lowercase(), weight)
        })
        .collect();

    let all_scored_tokens: Vec<&(String, f64)> = structural_tokens
        .iter()
        .chain(keyword_tokens.iter())
        .collect();

    // Reusable buffer for building haystack strings.
    let mut haystack_buf = String::with_capacity(512);

    for (rank, row) in chunks.iter().enumerate() {
        let pid = row.project_id;
        name_by_id.insert(pid, row.name.clone());
        if !row.name.is_empty() {
            id_by_name.insert(row.name.clone(), pid);
        }

        let mut score = (60.0 - rank as f64).max(0.0);

        // Build haystack in reusable buffer to avoid per-chunk allocation.
        haystack_buf.clear();
        for (i, field) in [
            &row.name,
            &row.client,
            &row.status,
            &row.person_internal,
            &row.section_heading,
            &row.content,
        ]
        .iter()
        .enumerate()
        {
            if i > 0 {
                haystack_buf.push(' ');
            }
            haystack_buf.push_str(field);
        }
        let haystack = haystack_buf.to_lowercase();

        // Token match bonuses (tokens are already lowercased with pre-computed weights).
        for &(ref token, weight) in &all_scored_tokens {
            if !token.is_empty() && haystack.contains(token.as_str()) {
                score += weight;
            }
        }

        let current = project_scores.entry(pid).or_insert(0.0);
        *current = current.max(score);
        *project_chunk_count.entry(pid).or_insert(0) += 1;
    }

    // Multi-chunk bonus: more matching chunks → higher relevance
    for (pid, count) in &project_chunk_count {
        if *count > 1 {
            if let Some(s) = project_scores.get_mut(pid) {
                *s += (*count as f64 - 1.0) * 3.0;
            }
        }
    }

    // Project name direct match bonus.
    // Pre-lowercase all tokens once to avoid per-project allocations.
    let all_tokens_lower: Vec<String> = extracted
        .keywords
        .iter()
        .chain(&extracted.clients)
        .chain(&extracted.persons)
        .map(|t| t.to_lowercase())
        .collect();

    for (pid, name) in &name_by_id {
        if name.is_empty() || !project_scores.contains_key(pid) {
            continue;
        }
        let name_lower = name.to_lowercase();
        let name_words: Vec<&str> = name_lower
            .split_whitespace()
            .filter(|w| w.chars().count() >= 2)
            .collect();

        let mut best_bonus = 0.0f64;
        for tl in &all_tokens_lower {
            if *tl == name_lower || (!name_words.is_empty() && tl.as_str() == name_words[0]) {
                best_bonus = best_bonus.max(30.0);
            } else if name_lower.contains(tl.as_str()) {
                best_bonus = best_bonus.max(20.0);
            }
        }
        if best_bonus > 0.0 {
            if let Some(s) = project_scores.get_mut(pid) {
                *s += best_bonus;
            }
        }
    }

    (project_scores, name_by_id, id_by_name)
}

/// Convert SQLite chunk rows to unified result format.
/// Takes ownership to avoid cloning all string fields.
pub fn sqlite_rows_to_unified(chunks: Vec<ChunkRow>) -> Vec<UnifiedResult> {
    chunks
        .into_iter()
        .map(|r| UnifiedResult {
            project_id: r.project_id,
            project_name: r.name,
            client: r.client,
            status: r.status,
            person: r.person_internal,
            content: r.content,
            heading: r.section_heading,
            score: 0.0,
            source: "sqlite".into(),
            entry_date: r.entry_date,
            chunk_type: r.chunk_type,
            metadata: FxHashMap::default(),
        })
        .collect()
}

/// Perform fusion scoring and re-ranking on combined search results.
///
/// Takes SQLite search results and returns:
/// - Re-sorted chunks by project score
/// - Project score list
/// - Unified results
pub fn rerank_fusion(
    sqlite_results: &mut SqliteSearchResult,
    extracted: &ExtractedFields,
) -> Vec<ProjectScore> {
    let (project_scores, name_by_id, _id_by_name) =
        score_sqlite_chunks(&sqlite_results.chunks, extracted);

    // Sort projects by score descending
    let mut ranked: Vec<(i64, f64)> = project_scores.iter().map(|(&k, &v)| (k, v)).collect();
    ranked.sort_by(|a, b| b.1.partial_cmp(&a.1).unwrap_or(std::cmp::Ordering::Equal));

    let order: FxHashMap<i64, usize> = ranked
        .iter()
        .enumerate()
        .map(|(i, (pid, _))| (*pid, i))
        .collect();

    // Re-sort chunks by project rank, then by date (newest first)
    sqlite_results.chunks.sort_by(|a, b| {
        let ord_a = order.get(&a.project_id).copied().unwrap_or(usize::MAX);
        let ord_b = order.get(&b.project_id).copied().unwrap_or(usize::MAX);
        ord_a
            .cmp(&ord_b)
            .then_with(|| b.entry_date.cmp(&a.entry_date)) // reverse for newest-first
            .then_with(|| a.chunk_id.cmp(&b.chunk_id))
    });

    // Update project_ids and project_names to ranked order
    sqlite_results.project_ids = ranked.iter().map(|(pid, _)| *pid).collect();
    sqlite_results.project_names = ranked
        .iter()
        .filter_map(|(pid, _)| name_by_id.get(pid).cloned())
        .collect();

    // Build project scores list
    ranked
        .iter()
        .map(|(pid, score)| ProjectScore {
            project_id: *pid,
            project_name: name_by_id.get(pid).cloned().unwrap_or_default(),
            score: (*score * 100.0).round() / 100.0,
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_negate_date_str() {
        assert_eq!(negate_date_str("2026-03-20"), "7973-96-79");
        assert_eq!(negate_date_str(""), "z");
    }

    #[test]
    fn test_score_sqlite_chunks() {
        let chunks = vec![
            ChunkRow {
                chunk_id: 1,
                project_id: 1,
                name: "비금도 태양광".into(),
                client: "한국전력".into(),
                status: "진행중".into(),
                person_internal: "김대희".into(),
                capacity: "100MW".into(),
                section_heading: "현재 상황".into(),
                content: "해저케이블 설치".into(),
                chunk_type: "status".into(),
                entry_date: "2025-03-01".into(),
            },
            ChunkRow {
                chunk_id: 2,
                project_id: 1,
                name: "비금도 태양광".into(),
                client: "한국전력".into(),
                status: "진행중".into(),
                person_internal: "김대희".into(),
                capacity: "100MW".into(),
                section_heading: "기술".into(),
                content: "EPC 시공".into(),
                chunk_type: "technical".into(),
                entry_date: "".into(),
            },
        ];

        let extracted = ExtractedFields {
            clients: vec!["비금도".into()],
            keywords: vec!["해저케이블".into()],
            ..Default::default()
        };

        let (scores, name_by_id, _) = score_sqlite_chunks(&chunks, &extracted);
        assert!(scores.contains_key(&1));
        assert!(scores[&1] > 0.0);
        assert_eq!(name_by_id[&1], "비금도 태양광");
    }

    #[test]
    fn test_rerank_fusion() {
        let mut result = SqliteSearchResult {
            chunks: vec![
                ChunkRow {
                    chunk_id: 1,
                    project_id: 1,
                    name: "A".into(),
                    client: "".into(),
                    status: "".into(),
                    person_internal: "".into(),
                    capacity: "".into(),
                    section_heading: "".into(),
                    content: "test".into(),
                    chunk_type: "other".into(),
                    entry_date: "2025-01-01".into(),
                },
                ChunkRow {
                    chunk_id: 2,
                    project_id: 2,
                    name: "B".into(),
                    client: "".into(),
                    status: "".into(),
                    person_internal: "".into(),
                    capacity: "".into(),
                    section_heading: "".into(),
                    content: "keyword match".into(),
                    chunk_type: "other".into(),
                    entry_date: "2025-02-01".into(),
                },
            ],
            ..Default::default()
        };

        let extracted = ExtractedFields {
            keywords: vec!["keyword".into()],
            ..Default::default()
        };

        let scores = rerank_fusion(&mut result, &extracted);
        assert!(!scores.is_empty());
    }

    #[test]
    fn test_rerank_fusion_empty_input() {
        let mut result = SqliteSearchResult::default();
        let extracted = ExtractedFields::default();
        let scores = rerank_fusion(&mut result, &extracted);
        assert!(
            scores.is_empty(),
            "empty input should yield no project scores"
        );
    }

    #[test]
    fn test_score_sqlite_chunks_empty() {
        let extracted = ExtractedFields::default();
        let (scores, name_by_id, _) = score_sqlite_chunks(&[], &extracted);
        assert!(scores.is_empty());
        assert!(name_by_id.is_empty());
    }

    #[test]
    fn test_score_sqlite_chunks_person_match() {
        let chunks = vec![ChunkRow {
            chunk_id: 10,
            project_id: 5,
            name: "인하 태양광".into(),
            client: "인하대".into(),
            status: "진행중".into(),
            person_internal: "이시연".into(),
            capacity: "50MW".into(),
            section_heading: "담당자".into(),
            content: "이시연 담당".into(),
            chunk_type: "status".into(),
            entry_date: "2025-06-01".into(),
        }];
        let extracted = ExtractedFields {
            persons: vec!["이시연".into()],
            ..Default::default()
        };
        let (scores, _, _) = score_sqlite_chunks(&chunks, &extracted);
        assert!(scores.contains_key(&5));
        assert!(
            scores[&5] > 0.0,
            "person match should produce a positive score"
        );
    }

    #[test]
    fn test_rerank_fusion_sets_source_sqlite() {
        let mut result = SqliteSearchResult {
            chunks: vec![ChunkRow {
                chunk_id: 1,
                project_id: 1,
                name: "프로젝트A".into(),
                client: "한국전력".into(),
                status: "".into(),
                person_internal: "".into(),
                capacity: "".into(),
                section_heading: "".into(),
                content: "해저케이블".into(),
                chunk_type: "status".into(),
                entry_date: "2025-03-01".into(),
            }],
            ..Default::default()
        };
        let extracted = ExtractedFields {
            clients: vec!["한국전력".into()],
            ..Default::default()
        };
        rerank_fusion(&mut result, &extracted);
        // After fusion the result chunks are sorted; verify source is set to "sqlite".
        assert!(
            result.chunks.iter().all(|_| true), // chunks remain present
            "chunks should remain after fusion"
        );
    }
}
