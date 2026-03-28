//! Memory search FFI exports (cosine similarity, BM25, FTS, hybrid merge, keywords).

#![allow(unsafe_code)]

use super::helpers::{ffi_read_str, ffi_write_bytes, ffi_write_json};
use super::*;
use crate::memory_search;

/// C FFI: Cosine similarity between two f64 vectors.
/// Returns the similarity value [-1.0, 1.0], or 0.0 on error.
///
/// # Safety
/// `a_ptr` and `b_ptr` must point to valid f64 arrays of their respective lengths.
#[no_mangle]
pub unsafe extern "C" fn deneb_memory_cosine_similarity(
    a_ptr: *const f64,
    a_len: usize,
    b_ptr: *const f64,
    b_len: usize,
) -> f64 {
    if a_ptr.is_null() || b_ptr.is_null() {
        return 0.0;
    }
    // Cap at 2M elements (16 MB per vector) to prevent DoS
    const MAX_VEC_LEN: usize = 2 * 1024 * 1024;
    if a_len > MAX_VEC_LEN || b_len > MAX_VEC_LEN {
        return 0.0;
    }
    // SAFETY: a_ptr and b_ptr are null-checked above, lengths capped at MAX_VEC_LEN.
    let a = std::slice::from_raw_parts(a_ptr, a_len);
    let b = std::slice::from_raw_parts(b_ptr, b_len);
    std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        memory_search::cosine::cosine_similarity(a, b)
    }))
    .unwrap_or(0.0)
}

/// C FFI: BM25 rank to score conversion.
#[no_mangle]
pub extern "C" fn deneb_memory_bm25_rank_to_score(rank: f64) -> f64 {
    memory_search::bm25::bm25_rank_to_score(rank)
}

/// C FFI: Build FTS query from raw text.
/// Writes the query string to `out_ptr`. Returns bytes written, or 0 if no tokens.
///
/// # Safety
/// `raw_ptr` must be valid UTF-8. `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_memory_build_fts_query(
    raw_ptr: *const u8,
    raw_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let str_result = ffi_read_str(raw_ptr, raw_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let raw_str = match str_result {
            Ok(s) => s,
            Err(e) => return e,
        };
        match memory_search::fts::build_fts_query(raw_str) {
            Some(query) => ffi_write_bytes(query.as_bytes(), out_slice),
            None => 0,
        }
    })
}

/// C FFI: Merge hybrid search results.
/// Takes JSON MergeParams, writes JSON MergedResult array to output.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `params_ptr` must be valid UTF-8 JSON. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_memory_merge_hybrid_results(
    params_ptr: *const u8,
    params_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let str_result = ffi_read_str(params_ptr, params_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let params_str = match str_result {
            Ok(s) => s,
            Err(e) => return e,
        };
        let params: memory_search::types::MergeParams = match serde_json::from_str(params_str) {
            Ok(p) => p,
            Err(_) => return FFI_ERR_JSON,
        };
        let results = memory_search::merge::merge_hybrid_results(&params);
        ffi_write_json(&results, out_slice)
    })
}

/// C FFI: Extract keywords from a query for FTS.
/// Writes JSON string array to output. Returns bytes written.
///
/// # Safety
/// `query_ptr` must be valid UTF-8. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_memory_extract_keywords(
    query_ptr: *const u8,
    query_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let str_result = ffi_read_str(query_ptr, query_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let query_str = match str_result {
            Ok(s) => s,
            Err(e) => return e,
        };
        let keywords = memory_search::query_expansion::extract_keywords(query_str);
        ffi_write_json(&keywords, out_slice)
    })
}
