//! C FFI: Memory search primitives (SIMD cosine similarity, BM25, FTS, hybrid merge).

use crate::ffi_utils::*;

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
        crate::memory_search::cosine::cosine_similarity(a, b)
    }))
    .unwrap_or(0.0)
}

/// C FFI: BM25 rank to score conversion.
#[no_mangle]
pub extern "C" fn deneb_memory_bm25_rank_to_score(rank: f64) -> f64 {
    crate::memory_search::bm25::bm25_rank_to_score(rank)
}

ffi_string_to_buffer!(
    /// C FFI: Build FTS query from raw text.
    /// Writes the query string to `out_ptr`. Returns bytes written, or 0 if no tokens.
    ///
    /// # Safety
    /// `raw_ptr` must be valid UTF-8. `out_ptr` must be writable for `out_len` bytes.
    fn deneb_memory_build_fts_query(
        raw_ptr,
        raw_len,
        out = (out_ptr, out_len),
        max_len = FFI_MAX_INPUT_LEN,
        raw_str,
        out_slice
    ) {
        match crate::memory_search::fts::build_fts_query(raw_str) {
            Some(query) => ffi_write_bytes(out_slice, query.as_bytes()),
            None => 0,
        }
    }
);

ffi_string_to_buffer!(
    /// C FFI: Merge hybrid search results.
    /// Takes JSON `MergeParams`, writes JSON `MergedResult` array to output.
    /// Returns bytes written, or negative on error.
    ///
    /// # Safety
    /// `params_ptr` must be valid UTF-8 JSON. `out_ptr` must be writable.
    fn deneb_memory_merge_hybrid_results(
        params_ptr,
        params_len,
        out = (out_ptr, out_len),
        max_len = FFI_MAX_INPUT_LEN,
        params_str,
        out_slice
    ) {
        let params: crate::memory_search::types::MergeParams =
            match serde_json::from_str(params_str) {
                Ok(p) => p,
                Err(_) => return FFI_ERR_JSON_ERROR,
            };
        let results = crate::memory_search::merge::merge_hybrid_results(&params);
        ffi_write_json(out_slice, &results)
    }
);

ffi_string_to_buffer!(
    /// C FFI: Extract keywords from a query for FTS.
    /// Writes JSON string array to output. Returns bytes written.
    ///
    /// # Safety
    /// `query_ptr` must be valid UTF-8. `out_ptr` must be writable.
    fn deneb_memory_extract_keywords(
        query_ptr,
        query_len,
        out = (out_ptr, out_len),
        max_len = FFI_MAX_INPUT_LEN,
        query_str,
        out_slice
    ) {
        let keywords = crate::memory_search::query_expansion::extract_keywords(query_str);
        ffi_write_json(out_slice, &keywords)
    }
);
