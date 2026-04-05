//! C FFI: Local ML embedding inference (requires `ml` feature).

use crate::ffi_utils::*;

ffi_string_to_buffer!(
    /// C FFI: Embed texts using a local GGUF model.
    /// Takes JSON input `{"texts":["t1","t2"],"model_path":"/path/to/model.gguf"}`,
    /// writes JSON result to output buffer.
    /// Returns bytes written on success, negative on error.
    ///
    /// When the `ml` feature is enabled, dispatches to `deneb-ml` embedder.
    /// Otherwise returns `{"error":"ml_not_available"}`.
    ///
    /// # Safety
    /// `input_ptr` must point to valid UTF-8 of `input_len` bytes.
    /// `out_ptr` must be writable for `out_len` bytes.
    fn deneb_ml_embed(
        input_ptr,
        input_len,
        out = (out_ptr, out_len),
        max_len = FFI_MAX_INPUT_LEN,
        input_str,
        out_slice
    ) {
        let result_json = ml_embed_impl(input_str);
        ffi_write_bytes(out_slice, result_json.as_bytes())
    }
);

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

#[cfg(feature = "ml")]
fn ml_embed_impl(input_json: &str) -> String {
    let parsed: serde_json::Value = match serde_json::from_str(input_json) {
        Ok(v) => v,
        Err(e) => return format!(r#"{{"error":"invalid_json","detail":"{e}"}}"#),
    };

    let model_path = match parsed.get("model_path").and_then(|v| v.as_str()) {
        Some(p) => p,
        None => return r#"{"error":"missing_model_path"}"#.to_string(),
    };

    let texts: Vec<&str> = match parsed.get("texts").and_then(|v| v.as_array()) {
        Some(arr) => arr.iter().filter_map(|v| v.as_str()).collect(),
        None => return r#"{"error":"missing_texts"}"#.to_string(),
    };

    match deneb_ml::embed_texts(model_path, &texts) {
        Ok(result) => serde_json::to_string(&result)
            .unwrap_or_else(|e| format!(r#"{{"error":"serialize","detail":"{e}"}}"#)),
        Err(e) => format!(r#"{{"error":"embed_failed","detail":"{e}"}}"#),
    }
}

#[cfg(not(feature = "ml"))]
fn ml_embed_impl(_input_json: &str) -> String {
    r#"{"error":"ml_not_available"}"#.to_string()
}
