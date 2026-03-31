//! C FFI: Vega FTS/semantic search commands (requires `vega` feature).

use crate::ffi_utils::*;

ffi_string_to_buffer!(
    /// C FFI: Execute a Vega command.
    /// Takes a JSON command string `{"command":"search","args":{...}}`,
    /// writes JSON result to output buffer.
    /// Returns bytes written on success, negative on error.
    ///
    /// When the `vega` feature is enabled, dispatches to deneb-vega command registry.
    /// Otherwise returns `{"error":"vega_not_implemented","phase":0}`.
    ///
    /// # Safety
    /// `cmd_ptr` must point to valid UTF-8 of `cmd_len` bytes.
    /// `out_ptr` must be writable for `out_len` bytes.
    fn deneb_vega_execute(
        cmd_ptr,
        cmd_len,
        out = (out_ptr, out_len),
        max_len = FFI_MAX_INPUT_LEN,
        cmd_str,
        out_slice
    ) {
        let result_json = vega_execute_impl(cmd_str);
        ffi_write_bytes(out_slice, result_json.as_bytes())
    }
);

ffi_string_to_buffer!(
    /// C FFI: Execute a Vega search query.
    /// Takes a JSON query string `{"query":"검색어","config":{...}}`,
    /// writes JSON results to output buffer.
    /// Returns bytes written on success, negative on error.
    ///
    /// # Safety
    /// `query_ptr` must point to valid UTF-8 of `query_len` bytes.
    /// `out_ptr` must be writable for `out_len` bytes.
    fn deneb_vega_search(
        query_ptr,
        query_len,
        out = (out_ptr, out_len),
        max_len = FFI_MAX_INPUT_LEN,
        query_str,
        out_slice
    ) {
        let result_json = vega_search_impl(query_str);
        ffi_write_bytes(out_slice, result_json.as_bytes())
    }
);

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

/// Cached VegaConfig to avoid reading 7+ env vars on every FFI call.
/// Initialized once on first use; env vars are stable at runtime.
#[cfg(feature = "vega")]
fn cached_vega_config() -> &'static deneb_vega::config::VegaConfig {
    use std::sync::OnceLock;
    static CONFIG: OnceLock<deneb_vega::config::VegaConfig> = OnceLock::new();
    CONFIG.get_or_init(deneb_vega::config::VegaConfig::from_env)
}

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

    // Use cached base config; apply per-call overrides only when provided.
    let base = cached_vega_config();
    let config = if let Some(cfg) = parsed.get("config") {
        let mut vc = base.clone();
        if let Some(p) = cfg.get("db_path").and_then(|v| v.as_str()) {
            vc.db_path = std::path::PathBuf::from(p);
        }
        if let Some(p) = cfg.get("md_dir").and_then(|v| v.as_str()) {
            vc.md_dir = std::path::PathBuf::from(p);
        }
        if let Some(m) = cfg.get("rerank_mode").and_then(|v| v.as_str()) {
            vc.rerank_mode = m.to_string();
        }
        // model_embedder and model_reranker removed — embeddings via SGLang HTTP.
        vc
    } else {
        base.clone()
    };

    let result = deneb_vega::commands::execute(command, &args, &config);
    serde_json::to_string(&result)
        .unwrap_or_else(|e| format!(r#"{{"error":"serialize","detail":"{}"}}"#, e))
}

#[cfg(not(feature = "vega"))]
fn vega_execute_impl(_cmd_json: &str) -> String {
    r#"{"error":"vega_not_implemented","phase":0}"#.to_string()
}

/// Internal Vega search dispatch.
/// Supports optional `query_embedding` field for SGLang-generated vectors.
#[cfg(feature = "vega")]
fn vega_search_impl(query_json: &str) -> String {
    // Parse: {"query": "검색어", "query_embedding": [...], "config": {"db_path": "..."}}
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

    // Parse optional pre-computed query embedding from SGLang.
    let query_embedding: Option<Vec<f32>> = parsed
        .get("query_embedding")
        .and_then(|v| v.as_array())
        .map(|arr| {
            arr.iter()
                .filter_map(|v| v.as_f64().map(|f| f as f32))
                .collect()
        });

    // Parse optional mode override ("bm25", "semantic", "hybrid").
    let mode: Option<&str> = parsed
        .get("mode")
        .and_then(|v| v.as_str());

    let base = cached_vega_config();
    let config = if let Some(cfg) = parsed.get("config") {
        let mut vc = base.clone();
        if let Some(p) = cfg.get("db_path").and_then(|v| v.as_str()) {
            vc.db_path = std::path::PathBuf::from(p);
        }
        if let Some(p) = cfg.get("md_dir").and_then(|v| v.as_str()) {
            vc.md_dir = std::path::PathBuf::from(p);
        }
        vc
    } else {
        base.clone()
    };

    vega_search_with_config(query, query_embedding.as_deref(), mode, &config)
}

#[cfg(feature = "vega")]
fn vega_search_direct(query: &str) -> String {
    vega_search_with_config(query, None, None, cached_vega_config())
}

#[cfg(feature = "vega")]
fn vega_search_with_config(
    query: &str,
    query_embedding: Option<&[f32]>,
    mode: Option<&str>,
    config: &deneb_vega::config::VegaConfig,
) -> String {
    let router = deneb_vega::search::SearchRouter::new(config.clone());
    match router.search_with_mode(query, query_embedding, mode) {
        Ok(result) => serde_json::to_string(&result)
            .unwrap_or_else(|e| format!(r#"{{"error":"serialize","detail":"{}"}}"#, e)),
        Err(e) => format!(r#"{{"error":"search_failed","detail":"{}"}}"#, e),
    }
}

#[cfg(not(feature = "vega"))]
fn vega_search_impl(_query_json: &str) -> String {
    r#"{"results":[],"phase":0}"#.to_string()
}
