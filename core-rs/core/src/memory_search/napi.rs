//! napi-rs wrappers for memory search functions.
//!
//! These functions are conditionally compiled with the `napi_binding` feature
//! and exposed to Node.js via the @deneb/native addon.

#[cfg(feature = "napi_binding")]
use napi::bindgen_prelude::*;

use super::{bm25, cosine, fts, merge, mmr, query_expansion, temporal_decay, types};

/// Maximum input size for napi string/JSON functions (16 MB, matching `FFI_MAX_INPUT_LEN`).
const NAPI_MAX_INPUT_LEN: usize = 16 * 1024 * 1024;

// ---------------------------------------------------------------------------
// Cosine similarity — zero-copy Float64Array
// ---------------------------------------------------------------------------

#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_cosine_similarity(a: Vec<f64>, b: Vec<f64>) -> f64 {
    cosine::cosine_similarity(&a, &b)
}

// ---------------------------------------------------------------------------
// BM25
// ---------------------------------------------------------------------------

#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_bm25_rank_to_score(rank: f64) -> f64 {
    bm25::bm25_rank_to_score(rank)
}

// ---------------------------------------------------------------------------
// FTS query building
// ---------------------------------------------------------------------------

#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_build_fts_query(raw: String) -> Option<String> {
    fts::build_fts_query(&raw)
}

// ---------------------------------------------------------------------------
// Temporal decay
// ---------------------------------------------------------------------------

#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_temporal_decay_multiplier(age_in_days: f64, half_life_days: f64) -> f64 {
    temporal_decay::calculate_temporal_decay_multiplier(age_in_days, half_life_days)
}

#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_apply_temporal_decay(score: f64, age_in_days: f64, half_life_days: f64) -> f64 {
    temporal_decay::apply_temporal_decay_to_score(score, age_in_days, half_life_days)
}

/// Returns ISO date string "YYYY-MM-DD" or null if path is not a dated memory file.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_parse_memory_date_from_path(file_path: String) -> Option<String> {
    temporal_decay::parse_memory_date_from_path(&file_path)
        .map(|(y, m, d)| format!("{y:04}-{m:02}-{d:02}"))
}

#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_is_evergreen_memory_path(file_path: String) -> bool {
    temporal_decay::is_evergreen_memory_path(&file_path)
}

// ---------------------------------------------------------------------------
// MMR re-ranking
// ---------------------------------------------------------------------------

/// Takes JSON arrays of items and config, returns JSON array of reranked items.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_mmr_rerank(items_json: String, config_json: String) -> String {
    if items_json.len() > NAPI_MAX_INPUT_LEN {
        return r#"{"error":"input_too_large","detail":"items_json exceeds 16MB limit"}"#
            .to_string();
    }
    let items: Vec<types::MmrItem> = match serde_json::from_str(&items_json) {
        Ok(v) => v,
        Err(e) => {
            return format!(
                r#"{{"error":"parse_failed","detail":"items_json: {}"}}"#,
                e.to_string().replace('"', "'")
            );
        }
    };
    let config: types::MmrConfig = serde_json::from_str(&config_json).unwrap_or_default();

    let indices = mmr::mmr_rerank(&items, &config);
    let reranked: Vec<&types::MmrItem> = indices
        .iter()
        .filter(|&&i| i < items.len())
        .map(|&i| &items[i])
        .collect();
    serde_json::to_string(&reranked).unwrap_or_else(|_| "[]".to_string())
}

// ---------------------------------------------------------------------------
// Query expansion
// ---------------------------------------------------------------------------

#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_extract_keywords(query: String) -> Vec<String> {
    query_expansion::extract_keywords(&query)
}

#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_is_query_stop_word(token: String) -> bool {
    query_expansion::is_query_stop_word_token(&token)
}

/// Returns JSON `ExpandedQuery`.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_expand_query_for_fts(query: String) -> String {
    let result = query_expansion::expand_query_for_fts(&query);
    serde_json::to_string(&result).unwrap_or_else(|_| "{}".to_string())
}

// ---------------------------------------------------------------------------
// Hybrid merge (composite pipeline)
// ---------------------------------------------------------------------------

/// Takes JSON `MergeParams`, returns JSON array of `MergedResult`.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_merge_hybrid_results(params_json: String) -> String {
    if params_json.len() > NAPI_MAX_INPUT_LEN {
        return r#"{"error":"input_too_large","detail":"params_json exceeds 16MB limit"}"#
            .to_string();
    }
    let params: types::MergeParams = match serde_json::from_str(&params_json) {
        Ok(v) => v,
        Err(e) => {
            return format!(
                r#"{{"error":"parse_failed","detail":"params_json: {}"}}"#,
                e.to_string().replace('"', "'")
            );
        }
    };
    let results = merge::merge_hybrid_results(&params);
    serde_json::to_string(&results).unwrap_or_else(|_| "[]".to_string())
}

// ---------------------------------------------------------------------------
// Jaccard / text similarity (exposed individually for testing)
// ---------------------------------------------------------------------------

#[cfg_attr(feature = "napi_binding", napi)]
pub fn memory_text_similarity(a: String, b: String) -> f64 {
    mmr::text_similarity(&a, &b)
}
