//! Semantic search for Vega.
//!
//! Provides vector-based search using pre-computed embeddings (e.g. from SGLang
//! or Gemini API). Uses SIMD-accelerated cosine similarity and rayon parallelism.

use rusqlite::Connection;

use rayon::prelude::*;

use super::fts_search::ChunkRow;

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

/// Semantic search with a pre-computed query vector.
/// Used when embeddings are generated externally (e.g. via SGLang HTTP API).
/// Reuses the same SIMD-accelerated cosine similarity and rayon parallelism.
pub fn semantic_search_with_vec(
    conn: &Connection,
    query_vec: &[f32],
    config: &SemanticConfig,
    project_filter: Option<&[i64]>,
) -> Vec<ChunkRow> {
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
            let _ = e;
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
            let _ = e;
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
    let embeddings: Vec<Vec<f32>> = all_rows.iter().map(|row| blob_to_f32_vec(&row.1)).collect();

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

    // Build results from scored indices (sorted by cosine similarity descending).
    let mut scored: Vec<(usize, f64)> = scores;
    scored.sort_by(|a, b| b.1.partial_cmp(&a.1).unwrap_or(std::cmp::Ordering::Equal));
    scored.truncate(config.top_k);

    scored
        .into_iter()
        .map(|(i, _score)| {
            let row = &all_rows[i];
            ChunkRow {
                chunk_id: row.0,
                project_id: row.2,
                name: row.3.clone(),
                client: row.4.clone(),
                status: row.5.clone(),
                person_internal: row.6.clone(),
                capacity: String::new(),
                section_heading: row.7.clone(),
                content: row.8.clone(),
                chunk_type: row.9.clone(),
                entry_date: row.10.clone(),
            }
        })
        .collect()
}

// -- Utility functions --

/// Convert a byte blob (little-endian) to a f32 vector.
fn blob_to_f32_vec(blob: &[u8]) -> Vec<f32> {
    blob.chunks_exact(4)
        .map(|chunk| f32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]]))
        .collect()
}

/// Dot product of two f32 vectors (cosine similarity for L2-normalized vectors).
///
/// On aarch64 (production), uses NEON 128-bit FMA processing 4 f32 per iteration.
/// Scalar fallback on other architectures (CI).
#[allow(unsafe_code)]
fn dot_product_simd(a: &[f32], b: &[f32]) -> f64 {
    let len = a.len().min(b.len());
    if len == 0 {
        return 0.0;
    }

    #[cfg(target_arch = "aarch64")]
    {
        use std::arch::aarch64::*;

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

    #[cfg(not(target_arch = "aarch64"))]
    {
        a[..len]
            .iter()
            .zip(b[..len].iter())
            .map(|(&x, &y)| x as f64 * y as f64)
            .sum()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Convert a f32 vector to a byte blob (little-endian).
    fn f32_vec_to_blob(vec: &[f32]) -> Vec<u8> {
        let mut blob = Vec::with_capacity(vec.len() * 4);
        for &v in vec {
            blob.extend_from_slice(&v.to_le_bytes());
        }
        blob
    }

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
}
