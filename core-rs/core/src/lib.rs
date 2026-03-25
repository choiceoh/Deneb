//! Deneb Core — Rust implementation of performance-critical modules.
//!
//! This crate provides:
//! - Protocol frame validation (replacing AJV)
//! - Security verification primitives + ReDoS detection
//! - Media MIME detection, EXIF parsing, PNG encoding
//!
//! It exposes both a Rust API and a C FFI surface for integration
//! with Go (via CGo) and Node.js (via napi-rs).

#![deny(clippy::all)]

#[cfg(feature = "napi_binding")]
#[macro_use]
extern crate napi_derive;

// Phase 0: Core modules (C FFI + Rust API)
pub mod compaction;
pub mod context_engine;
pub mod markdown;
pub mod media;
pub mod memory_search;
pub mod parsing;
pub mod protocol;
pub mod security;

// Phase 1: napi-rs modules (Node.js native addon)
pub mod exif;
pub mod external_content;
pub mod mime_utils;
pub mod png;
pub mod safe_regex;

// ---------------------------------------------------------------------------
// C FFI exports (Phase 0 — used by Go via CGo)
// ---------------------------------------------------------------------------

/// Maximum input size for FFI string functions (16 MB).
/// Prevents DoS via pathologically large inputs.
const FFI_MAX_INPUT_LEN: usize = 16 * 1024 * 1024;

// FFI error code constants — used across all `extern "C"` functions.
// Positive values are function-specific; negative values are shared errors.
// These MUST stay in sync with gateway-go/internal/ffi/errors.go.
const FFI_ERR_NULL_PTR: i32 = -1;
const FFI_ERR_INVALID_UTF8: i32 = -2;
const FFI_ERR_OUTPUT_TOO_SMALL: i32 = -3;
const FFI_ERR_INPUT_TOO_LARGE: i32 = -4;
const FFI_ERR_JSON: i32 = -5;
const FFI_ERR_OVERFLOW: i32 = -6;
const FFI_ERR_VALIDATION: i32 = -7;
const FFI_ERR_PANIC: i32 = -99;

/// Wraps an FFI body in catch_unwind to prevent Rust panics from aborting
/// the Go process. Returns `panic_rc` if the closure panics.
///
/// # Safety
/// Callers must ensure the closure does not rely on invariants that could
/// be violated by unwinding. All FFI closures here operate on local data
/// only, so AssertUnwindSafe is safe.
fn ffi_catch(panic_rc: i32, f: impl FnOnce() -> i32) -> i32 {
    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(f)) {
        Ok(rc) => rc,
        Err(_) => panic_rc,
    }
}

/// C FFI: Validate a gateway frame (JSON bytes).
/// Returns 0 on success, negative error code on failure.
///
/// # Safety
/// `json_ptr` must point to a valid UTF-8 byte buffer of length `json_len`.
#[no_mangle]
pub unsafe extern "C" fn deneb_validate_frame(json_ptr: *const u8, json_len: usize) -> i32 {
    if json_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if json_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(json_ptr, json_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let json_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        match protocol::validate_frame(json_str) {
            Ok(_) => 0,
            Err(_) => FFI_ERR_VALIDATION,
        }
    })
}

/// C FFI: Constant-time secret comparison.
/// Returns 0 if equal, non-zero otherwise.
///
/// # Safety
/// Both pointers must be valid byte buffers of their respective lengths.
#[no_mangle]
pub unsafe extern "C" fn deneb_constant_time_eq(
    a_ptr: *const u8,
    a_len: usize,
    b_ptr: *const u8,
    b_len: usize,
) -> i32 {
    if a_ptr.is_null() || b_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let a = std::slice::from_raw_parts(a_ptr, a_len);
    let b = std::slice::from_raw_parts(b_ptr, b_len);
    if security::constant_time_eq(a, b) {
        0
    } else {
        1
    }
}

/// C FFI: Detect MIME type from a byte buffer.
/// Writes the MIME string into `out_ptr` (max `out_len` bytes).
/// Returns the number of bytes written, or negative on error.
///
/// # Safety
/// `data_ptr` must point to a valid buffer of `data_len` bytes.
/// `out_ptr` must point to a writable buffer of `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_detect_mime(
    data_ptr: *const u8,
    data_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if data_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let data = std::slice::from_raw_parts(data_ptr, data_len);
    let mime = media::detect_mime(data);
    let mime_bytes = mime.as_bytes();
    if mime_bytes.len() > out_len {
        return FFI_ERR_OUTPUT_TOO_SMALL;
    }
    std::ptr::copy_nonoverlapping(mime_bytes.as_ptr(), out_ptr, mime_bytes.len());
    mime_bytes.len() as i32
}

/// C FFI: Validate a session key string.
/// Returns 0 if valid, -1 if null pointer, -2 if invalid UTF-8, -3 if invalid key.
///
/// # Safety
/// `key_ptr` must point to a valid UTF-8 byte buffer of length `key_len`.
#[no_mangle]
pub unsafe extern "C" fn deneb_validate_session_key(key_ptr: *const u8, key_len: usize) -> i32 {
    if key_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if key_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(key_ptr, key_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let key_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        if security::is_valid_session_key(key_str) {
            0
        } else {
            -3
        }
    })
}

/// C FFI: Sanitize HTML in a string.
/// Writes the sanitized output into `out_ptr` (max `out_len` bytes).
/// Returns the number of bytes written, or negative on error.
///
/// # Safety
/// `input_ptr` must be valid UTF-8 of `input_len` bytes.
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_sanitize_html(
    input_ptr: *const u8,
    input_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if input_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if input_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(input_ptr, input_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let input_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let sanitized = security::sanitize_html(input_str);
        let bytes = sanitized.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OUTPUT_TOO_SMALL; // output buffer too small
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Check if a URL is safe (not targeting internal networks).
/// Returns 0 if safe, 1 if unsafe, negative on error.
///
/// # Safety
/// `url_ptr` must point to valid UTF-8 of `url_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_is_safe_url(url_ptr: *const u8, url_len: usize) -> i32 {
    if url_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    // URLs should not be extremely long; cap at 8 KB.
    if url_len > 8192 {
        return 1; // treat oversized URLs as unsafe
    }
    let slice = std::slice::from_raw_parts(url_ptr, url_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let url_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        if security::is_safe_url(url_str) {
            0
        } else {
            1
        }
    })
}

/// C FFI: Validate an error code string.
/// Returns 0 if valid, 1 if unknown, negative on error.
///
/// # Safety
/// `code_ptr` must point to valid UTF-8 of `code_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_validate_error_code(code_ptr: *const u8, code_len: usize) -> i32 {
    if code_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    // Cap input length for consistency with other FFI functions.
    if code_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(code_ptr, code_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let code_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        if protocol::error_codes::is_valid_error_code(code_str) {
            0
        } else {
            1
        }
    })
}

// ---------------------------------------------------------------------------
// Vega FFI exports (Phase 0 scaffolding — full implementation in Phase 1)
// ---------------------------------------------------------------------------

/// C FFI: Execute a Vega command.
/// Takes a JSON command string `{"command":"search","args":{...}}`,
/// writes JSON result to output buffer.
/// Returns bytes written on success, negative on error.
///
/// When the `vega` feature is enabled, dispatches to deneb-vega command registry.
/// Otherwise returns a phase-0 stub response.
///
/// # Safety
/// `cmd_ptr` must point to valid UTF-8 of `cmd_len` bytes.
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_vega_execute(
    cmd_ptr: *const u8,
    cmd_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if cmd_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if cmd_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(cmd_ptr, cmd_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let cmd_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let result_json = vega_execute_impl(cmd_str);
        let result_bytes = result_json.as_bytes();
        if result_bytes.len() > out_slice.len() {
            return FFI_ERR_OUTPUT_TOO_SMALL;
        }
        out_slice[..result_bytes.len()].copy_from_slice(result_bytes);
        result_bytes.len() as i32
    })
}

/// Internal Vega execute dispatch.
#[cfg(feature = "vega")]
fn vega_execute_impl(cmd_json: &str) -> String {
    // Parse command JSON: {"command": "search", "args": {...}, "config": {...}}
    let parsed: serde_json::Value = match serde_json::from_str(cmd_json) {
        Ok(v) => v,
        Err(e) => return format!(r#"{{"error":"invalid_json","detail":"{}"}}"#, e),
    };

    let command = parsed
        .get("command")
        .and_then(|v| v.as_str())
        .unwrap_or("search");
    let args = parsed
        .get("args")
        .cloned()
        .unwrap_or(serde_json::Value::Null);

    // Build config from JSON or env (model paths are read from env by from_env())
    let config = if let Some(cfg) = parsed.get("config") {
        let mut vc = deneb_vega::config::VegaConfig::from_env();
        if let Some(p) = cfg.get("db_path").and_then(|v| v.as_str()) {
            vc.db_path = std::path::PathBuf::from(p);
        }
        if let Some(p) = cfg.get("md_dir").and_then(|v| v.as_str()) {
            vc.md_dir = std::path::PathBuf::from(p);
        }
        if let Some(m) = cfg.get("rerank_mode").and_then(|v| v.as_str()) {
            vc.rerank_mode = m.to_string();
        }
        if let Some(p) = cfg.get("model_embedder").and_then(|v| v.as_str()) {
            vc.model_embedder = Some(std::path::PathBuf::from(p));
        }
        if let Some(p) = cfg.get("model_reranker").and_then(|v| v.as_str()) {
            vc.model_reranker = Some(std::path::PathBuf::from(p));
        }
        vc
    } else {
        deneb_vega::config::VegaConfig::from_env()
    };

    let result = deneb_vega::commands::execute(command, &args, &config);
    serde_json::to_string(&result)
        .unwrap_or_else(|e| format!(r#"{{"error":"serialize","detail":"{}"}}"#, e))
}

#[cfg(not(feature = "vega"))]
fn vega_execute_impl(_cmd_json: &str) -> String {
    r#"{"error":"vega_not_implemented","phase":0}"#.to_string()
}

/// C FFI: Execute a Vega search query.
/// Takes a JSON query string `{"query":"검색어","config":{...}}`,
/// writes JSON results to output buffer.
/// Returns bytes written on success, negative on error.
///
/// # Safety
/// `query_ptr` must point to valid UTF-8 of `query_len` bytes.
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_vega_search(
    query_ptr: *const u8,
    query_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if query_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if query_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(query_ptr, query_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let query_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let result_json = vega_search_impl(query_str);
        let result_bytes = result_json.as_bytes();
        if result_bytes.len() > out_slice.len() {
            return FFI_ERR_OUTPUT_TOO_SMALL;
        }
        out_slice[..result_bytes.len()].copy_from_slice(result_bytes);
        result_bytes.len() as i32
    })
}

/// Internal Vega search dispatch.
#[cfg(feature = "vega")]
fn vega_search_impl(query_json: &str) -> String {
    // Parse: {"query": "검색어", "config": {"db_path": "..."}}
    let parsed: serde_json::Value = match serde_json::from_str(query_json) {
        Ok(v) => v,
        Err(_) => {
            // Treat raw string as direct query text
            return vega_search_direct(query_json);
        }
    };

    let query = parsed
        .get("query")
        .and_then(|v| v.as_str())
        .unwrap_or(query_json);

    let config = if let Some(cfg) = parsed.get("config") {
        let mut vc = deneb_vega::config::VegaConfig::from_env();
        if let Some(p) = cfg.get("db_path").and_then(|v| v.as_str()) {
            vc.db_path = std::path::PathBuf::from(p);
        }
        if let Some(p) = cfg.get("md_dir").and_then(|v| v.as_str()) {
            vc.md_dir = std::path::PathBuf::from(p);
        }
        vc
    } else {
        deneb_vega::config::VegaConfig::from_env()
    };

    vega_search_with_config(query, &config)
}

#[cfg(feature = "vega")]
fn vega_search_direct(query: &str) -> String {
    let config = deneb_vega::config::VegaConfig::from_env();
    vega_search_with_config(query, &config)
}

#[cfg(feature = "vega")]
fn vega_search_with_config(query: &str, config: &deneb_vega::config::VegaConfig) -> String {
    let router = deneb_vega::search::SearchRouter::new(config.clone());
    match router.search(query) {
        Ok(result) => serde_json::to_string(&result)
            .unwrap_or_else(|e| format!(r#"{{"error":"serialize","detail":"{}"}}"#, e)),
        Err(e) => format!(r#"{{"error":"search_failed","detail":"{}"}}"#, e),
    }
}

#[cfg(not(feature = "vega"))]
fn vega_search_impl(_query_json: &str) -> String {
    r#"{"results":[],"phase":0}"#.to_string()
}

// ---------------------------------------------------------------------------
// ML FFI exports — delegates to deneb-ml when the `ml` feature is enabled.
// Without it, returns a JSON error indicating the backend is unavailable.
// ---------------------------------------------------------------------------

/// JSON request for the embed FFI.
#[derive(serde::Deserialize)]
#[allow(dead_code)]
struct EmbedRequest {
    texts: Vec<String>,
}

/// JSON request for the rerank FFI.
#[derive(serde::Deserialize)]
#[allow(dead_code)]
struct RerankRequest {
    query: String,
    documents: Vec<String>,
}

/// Write a JSON response to the output buffer. Returns bytes written or -3 if
/// the buffer is too small.
fn write_json_response(out_slice: &mut [u8], value: &impl serde::Serialize) -> i32 {
    match serde_json::to_vec(value) {
        Ok(bytes) => {
            if bytes.len() > out_slice.len() {
                return FFI_ERR_OUTPUT_TOO_SMALL;
            }
            out_slice[..bytes.len()].copy_from_slice(&bytes);
            bytes.len() as i32
        }
        Err(_) => FFI_ERR_JSON,
    }
}

/// C FFI: Generate text embeddings.
/// Takes a JSON request (`{"texts":["..."]}`), writes JSON result to output buffer.
/// Returns bytes written on success, negative on error.
///
/// When the `ml` feature is enabled, uses deneb-ml for real inference.
/// Otherwise returns `{"error":"ml backend unavailable"}`.
///
/// # Safety
/// `input_ptr` must point to valid UTF-8 of `input_len` bytes.
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_ml_embed(
    input_ptr: *const u8,
    input_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if input_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if input_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(input_ptr, input_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let input_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };

        #[cfg(feature = "ml")]
        {
            let req: EmbedRequest = match serde_json::from_str(input_str) {
                Ok(r) => r,
                Err(_) => return FFI_ERR_INVALID_UTF8,
            };
            let text_refs: Vec<&str> = req.texts.iter().map(|s| s.as_str()).collect();

            // Build manager from env config (reuses VegaConfig TTL defaults).
            let mgr = ml_manager_from_env();
            let embedder = deneb_ml::LocalEmbedder::new(mgr);

            match embedder.embed(&text_refs) {
                Ok(result) => {
                    let response = serde_json::json!({
                        "embeddings": result.vectors,
                        "dim": result.dim,
                    });
                    write_json_response(out_slice, &response)
                }
                Err(e) => {
                    let response = serde_json::json!({"error": e.to_string()});
                    write_json_response(out_slice, &response)
                }
            }
        }

        #[cfg(not(feature = "ml"))]
        {
            let _ = input_str;
            let response = serde_json::json!({"error": "ml backend unavailable"});
            write_json_response(out_slice, &response)
        }
    })
}

/// C FFI: Rerank documents against a query.
/// Takes a JSON request (`{"query":"...","documents":["..."]}`), writes JSON ranked results.
/// Returns bytes written on success, negative on error.
///
/// # Safety
/// `input_ptr` must point to valid UTF-8 of `input_len` bytes.
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_ml_rerank(
    input_ptr: *const u8,
    input_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if input_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if input_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(input_ptr, input_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let input_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };

        #[cfg(feature = "ml")]
        {
            let req: RerankRequest = match serde_json::from_str(input_str) {
                Ok(r) => r,
                Err(_) => return FFI_ERR_INVALID_UTF8,
            };
            let doc_refs: Vec<&str> = req.documents.iter().map(|s| s.as_str()).collect();

            let mgr = ml_manager_from_env();
            let reranker = deneb_ml::LocalReranker::new(mgr);

            match reranker.rerank(&req.query, &doc_refs) {
                Ok(results) => {
                    let ranked: Vec<serde_json::Value> = results
                        .iter()
                        .map(|r| serde_json::json!({"index": r.index, "score": r.score}))
                        .collect();
                    let response = serde_json::json!({"ranked": ranked});
                    write_json_response(out_slice, &response)
                }
                Err(e) => {
                    let response = serde_json::json!({"error": e.to_string()});
                    write_json_response(out_slice, &response)
                }
            }
        }

        #[cfg(not(feature = "ml"))]
        {
            let _ = input_str;
            let response = serde_json::json!({"error": "ml backend unavailable"});
            write_json_response(out_slice, &response)
        }
    })
}

/// Build an ML ModelManager from environment variables.
/// Uses the same env vars as VegaConfig + model path env vars.
#[cfg(feature = "ml")]
fn ml_manager_from_env() -> deneb_ml::ModelManager {
    use deneb_ml::{ModelConfig, ModelManager};
    use std::path::PathBuf;

    let ttl: u64 = std::env::var("VEGA_MODEL_TTL")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(300);

    let mut configs = Vec::new();

    if let Ok(path) = std::env::var("VEGA_MODEL_EMBEDDER") {
        configs.push(ModelConfig::embedder(PathBuf::from(path), ttl));
    }
    if let Ok(path) = std::env::var("VEGA_MODEL_RERANKER") {
        configs.push(ModelConfig::reranker(PathBuf::from(path), ttl));
    }
    if let Ok(path) = std::env::var("VEGA_MODEL_EXPANDER") {
        configs.push(ModelConfig::expander(PathBuf::from(path), ttl));
    }

    ModelManager::new(configs)
}

// ---------------------------------------------------------------------------
// Protocol schema validation FFI (validates RPC parameters in Rust)
// ---------------------------------------------------------------------------

/// C FFI: Validate RPC parameters for a given method name.
/// Returns 0 if valid, positive N = bytes written to `errors_out` (JSON error array),
/// negative on error (-1 = null ptr, -2 = invalid UTF-8, -3 = unknown method,
/// -4 = input too large, -5 = invalid JSON).
///
/// # Safety
/// All pointers must be valid for their respective lengths.
/// `errors_out` must be writable for `errors_out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_validate_params(
    method_ptr: *const u8,
    method_len: usize,
    json_ptr: *const u8,
    json_len: usize,
    errors_out: *mut u8,
    errors_out_len: usize,
) -> i32 {
    if method_ptr.is_null() || json_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    // Cap both method name and JSON payload lengths.
    if json_len > FFI_MAX_INPUT_LEN || method_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let method_slice = std::slice::from_raw_parts(method_ptr, method_len);
    let json_slice = std::slice::from_raw_parts(json_ptr, json_len);

    ffi_catch(FFI_ERR_PANIC, move || {
        let method_str = match std::str::from_utf8(method_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let json_str = match std::str::from_utf8(json_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };

        match protocol::validation::validate_params(method_str, json_str) {
            Ok(result) => {
                if result.valid {
                    0
                } else {
                    // Write errors as JSON to output buffer.
                    // Returns the TOTAL required bytes (snprintf convention) so
                    // callers can detect truncation: if retval > errors_out_len,
                    // the output was truncated.
                    let json_bytes = serde_json::to_vec(&result.errors).unwrap_or_default();
                    let total_len = json_bytes.len();
                    if !errors_out.is_null() && !json_bytes.is_empty() {
                        let write_len = total_len.min(errors_out_len);
                        let out = std::slice::from_raw_parts_mut(errors_out, errors_out_len);
                        out[..write_len].copy_from_slice(&json_bytes[..write_len]);
                    }
                    // Always return total required bytes so caller can detect
                    // truncation (total > buffer size) or no-buffer case.
                    total_len.max(1) as i32
                }
            }
            Err(protocol::validation::ValidateParamsError::UnknownMethod(_)) => FFI_ERR_VALIDATION,
            Err(protocol::validation::ValidateParamsError::InvalidJson(_)) => FFI_ERR_JSON,
        }
    })
}

// ---------------------------------------------------------------------------
// Compaction FFI exports (context compression engine)
// ---------------------------------------------------------------------------

/// C FFI: Evaluate whether compaction is needed.
/// Writes JSON `CompactionDecision` into `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `config_ptr` must be valid UTF-8 JSON of `config_len` bytes.
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_compaction_evaluate(
    config_ptr: *const u8,
    config_len: usize,
    stored_tokens: u64,
    live_tokens: u64,
    token_budget: u64,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if config_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if config_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let config_slice = std::slice::from_raw_parts(config_ptr, config_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let config_str = match std::str::from_utf8(config_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let config: compaction::CompactionConfig = match serde_json::from_str(config_str) {
            Ok(c) => c,
            Err(_) => return FFI_ERR_JSON,
        };
        let decision = compaction::evaluate(&config, stored_tokens, live_tokens, token_budget);
        let json = match serde_json::to_string(&decision) {
            Ok(j) => j,
            Err(_) => return FFI_ERR_JSON,
        };
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Create a new compaction sweep engine.
/// Returns a positive handle on success, negative on error.
///
/// # Safety
/// `config_ptr` must be valid UTF-8 JSON of `config_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_compaction_sweep_new(
    config_ptr: *const u8,
    config_len: usize,
    conversation_id: u64,
    token_budget: u64,
    force: i32,
    hard_trigger: i32,
    now_ms: i64,
) -> i64 {
    if config_ptr.is_null() {
        return FFI_ERR_NULL_PTR as i64;
    }
    if config_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE as i64;
    }
    let config_slice = std::slice::from_raw_parts(config_ptr, config_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let config_str = match std::str::from_utf8(config_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        // Validate u64→u32 narrowing to prevent silent truncation.
        if conversation_id > u32::MAX as u64 || token_budget > u32::MAX as u64 {
            return FFI_ERR_INPUT_TOO_LARGE;
        }
        let handle = compaction::napi::compaction_sweep_new(
            config_str.to_string(),
            conversation_id as u32,
            token_budget as u32,
            force != 0,
            hard_trigger != 0,
            now_ms as f64,
        );
        // Validate handle fits in i32 to prevent truncation in i32→i64 cast chain.
        if handle > i32::MAX as u32 {
            return FFI_ERR_OVERFLOW;
        }
        handle as i32
    }) as i64
}

/// C FFI: Start a sweep engine. Writes first SweepCommand JSON to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_compaction_sweep_start(
    handle: u32,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let json = compaction::napi::compaction_sweep_start(handle);
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Step a sweep engine with a response. Writes next SweepCommand JSON.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `resp_ptr` must be valid UTF-8 JSON. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_compaction_sweep_step(
    handle: u32,
    resp_ptr: *const u8,
    resp_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if resp_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if resp_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let resp_slice = std::slice::from_raw_parts(resp_ptr, resp_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let resp_str = match std::str::from_utf8(resp_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let json = compaction::napi::compaction_sweep_step(handle, resp_str.to_string());
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Drop a sweep engine, freeing its resources.
#[no_mangle]
pub extern "C" fn deneb_compaction_sweep_drop(handle: u32) {
    compaction::napi::compaction_sweep_drop(handle);
}

// ---------------------------------------------------------------------------
// Memory search FFI exports
// ---------------------------------------------------------------------------

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
    if raw_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if raw_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(raw_ptr, raw_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let raw_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        match memory_search::fts::build_fts_query(raw_str) {
            Some(query) => {
                let bytes = query.as_bytes();
                if bytes.len() > out_slice.len() {
                    return FFI_ERR_OUTPUT_TOO_SMALL;
                }
                out_slice[..bytes.len()].copy_from_slice(bytes);
                bytes.len() as i32
            }
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
    if params_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if params_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(params_ptr, params_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let params_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let params: memory_search::types::MergeParams = match serde_json::from_str(params_str) {
            Ok(p) => p,
            Err(_) => return FFI_ERR_JSON,
        };
        let results = memory_search::merge::merge_hybrid_results(&params);
        let json = match serde_json::to_string(&results) {
            Ok(j) => j,
            Err(_) => return FFI_ERR_JSON,
        };
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
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
    if query_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if query_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(query_ptr, query_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let query_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let keywords = memory_search::query_expansion::extract_keywords(query_str);
        let json = match serde_json::to_string(&keywords) {
            Ok(j) => j,
            Err(_) => return FFI_ERR_JSON,
        };
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

// ---------------------------------------------------------------------------
// Parsing FFI exports (pre-LLM heavy parsing for Go gateway)
// ---------------------------------------------------------------------------

/// C FFI: Extract links from message text.
/// Takes the message text and a JSON config `{"max_links": N}`.
/// Writes a JSON array of URL strings to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// All pointers must be valid for their respective lengths.
#[no_mangle]
pub unsafe extern "C" fn deneb_extract_links(
    text_ptr: *const u8,
    text_len: usize,
    config_ptr: *const u8,
    config_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if text_ptr.is_null() || config_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if text_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let text_slice = std::slice::from_raw_parts(text_ptr, text_len);
    let config_slice = std::slice::from_raw_parts(config_ptr, config_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let text_str = match std::str::from_utf8(text_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let config_str = match std::str::from_utf8(config_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };

        #[derive(serde::Deserialize)]
        struct ConfigInput {
            #[serde(default = "default_max_links")]
            max_links: usize,
        }
        fn default_max_links() -> usize {
            5
        }

        let config: ConfigInput = match serde_json::from_str(config_str) {
            Ok(c) => c,
            Err(_) => return FFI_ERR_JSON,
        };
        let cfg = parsing::url_extract::ExtractLinksConfig {
            max_links: config.max_links,
        };
        let urls = parsing::url_extract::extract_links(text_str, &cfg);
        let json = match serde_json::to_string(&urls) {
            Ok(j) => j,
            Err(_) => return FFI_ERR_JSON,
        };
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Convert HTML to Markdown.
/// Writes JSON `{"text":"...","title":"..."}` to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `html_ptr` must be valid UTF-8. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_html_to_markdown(
    html_ptr: *const u8,
    html_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if html_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if html_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let html_slice = std::slice::from_raw_parts(html_ptr, html_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let html_str = match std::str::from_utf8(html_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let result = parsing::html_to_markdown::html_to_markdown(html_str);
        let json = match serde_json::to_string(&result) {
            Ok(j) => j,
            Err(_) => return FFI_ERR_JSON,
        };
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Estimate decoded size of a base64 string.
/// Returns estimated byte count (>= 0) on success, negative on error.
///
/// # Safety
/// `input_ptr` must be valid UTF-8.
#[no_mangle]
pub unsafe extern "C" fn deneb_base64_estimate(input_ptr: *const u8, input_len: usize) -> i64 {
    if input_ptr.is_null() {
        return FFI_ERR_NULL_PTR as i64;
    }
    if input_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE as i64;
    }
    let slice = std::slice::from_raw_parts(input_ptr, input_len);
    std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        let input_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8 as i64,
        };
        parsing::base64_util::estimate_base64_decoded_bytes(input_str) as i64
    }))
    .unwrap_or(FFI_ERR_PANIC as i64)
}

/// C FFI: Canonicalize a base64 string (strip whitespace, validate).
/// Writes the canonical base64 string to `out_ptr`.
/// Returns bytes written on success, -3 if invalid, other negatives on error.
///
/// # Safety
/// `input_ptr` must be valid UTF-8. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_base64_canonicalize(
    input_ptr: *const u8,
    input_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if input_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if input_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let slice = std::slice::from_raw_parts(input_ptr, input_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let input_str = match std::str::from_utf8(slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        match parsing::base64_util::canonicalize_base64(input_str) {
            Some(canonical) => {
                let bytes = canonical.as_bytes();
                if bytes.len() > out_slice.len() {
                    return FFI_ERR_OVERFLOW;
                }
                out_slice[..bytes.len()].copy_from_slice(bytes);
                bytes.len() as i32
            }
            None => FFI_ERR_VALIDATION, // invalid base64
        }
    })
}

/// C FFI: Parse MEDIA: tokens from text output.
/// Writes JSON `{"text":"...","media_urls":[...],"audio_as_voice":bool}` to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `text_ptr` must be valid UTF-8. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_parse_media_tokens(
    text_ptr: *const u8,
    text_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if text_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if text_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let text_slice = std::slice::from_raw_parts(text_ptr, text_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let text_str = match std::str::from_utf8(text_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let result = parsing::media_tokens::split_media_from_output(text_str);
        let json = match serde_json::to_string(&result) {
            Ok(j) => j,
            Err(_) => return FFI_ERR_JSON,
        };
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

// ---------------------------------------------------------------------------
// Markdown FFI exports (IR parsing for Go gateway)
// ---------------------------------------------------------------------------

/// C FFI: Parse markdown text into a MarkdownIR structure.
/// Takes markdown text and an optional JSON options string.
/// Writes JSON `{"text":"...","styles":[...],"links":[...],"has_code_blocks":bool}` to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `md_ptr` must be valid UTF-8. `out_ptr` must be writable for `out_len` bytes.
/// `opts_ptr` may be null for default options.
#[no_mangle]
pub unsafe extern "C" fn deneb_markdown_to_ir(
    md_ptr: *const u8,
    md_len: usize,
    opts_ptr: *const u8,
    opts_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if md_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if md_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let md_slice = std::slice::from_raw_parts(md_ptr, md_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let md_str = match std::str::from_utf8(md_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let options = if !opts_ptr.is_null() && opts_len > 0 {
            let opts_bytes = std::slice::from_raw_parts(opts_ptr, opts_len);
            match std::str::from_utf8(opts_bytes) {
                Ok(s) => match serde_json::from_str::<markdown::parser::ParseOptions>(s) {
                    Ok(o) => o,
                    Err(_) => return FFI_ERR_JSON,
                },
                Err(_) => return FFI_ERR_INVALID_UTF8,
            }
        } else {
            markdown::parser::ParseOptions::default()
        };
        let (ir, has_tables) = markdown::parser::markdown_to_ir_with_meta(md_str, &options);
        let has_code_blocks = ir
            .styles
            .iter()
            .any(|s| s.style == markdown::spans::MarkdownStyle::CodeBlock);
        #[derive(serde::Serialize)]
        struct IrOutput<'a> {
            text: &'a str,
            styles: &'a [markdown::spans::StyleSpan],
            links: &'a [markdown::spans::LinkSpan],
            has_code_blocks: bool,
            has_tables: bool,
        }
        let output = IrOutput {
            text: &ir.text,
            styles: &ir.styles,
            links: &ir.links,
            has_code_blocks,
            has_tables,
        };
        let json = match serde_json::to_string(&output) {
            Ok(j) => j,
            Err(_) => return FFI_ERR_JSON,
        };
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Detect fenced code blocks in text.
/// Writes JSON array of fence block objects to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `text_ptr` must be valid UTF-8. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_markdown_detect_fences(
    text_ptr: *const u8,
    text_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if text_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if text_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let text_slice = std::slice::from_raw_parts(text_ptr, text_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let text_str = match std::str::from_utf8(text_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let fences = markdown::fences::parse_fence_spans(text_str);
        let json = match serde_json::to_string(&fences) {
            Ok(j) => j,
            Err(_) => return FFI_ERR_JSON,
        };
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

// ---------------------------------------------------------------------------
// C FFI exports — Context Engine
// ---------------------------------------------------------------------------

/// C FFI: Create a new context assembly engine.
/// Returns a positive handle on success.
#[no_mangle]
pub extern "C" fn deneb_context_assembly_new(
    conversation_id: u64,
    token_budget: u64,
    fresh_tail_count: u32,
) -> u32 {
    context_engine::napi::context_assembly_new(
        conversation_id as u32,
        token_budget as u32,
        fresh_tail_count,
    )
}

/// C FFI: Start an assembly engine. Writes first AssemblyCommand JSON to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_context_assembly_start(
    handle: u32,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let json = context_engine::napi::context_assembly_start(handle);
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Step an assembly engine with a host response.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `resp_ptr` must be valid UTF-8 JSON. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_context_assembly_step(
    handle: u32,
    resp_ptr: *const u8,
    resp_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if resp_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if resp_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let resp_slice = std::slice::from_raw_parts(resp_ptr, resp_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let resp_str = match std::str::from_utf8(resp_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let json = context_engine::napi::context_assembly_step(handle, resp_str.to_string());
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Create a new context expand engine.
/// Returns a positive handle on success.
///
/// # Safety
/// `summary_id_ptr` must be valid UTF-8.
#[no_mangle]
pub unsafe extern "C" fn deneb_context_expand_new(
    summary_id_ptr: *const u8,
    summary_id_len: usize,
    max_depth: u32,
    include_messages: i32,
    token_cap: u64,
) -> u32 {
    if summary_id_ptr.is_null() {
        return 0;
    }
    let slice = std::slice::from_raw_parts(summary_id_ptr, summary_id_len);
    let summary_id = match std::str::from_utf8(slice) {
        Ok(s) => s.to_string(),
        Err(_) => return 0,
    };
    context_engine::napi::context_expand_new(
        summary_id,
        max_depth,
        include_messages != 0,
        token_cap as u32,
    )
}

/// C FFI: Start an expand engine. Writes first RetrievalCommand JSON to `out_ptr`.
///
/// # Safety
/// `out_ptr` must be writable for `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_context_expand_start(
    handle: u32,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let json = context_engine::napi::context_expand_start(handle);
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Step an expand engine with a host response.
///
/// # Safety
/// `resp_ptr` must be valid UTF-8 JSON. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_context_expand_step(
    handle: u32,
    resp_ptr: *const u8,
    resp_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if resp_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if resp_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    let resp_slice = std::slice::from_raw_parts(resp_ptr, resp_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let resp_str = match std::str::from_utf8(resp_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let json = context_engine::napi::context_expand_step(handle, resp_str.to_string());
        let bytes = json.as_bytes();
        if bytes.len() > out_slice.len() {
            return FFI_ERR_OVERFLOW;
        }
        out_slice[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
}

/// C FFI: Drop any context engine, freeing its resources.
#[no_mangle]
pub extern "C" fn deneb_context_engine_drop(handle: u32) {
    context_engine::napi::context_engine_drop(handle);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_validate_frame_valid_request() {
        let json = r#"{"type":"req","id":"1","method":"chat.send"}"#;
        let result = unsafe { deneb_validate_frame(json.as_ptr(), json.len()) };
        assert_eq!(result, 0);
    }

    #[test]
    fn test_validate_frame_invalid() {
        let json = r#"{"type":"unknown"}"#;
        let result = unsafe { deneb_validate_frame(json.as_ptr(), json.len()) };
        assert!(result < 0);
    }

    #[test]
    fn test_constant_time_eq() {
        let a = b"secret123";
        let b = b"secret123";
        let c = b"different";
        assert_eq!(
            unsafe { deneb_constant_time_eq(a.as_ptr(), a.len(), b.as_ptr(), b.len()) },
            0
        );
        assert_ne!(
            unsafe { deneb_constant_time_eq(a.as_ptr(), a.len(), c.as_ptr(), c.len()) },
            0
        );
    }

    #[test]
    fn test_validate_session_key_valid() {
        let key = "my-session-123";
        let result = unsafe { deneb_validate_session_key(key.as_ptr(), key.len()) };
        assert_eq!(result, 0);
    }

    #[test]
    fn test_validate_session_key_empty() {
        let key = "";
        let result = unsafe { deneb_validate_session_key(key.as_ptr(), key.len()) };
        assert_eq!(result, -3);
    }

    #[test]
    fn test_sanitize_html_ffi() {
        let input = "<b>hi</b>";
        let mut out = [0u8; 256];
        let len = unsafe {
            deneb_sanitize_html(input.as_ptr(), input.len(), out.as_mut_ptr(), out.len())
        };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize]).unwrap();
        assert_eq!(result, "&lt;b&gt;hi&lt;/b&gt;");
    }

    #[test]
    fn test_is_safe_url_ffi() {
        let safe = "https://example.com";
        assert_eq!(unsafe { deneb_is_safe_url(safe.as_ptr(), safe.len()) }, 0);

        let unsafe_url = "http://localhost/admin";
        assert_eq!(
            unsafe { deneb_is_safe_url(unsafe_url.as_ptr(), unsafe_url.len()) },
            1
        );
    }

    #[test]
    fn test_validate_error_code_ffi() {
        let valid = "NOT_FOUND";
        assert_eq!(
            unsafe { deneb_validate_error_code(valid.as_ptr(), valid.len()) },
            0
        );

        let invalid = "BOGUS";
        assert_eq!(
            unsafe { deneb_validate_error_code(invalid.as_ptr(), invalid.len()) },
            1
        );
    }

    #[test]
    fn test_detect_mime() {
        let png = [0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A];
        let mut out = [0u8; 64];
        let len =
            unsafe { deneb_detect_mime(png.as_ptr(), png.len(), out.as_mut_ptr(), out.len()) };
        assert!(len > 0);
        let mime = std::str::from_utf8(&out[..len as usize]).unwrap();
        assert_eq!(mime, "image/png");
    }

    #[test]
    fn test_vega_execute_stub() {
        let cmd = r#"{"command":"search","query":"test"}"#;
        let mut out = [0u8; 256];
        let len =
            unsafe { deneb_vega_execute(cmd.as_ptr(), cmd.len(), out.as_mut_ptr(), out.len()) };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize]).unwrap();
        assert!(result.contains("vega_not_implemented"));
    }

    #[test]
    fn test_vega_search_stub() {
        let query = r#"{"query":"test"}"#;
        let mut out = [0u8; 256];
        let len =
            unsafe { deneb_vega_search(query.as_ptr(), query.len(), out.as_mut_ptr(), out.len()) };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize]).unwrap();
        assert!(result.contains("results"));
    }

    #[test]
    fn test_ml_embed_stub() {
        let input = r#"{"texts":["hello world"]}"#;
        let mut out = [0u8; 256];
        let len =
            unsafe { deneb_ml_embed(input.as_ptr(), input.len(), out.as_mut_ptr(), out.len()) };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize]).unwrap();
        // With ml feature: returns embeddings; without: returns error stub.
        assert!(
            result.contains("embeddings") || result.contains("ml backend unavailable"),
            "unexpected embed response: {result}"
        );
    }

    #[test]
    fn test_ml_rerank_stub() {
        let input = r#"{"query":"test","documents":["doc1","doc2"]}"#;
        let mut out = [0u8; 256];
        let len =
            unsafe { deneb_ml_rerank(input.as_ptr(), input.len(), out.as_mut_ptr(), out.len()) };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize]).unwrap();
        // With ml feature: returns ranked results; without: returns error stub.
        assert!(
            result.contains("ranked") || result.contains("ml backend unavailable"),
            "unexpected rerank response: {result}"
        );
    }

    #[test]
    fn test_extract_links_ffi() {
        let text = "Check https://example.com and https://rust-lang.org please";
        let config = r#"{"max_links":5}"#;
        let mut out = [0u8; 1024];
        let len = unsafe {
            deneb_extract_links(
                text.as_ptr(),
                text.len(),
                config.as_ptr(),
                config.len(),
                out.as_mut_ptr(),
                out.len(),
            )
        };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize]).unwrap();
        assert!(result.contains("https://example.com"));
        assert!(result.contains("https://rust-lang.org"));
    }

    #[test]
    fn test_html_to_markdown_ffi() {
        let html =
            "<html><head><title>Test</title></head><body><h1>Hello</h1><p>World</p></body></html>";
        let mut out = [0u8; 4096];
        let len = unsafe {
            deneb_html_to_markdown(html.as_ptr(), html.len(), out.as_mut_ptr(), out.len())
        };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize]).unwrap();
        assert!(result.contains("Hello"));
        assert!(result.contains("World"));
        assert!(result.contains("Test"));
    }

    #[test]
    fn test_base64_estimate_ffi() {
        let input = "AAAA";
        let result = unsafe { deneb_base64_estimate(input.as_ptr(), input.len()) };
        assert_eq!(result, 3);
    }

    #[test]
    fn test_base64_canonicalize_ffi() {
        let input = " A A A A ";
        let mut out = [0u8; 256];
        let len = unsafe {
            deneb_base64_canonicalize(input.as_ptr(), input.len(), out.as_mut_ptr(), out.len())
        };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize]).unwrap();
        assert_eq!(result, "AAAA");
    }

    #[test]
    fn test_base64_canonicalize_invalid() {
        let input = "AAA"; // not multiple of 4
        let mut out = [0u8; 256];
        let len = unsafe {
            deneb_base64_canonicalize(input.as_ptr(), input.len(), out.as_mut_ptr(), out.len())
        };
        assert_eq!(len, -3); // invalid
    }

    #[test]
    fn test_parse_media_tokens_ffi() {
        let text = "Here is output\nMEDIA: https://example.com/img.png\nDone.";
        let mut out = [0u8; 4096];
        let len = unsafe {
            deneb_parse_media_tokens(text.as_ptr(), text.len(), out.as_mut_ptr(), out.len())
        };
        assert!(len > 0);
        let result = std::str::from_utf8(&out[..len as usize]).unwrap();
        assert!(result.contains("https://example.com/img.png"));
        assert!(result.contains("media_urls"));
    }
}
