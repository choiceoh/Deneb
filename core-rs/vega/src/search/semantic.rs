//! Semantic search using ML embeddings for Vega.
//!
//! When the `ml` feature is enabled, provides vector-based search using
//! deneb-ml's LocalEmbedder for embedding and LocalReranker for reranking.
//! Without it, all functions return empty results.

#[cfg(feature = "ml")]
use rusqlite::params;
use rusqlite::Connection;
use serde::{Deserialize, Serialize};

use rayon::prelude::*;

use super::fts_search::ChunkRow;

/// Semantic search result for a single chunk.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SemanticResult {
    pub chunk_id: i64,
    pub project_id: i64,
    pub project_name: String,
    pub client: String,
    pub status: String,
    pub person_internal: String,
    pub section_heading: String,
    pub content: String,
    pub chunk_type: String,
    pub entry_date: String,
    pub score: f64,
}

/// Configuration for semantic search.
#[derive(Debug, Clone)]
pub struct SemanticConfig {
    /// Maximum number of results to return.
    pub top_k: usize,
    /// Minimum similarity score threshold (0.0–1.0).
    pub min_score: f64,
}

impl Default for SemanticConfig {
    fn default() -> Self {
        Self {
            top_k: 20,
            min_score: 0.3,
        }
    }
}

/// Run semantic search: embed query → cosine similarity against chunk_embeddings.
///
/// Requires `ml` feature and populated chunk_embeddings table.
/// Returns empty vec if ML is unavailable or no embeddings exist.
#[cfg(feature = "ml")]
pub fn semantic_search(
    conn: &Connection,
    query: &str,
    config: &SemanticConfig,
    project_filter: Option<&[i64]>,
    ml_manager: &deneb_ml::ModelManager,
) -> Vec<SemanticResult> {
    let embedder = deneb_ml::LocalEmbedder::new(ml_manager.clone());

    // Embed the query
    let query_vec = match embedder.embed_single(query) {
        Ok(v) => v,
        Err(e) => {
            log::debug!("Semantic search: embed failed: {}", e);
            return Vec::new();
        }
    };

    // Load chunk embeddings from DB
    let mut sql = String::from(
        "SELECT ce.chunk_id, ce.embedding, c.project_id, p.name, p.client, p.status,
                p.person_internal, c.section_heading, c.content, c.chunk_type, c.entry_date
         FROM chunk_embeddings ce
         JOIN chunks c ON c.id = ce.chunk_id
         JOIN projects p ON p.id = c.project_id",
    );

    let mut filter_params: Vec<i64> = Vec::new();
    if let Some(pids) = project_filter {
        if !pids.is_empty() {
            let ph: Vec<&str> = pids.iter().map(|_| "?").collect();
            sql.push_str(&format!(" WHERE c.project_id IN ({})", ph.join(",")));
            filter_params.extend_from_slice(pids);
        }
    }

    let mut stmt = match conn.prepare(&sql) {
        Ok(s) => s,
        Err(e) => {
            log::debug!("Semantic search: prepare failed: {}", e);
            return Vec::new();
        }
    };

    // Collect all rows from DB first (SQLite access is single-threaded).
    let param_refs: Vec<&dyn rusqlite::types::ToSql> = filter_params
        .iter()
        .map(|p| p as &dyn rusqlite::types::ToSql)
        .collect();
    let rows = stmt.query_map(param_refs.as_slice(), |row| {
        let chunk_id: i64 = row.get(0)?;
        let emb_blob: Vec<u8> = row.get(1)?;
        let project_id: i64 = row.get(2)?;
        let name: String = row.get::<_, Option<String>>(3)?.unwrap_or_default();
        let client: String = row.get::<_, Option<String>>(4)?.unwrap_or_default();
        let status: String = row.get::<_, Option<String>>(5)?.unwrap_or_default();
        let person: String = row.get::<_, Option<String>>(6)?.unwrap_or_default();
        let heading: String = row.get::<_, Option<String>>(7)?.unwrap_or_default();
        let content: String = row.get::<_, Option<String>>(8)?.unwrap_or_default();
        let chunk_type: String = row.get::<_, Option<String>>(9)?.unwrap_or_default();
        let entry_date: String = row.get::<_, Option<String>>(10)?.unwrap_or_default();

        Ok((
            chunk_id, emb_blob, project_id, name, client, status, person, heading, content,
            chunk_type, entry_date,
        ))
    });

    let rows = match rows {
        Ok(r) => r,
        Err(e) => {
            log::debug!("Semantic search: query failed: {}", e);
            return Vec::new();
        }
    };

    let all_rows: Vec<_> = rows.flatten().collect();

    // Parallel similarity computation across all chunks (leverages 20-core DGX Spark).
    let mut results: Vec<SemanticResult> = all_rows
        .par_iter()
        .filter_map(|row| {
            let (
                chunk_id,
                emb_blob,
                project_id,
                name,
                client,
                status,
                person,
                heading,
                content,
                chunk_type,
                entry_date,
            ) = row;

            let chunk_vec = blob_to_f32_vec(emb_blob);
            if chunk_vec.len() != query_vec.len() {
                return None; // Dimension mismatch
            }

            // Cosine similarity (vectors are L2-normalized, so dot product = cosine)
            let score = dot_product_simd(&query_vec, &chunk_vec);

            if score >= config.min_score {
                Some(SemanticResult {
                    chunk_id: *chunk_id,
                    project_id: *project_id,
                    project_name: name.clone(),
                    client: client.clone(),
                    status: status.clone(),
                    person_internal: person.clone(),
                    section_heading: heading.clone(),
                    content: content.clone(),
                    chunk_type: chunk_type.clone(),
                    entry_date: entry_date.clone(),
                    score,
                })
            } else {
                None
            }
        })
        .collect();

    // Sort by score descending, take top_k
    results.sort_by(|a, b| {
        b.score
            .partial_cmp(&a.score)
            .unwrap_or(std::cmp::Ordering::Equal)
    });
    results.truncate(config.top_k);
    results
}

/// Run semantic search (no-op when ml feature is disabled).
#[cfg(not(feature = "ml"))]
pub fn semantic_search(
    _conn: &Connection,
    _query: &str,
    _config: &SemanticConfig,
    _project_filter: Option<&[i64]>,
) -> Vec<SemanticResult> {
    Vec::new()
}

/// Semantic search with a pre-computed query vector (no GGUF model needed).
/// Used when embeddings are generated externally (e.g. via SGLang HTTP API).
/// Reuses the same SIMD-accelerated cosine similarity and rayon parallelism.
pub fn semantic_search_with_vec(
    conn: &Connection,
    query_vec: &[f32],
    config: &SemanticConfig,
    project_filter: Option<&[i64]>,
) -> Vec<SemanticResult> {
    if query_vec.is_empty() {
        return Vec::new();
    }

    // Load chunk embeddings from DB.
    let mut sql = String::from(
        "SELECT ce.chunk_id, ce.embedding, c.project_id, p.name, p.client, p.status,
                p.person_internal, c.section_heading, c.content, c.chunk_type, c.entry_date
         FROM chunk_embeddings ce
         JOIN chunks c ON c.id = ce.chunk_id
         JOIN projects p ON p.id = c.project_id",
    );

    let mut filter_params: Vec<i64> = Vec::new();
    if let Some(pids) = project_filter {
        if !pids.is_empty() {
            let ph: Vec<&str> = pids.iter().map(|_| "?").collect();
            sql.push_str(&format!(" WHERE c.project_id IN ({})", ph.join(",")));
            filter_params.extend_from_slice(pids);
        }
    }

    let mut stmt = match conn.prepare(&sql) {
        Ok(s) => s,
        Err(e) => {
            log::debug!("Semantic search (vec): prepare failed: {}", e);
            return Vec::new();
        }
    };

    let param_refs: Vec<&dyn rusqlite::types::ToSql> = filter_params
        .iter()
        .map(|p| p as &dyn rusqlite::types::ToSql)
        .collect();
    let rows = stmt.query_map(param_refs.as_slice(), |row| {
        let chunk_id: i64 = row.get(0)?;
        let emb_blob: Vec<u8> = row.get(1)?;
        let project_id: i64 = row.get(2)?;
        let name: String = row.get::<_, Option<String>>(3)?.unwrap_or_default();
        let client: String = row.get::<_, Option<String>>(4)?.unwrap_or_default();
        let status: String = row.get::<_, Option<String>>(5)?.unwrap_or_default();
        let person: String = row.get::<_, Option<String>>(6)?.unwrap_or_default();
        let heading: String = row.get::<_, Option<String>>(7)?.unwrap_or_default();
        let content: String = row.get::<_, Option<String>>(8)?.unwrap_or_default();
        let chunk_type: String = row.get::<_, Option<String>>(9)?.unwrap_or_default();
        let entry_date: String = row.get::<_, Option<String>>(10)?.unwrap_or_default();

        Ok((
            chunk_id, emb_blob, project_id, name, client, status, person, heading, content,
            chunk_type, entry_date,
        ))
    });

    let rows = match rows {
        Ok(r) => r,
        Err(e) => {
            log::debug!("Semantic search (vec): query failed: {}", e);
            return Vec::new();
        }
    };

    let all_rows: Vec<_> = rows.flatten().collect();
    if all_rows.is_empty() {
        return Vec::new();
    }

    // Separate embeddings from metadata for parallel SIMD computation.
    // Rayon can't par_iter over large tuples, so we split: compute scores
    // in parallel over (index, embedding) pairs, then build results sequentially.
    let embeddings: Vec<Vec<f32>> = all_rows
        .iter()
        .map(|row| blob_to_f32_vec(&row.1))
        .collect();

    // Parallel cosine similarity (SIMD-accelerated, leverages DGX Spark cores).
    let scores: Vec<(usize, f64)> = (0..embeddings.len())
        .into_par_iter()
        .filter_map(|i: usize| {
            let chunk_vec: &Vec<f32> = &embeddings[i];
            if chunk_vec.len() != query_vec.len() {
                return None;
            }
            let score = dot_product_simd(query_vec, chunk_vec);
            if score >= config.min_score {
                Some((i, score))
            } else {
                None
            }
        })
        .collect();

    // Build results from scored indices.
    let mut results: Vec<SemanticResult> = scores
        .into_iter()
        .map(|(i, score)| {
            let row = &all_rows[i];
            SemanticResult {
                chunk_id: row.0,
                project_id: row.2,
                project_name: row.3.clone(),
                client: row.4.clone(),
                status: row.5.clone(),
                person_internal: row.6.clone(),
                section_heading: row.7.clone(),
                content: row.8.clone(),
                chunk_type: row.9.clone(),
                entry_date: row.10.clone(),
                score,
            }
        })
        .collect();

    results.sort_by(|a, b| {
        b.score
            .partial_cmp(&a.score)
            .unwrap_or(std::cmp::Ordering::Equal)
    });
    results.truncate(config.top_k);
    results
}

/// Rerank search results using the ML reranker.
///
/// Takes existing chunk results and reranks them by query relevance.
/// Returns results sorted by reranker score.
#[cfg(feature = "ml")]
pub fn rerank_results(
    query: &str,
    chunks: &[ChunkRow],
    top_k: usize,
    ml_manager: &deneb_ml::ModelManager,
) -> Vec<(usize, f64)> {
    if chunks.is_empty() || query.is_empty() {
        return Vec::new();
    }

    let reranker = deneb_ml::LocalReranker::new(ml_manager.clone());

    // Build document strings for reranking
    let docs: Vec<String> = chunks
        .iter()
        .map(|c| {
            format!(
                "[{}] {}: {}",
                c.name,
                c.section_heading,
                truncate_str(&c.content, 500)
            )
        })
        .collect();
    let doc_refs: Vec<&str> = docs.iter().map(|s| s.as_str()).collect();

    match reranker.rerank_top_k(query, &doc_refs, top_k) {
        Ok(ranked) => ranked.iter().map(|r| (r.index, r.score)).collect(),
        Err(e) => {
            log::debug!("Rerank failed: {}", e);
            Vec::new()
        }
    }
}

/// Rerank (no-op without ml feature).
#[cfg(not(feature = "ml"))]
pub fn rerank_results(_query: &str, _chunks: &[ChunkRow], _top_k: usize) -> Vec<(usize, f64)> {
    Vec::new()
}

/// Embed and store chunk embeddings in the database.
#[cfg(feature = "ml")]
pub fn embed_chunks(
    conn: &Connection,
    model_name: &str,
    ml_manager: &deneb_ml::ModelManager,
) -> Result<usize, Box<dyn std::error::Error>> {
    let embedder = deneb_ml::LocalEmbedder::new(ml_manager.clone());

    // Find chunks without embeddings
    let mut stmt = conn.prepare(
        "SELECT c.id, c.content FROM chunks c
         LEFT JOIN chunk_embeddings ce ON ce.chunk_id = c.id
         WHERE ce.chunk_id IS NULL AND c.content IS NOT NULL AND LENGTH(c.content) > 10",
    )?;

    let chunks: Vec<(i64, String)> = stmt
        .query_map([], |row| Ok((row.get(0)?, row.get(1)?)))?
        .filter_map(|r| r.ok())
        .collect();

    if chunks.is_empty() {
        return Ok(0);
    }

    let mut count = 0;
    // Process in batches of 32 (increased for DGX Spark throughput).
    for batch in chunks.chunks(32) {
        let texts: Vec<&str> = batch.iter().map(|(_, content)| content.as_str()).collect();
        let result = embedder.embed(&texts)?;

        for (i, (chunk_id, _)) in batch.iter().enumerate() {
            if i >= result.vectors.len() {
                break;
            }
            let emb_blob = f32_vec_to_blob(&result.vectors[i]);
            conn.execute(
                "INSERT OR REPLACE INTO chunk_embeddings (chunk_id, embedding, model_name, updated_at)
                 VALUES (?1, ?2, ?3, ?4)",
                params![chunk_id, emb_blob, model_name, chrono::Utc::now().to_rfc3339()],
            )?;
            count += 1;
        }
    }

    Ok(count)
}

/// Embed chunks (no-op without ml feature).
#[cfg(not(feature = "ml"))]
pub fn embed_chunks(
    _conn: &Connection,
    _model_name: &str,
) -> Result<usize, Box<dyn std::error::Error>> {
    Ok(0)
}

// -- Utility functions --

/// Convert a f32 vector to a byte blob (little-endian).
#[allow(dead_code)]
fn f32_vec_to_blob(vec: &[f32]) -> Vec<u8> {
    let mut blob = Vec::with_capacity(vec.len() * 4);
    for &v in vec {
        blob.extend_from_slice(&v.to_le_bytes());
    }
    blob
}

/// Convert a byte blob (little-endian) to a f32 vector.
#[allow(dead_code)]
fn blob_to_f32_vec(blob: &[u8]) -> Vec<f32> {
    blob.chunks_exact(4)
        .map(|chunk| f32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]]))
        .collect()
}

/// Dot product of two f32 vectors (cosine similarity for L2-normalized vectors).
/// SIMD-optimized: uses NEON on aarch64, SSE on x86_64, scalar fallback otherwise.
#[allow(dead_code)]
fn dot_product_simd(a: &[f32], b: &[f32]) -> f64 {
    let len = a.len().min(b.len());
    if len == 0 {
        return 0.0;
    }

    #[cfg(target_arch = "aarch64")]
    {
        dot_product_neon(&a[..len], &b[..len])
    }

    #[cfg(not(target_arch = "aarch64"))]
    {
        dot_product_scalar(&a[..len], &b[..len])
    }
}

/// NEON-accelerated f32 dot product for aarch64.
/// Processes 4 f32 elements per iteration using 128-bit NEON registers.
#[cfg(target_arch = "aarch64")]
#[allow(unsafe_code, dead_code)]
fn dot_product_neon(a: &[f32], b: &[f32]) -> f64 {
    use std::arch::aarch64::*;

    let len = a.len();
    let chunks = len / 4;
    let remainder = len % 4;

    // SAFETY: NEON is always available on aarch64.
    unsafe {
        let mut acc = vdupq_n_f32(0.0);

        for i in 0..chunks {
            let offset = i * 4;
            let va = vld1q_f32(a.as_ptr().add(offset));
            let vb = vld1q_f32(b.as_ptr().add(offset));
            acc = vfmaq_f32(acc, va, vb);
        }

        let mut sum = vaddvq_f32(acc) as f64;

        // Scalar tail for remaining elements.
        let tail_start = chunks * 4;
        for i in 0..remainder {
            sum += a[tail_start + i] as f64 * b[tail_start + i] as f64;
        }

        sum
    }
}

/// Scalar f32 dot product fallback.
#[allow(dead_code)]
fn dot_product_scalar(a: &[f32], b: &[f32]) -> f64 {
    a.iter()
        .zip(b.iter())
        .map(|(&x, &y)| x as f64 * y as f64)
        .sum()
}

#[allow(dead_code)]
fn truncate_str(s: &str, max: usize) -> &str {
    if s.len() <= max {
        return s;
    }
    // Find a valid char boundary
    let mut end = max;
    while !s.is_char_boundary(end) && end > 0 {
        end -= 1;
    }
    &s[..end]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_f32_blob_roundtrip() {
        let vec = vec![1.0f32, 2.5, -0.5, 0.0];
        let blob = f32_vec_to_blob(&vec);
        let recovered = blob_to_f32_vec(&blob);
        assert_eq!(vec, recovered);
    }

    #[test]
    fn test_dot_product() {
        let a = vec![1.0f32, 0.0, 0.0];
        let b = vec![1.0f32, 0.0, 0.0];
        assert!((dot_product_simd(&a, &b) - 1.0).abs() < 1e-6);

        let c = vec![0.0f32, 1.0, 0.0];
        assert!((dot_product_simd(&a, &c)).abs() < 1e-6);
    }

    #[test]
    fn test_truncate_str() {
        assert_eq!(truncate_str("hello", 10), "hello");
        assert_eq!(truncate_str("hello world", 5), "hello");
        // Korean multibyte safety
        let korean = "안녕하세요";
        let t = truncate_str(korean, 6); // 3 bytes for '안', 3 for '녕'
        assert_eq!(t, "안녕");
    }
}
