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

// ---------------------------------------------------------------------------
// LoRA adapter management
// ---------------------------------------------------------------------------

ffi_string_to_buffer!(
    /// C FFI: Load a LoRA adapter for the generative model.
    /// Takes JSON `{"model_path":"/path/to/model.gguf","lora_path":"/path/to/adapter.gguf"}`,
    /// writes JSON result to output buffer.
    fn deneb_ml_lora_load(
        input_ptr,
        input_len,
        out = (out_ptr, out_len),
        max_len = FFI_MAX_INPUT_LEN,
        input_str,
        out_slice
    ) {
        let result_json = ml_lora_load_impl(input_str);
        ffi_write_bytes(out_slice, result_json.as_bytes())
    }
);

ffi_string_to_buffer!(
    /// C FFI: Unload the current LoRA adapter.
    /// Takes JSON `{"model_path":"/path/to/model.gguf"}`.
    fn deneb_ml_lora_unload(
        input_ptr,
        input_len,
        out = (out_ptr, out_len),
        max_len = FFI_MAX_INPUT_LEN,
        input_str,
        out_slice
    ) {
        let result_json = ml_lora_unload_impl(input_str);
        ffi_write_bytes(out_slice, result_json.as_bytes())
    }
);

ffi_string_to_buffer!(
    /// C FFI: Generate text using the local model (optionally with LoRA).
    /// Takes JSON `{"model_path":"/path/model.gguf","prompt":"text","max_tokens":256,"temperature":0.7}`.
    /// Returns JSON `{"text":"...","token_count":N,"model":"..."}`.
    /// Inference-only: logprobs for RL training are computed by sglang, not here.
    fn deneb_ml_generate(
        input_ptr,
        input_len,
        out = (out_ptr, out_len),
        max_len = FFI_MAX_INPUT_LEN,
        input_str,
        out_slice
    ) {
        let result_json = ml_generate_impl(input_str);
        ffi_write_bytes(out_slice, result_json.as_bytes())
    }
);

#[cfg(feature = "ml")]
fn ml_lora_load_impl(input_json: &str) -> String {
    let parsed: serde_json::Value = match serde_json::from_str(input_json) {
        Ok(v) => v,
        Err(e) => return format!(r#"{{"error":"invalid_json","detail":"{e}"}}"#),
    };
    let model_path = match parsed.get("model_path").and_then(|v| v.as_str()) {
        Some(p) => p,
        None => return r#"{"error":"missing_model_path"}"#.to_string(),
    };
    let lora_path = match parsed.get("lora_path").and_then(|v| v.as_str()) {
        Some(p) => p,
        None => return r#"{"error":"missing_lora_path"}"#.to_string(),
    };
    match deneb_ml::load_lora_adapter(model_path, lora_path) {
        Ok(info) => serde_json::to_string(&info)
            .unwrap_or_else(|e| format!(r#"{{"error":"serialize","detail":"{e}"}}"#)),
        Err(e) => format!(r#"{{"error":"lora_load_failed","detail":"{e}"}}"#),
    }
}

#[cfg(not(feature = "ml"))]
fn ml_lora_load_impl(_input_json: &str) -> String {
    r#"{"error":"ml_not_available"}"#.to_string()
}

#[cfg(feature = "ml")]
fn ml_lora_unload_impl(input_json: &str) -> String {
    let parsed: serde_json::Value = match serde_json::from_str(input_json) {
        Ok(v) => v,
        Err(e) => return format!(r#"{{"error":"invalid_json","detail":"{e}"}}"#),
    };
    let model_path = match parsed.get("model_path").and_then(|v| v.as_str()) {
        Some(p) => p,
        None => return r#"{"error":"missing_model_path"}"#.to_string(),
    };
    match deneb_ml::unload_lora_adapter(model_path) {
        Ok(()) => r#"{"ok":true}"#.to_string(),
        Err(e) => format!(r#"{{"error":"lora_unload_failed","detail":"{e}"}}"#),
    }
}

#[cfg(not(feature = "ml"))]
fn ml_lora_unload_impl(_input_json: &str) -> String {
    r#"{"error":"ml_not_available"}"#.to_string()
}

#[cfg(feature = "ml")]
fn ml_generate_impl(input_json: &str) -> String {
    let parsed: serde_json::Value = match serde_json::from_str(input_json) {
        Ok(v) => v,
        Err(e) => return format!(r#"{{"error":"invalid_json","detail":"{e}"}}"#),
    };
    let model_path = match parsed.get("model_path").and_then(|v| v.as_str()) {
        Some(p) => p,
        None => return r#"{"error":"missing_model_path"}"#.to_string(),
    };
    let prompt = match parsed.get("prompt").and_then(|v| v.as_str()) {
        Some(p) => p,
        None => return r#"{"error":"missing_prompt"}"#.to_string(),
    };
    let max_tokens = parsed
        .get("max_tokens")
        .and_then(|v| v.as_u64())
        .unwrap_or(256) as usize;
    let temperature = parsed
        .get("temperature")
        .and_then(|v| v.as_f64())
        .unwrap_or(0.7) as f32;

    match deneb_ml::generate(model_path, prompt, max_tokens, temperature) {
        Ok(result) => serde_json::to_string(&result)
            .unwrap_or_else(|e| format!(r#"{{"error":"serialize","detail":"{e}"}}"#)),
        Err(e) => format!(r#"{{"error":"generate_failed","detail":"{e}"}}"#),
    }
}

#[cfg(not(feature = "ml"))]
fn ml_generate_impl(_input_json: &str) -> String {
    r#"{"error":"ml_not_available"}"#.to_string()
}

/// Returns 1 if the `ml` feature is compiled in, 0 otherwise.
/// Allows Go to skip local embedder initialization when ML is unavailable.
///
/// # Safety
/// No pointer arguments — always safe to call.
#[no_mangle]
pub extern "C" fn deneb_ml_available() -> i32 {
    if cfg!(feature = "ml") {
        1
    } else {
        0
    }
}
