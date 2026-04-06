//! LoRA adapter management for local GGUF inference.
//!
//! Inference-only: loads LoRA adapters trained externally (via sglang + Tinker-Atropos)
//! and applies them to a local GGUF model for improved response generation.
//!
//! Training (forward-backward, IS loss, gradient updates) is handled entirely by
//! the external Python pipeline — this module never computes gradients or logprobs.
//!
//! Adapter lifecycle:
//! 1. Load base GGUF model
//! 2. Load LoRA adapter (.gguf format, converted from PyTorch via llama.cpp)
//! 3. Generate text with adapter applied
//! 4. Swap/unload adapter when a new one is trained

use crate::error::MlError;

/// Result of a text generation operation (inference only, no logprobs).
#[derive(Debug, serde::Serialize)]
pub struct GenerateResult {
    /// Generated text.
    pub text: String,
    /// Number of tokens generated.
    pub token_count: usize,
    /// Model identifier.
    pub model: String,
}

/// LoRA adapter metadata.
#[derive(Debug, serde::Serialize, serde::Deserialize)]
pub struct LoraAdapterInfo {
    /// Path to the adapter file.
    pub path: String,
    /// Whether the adapter is currently loaded.
    pub loaded: bool,
    /// Base model this adapter is applied to.
    pub base_model: String,
}

// ---------------------------------------------------------------------------
// Feature-gated implementation
// ---------------------------------------------------------------------------

#[cfg(feature = "llama")]
mod inner {
    use super::*;
    use std::path::PathBuf;
    use std::sync::Mutex;

    use llama_cpp_2::context::params::LlamaContextParams;
    use llama_cpp_2::llama_backend::LlamaBackend;
    use llama_cpp_2::llama_batch::LlamaBatch;
    use llama_cpp_2::model::params::LlamaModelParams;
    use llama_cpp_2::model::{AddBos, LlamaModel};
    use llama_cpp_2::token::data_array::LlamaTokenDataArray;

    /// RAII guard that redirects stderr to /dev/null (llama.cpp is noisy).
    #[allow(unsafe_code)]
    struct SuppressStderr {
        saved_fd: libc::c_int,
    }

    #[allow(unsafe_code)]
    impl SuppressStderr {
        fn new() -> Option<Self> {
            unsafe {
                let saved = libc::dup(libc::STDERR_FILENO);
                if saved < 0 {
                    return None;
                }
                let devnull = libc::open(b"/dev/null\0".as_ptr().cast(), libc::O_WRONLY);
                if devnull < 0 {
                    libc::close(saved);
                    return None;
                }
                libc::dup2(devnull, libc::STDERR_FILENO);
                libc::close(devnull);
                Some(Self { saved_fd: saved })
            }
        }
    }

    #[allow(unsafe_code)]
    impl Drop for SuppressStderr {
        fn drop(&mut self) {
            unsafe {
                libc::dup2(self.saved_fd, libc::STDERR_FILENO);
                libc::close(self.saved_fd);
            }
        }
    }

    /// Generative model state: base model + optional LoRA adapter path.
    /// Uses Mutex<Option<>> instead of OnceLock so the model can be replaced
    /// or unloaded (e.g., when swapping LoRA adapters).
    struct ModelState {
        model: LlamaModel,
        model_name: String,
        lora_path: Option<String>,
    }

    static GEN_MODEL: Mutex<Option<ModelState>> = Mutex::new(None);

    fn ensure_backend() -> &'static LlamaBackend {
        // Reuse the embedder's global backend to avoid double-init.
        // The embedder module initializes this on first use.
        static BACKEND: std::sync::OnceLock<LlamaBackend> = std::sync::OnceLock::new();
        BACKEND.get_or_init(|| {
            llama_cpp_2::send_logs_to_tracing(
                llama_cpp_2::LogOptions::default().with_logs_enabled(false),
            );
            LlamaBackend::init().unwrap_or_else(|e| {
                panic!("llama backend init failed: {e}");
            })
        })
    }

    fn ensure_model_loaded(model_path: &str) -> Result<(), MlError> {
        let mut guard = GEN_MODEL.lock().map_err(|e| MlError::InferenceFailed(format!("lock: {e}")))?;
        if guard.is_some() {
            return Ok(());
        }
        let path = PathBuf::from(model_path);
        if !path.exists() {
            return Err(MlError::ModelNotFound(model_path.to_string()));
        }
        let _suppress = SuppressStderr::new();
        let backend = ensure_backend();
        let params = LlamaModelParams::default();
        let model = LlamaModel::load_from_file(backend, &path, &params)
            .map_err(|e| MlError::InferenceFailed(format!("model load: {e}")))?;
        let name = path.file_stem().and_then(|s| s.to_str()).unwrap_or("unknown").to_string();
        *guard = Some(ModelState { model, model_name: name, lora_path: None });
        Ok(())
    }

    /// Load a LoRA adapter for subsequent generation calls.
    /// The adapter is validated on disk and its path stored. It gets applied
    /// to each new inference context, so there is no use-after-free risk.
    pub fn load_lora_adapter(model_path: &str, lora_path: &str) -> Result<LoraAdapterInfo, MlError> {
        let lora_file = PathBuf::from(lora_path);
        if !lora_file.exists() {
            return Err(MlError::LoraAdapterNotFound(lora_path.to_string()));
        }

        ensure_model_loaded(model_path)?;
        let mut guard = GEN_MODEL.lock().map_err(|e| MlError::InferenceFailed(format!("lock: {e}")))?;
        let state = guard.as_mut().ok_or_else(|| MlError::InferenceFailed("model not loaded".into()))?;

        // Validate adapter can be initialized (catch format errors early).
        let _adapter = state.model.lora_adapter_init(&lora_file)
            .map_err(|e| MlError::LoraAdapterLoadFailed(format!("{e}")))?;
        // Adapter is dropped here — it will be re-created per inference context.
        // This avoids lifetime issues between adapter and context.

        let base_model = state.model_name.clone();
        state.lora_path = Some(lora_path.to_string());

        Ok(LoraAdapterInfo { path: lora_path.to_string(), loaded: true, base_model })
    }

    /// Remove the current LoRA adapter. Subsequent generations use the base model.
    pub fn unload_lora_adapter(model_path: &str) -> Result<(), MlError> {
        ensure_model_loaded(model_path)?;
        let mut guard = GEN_MODEL.lock().map_err(|e| MlError::InferenceFailed(format!("lock: {e}")))?;
        if let Some(ref mut state) = *guard {
            state.lora_path = None;
        }
        Ok(())
    }

    /// Generate text using the local model, optionally with a LoRA adapter.
    ///
    /// This is inference-only — no logprob computation. Log-probabilities for
    /// RL training are computed by sglang, not here. This function is for
    /// production inference with a trained adapter.
    pub fn generate(
        model_path: &str,
        prompt: &str,
        max_tokens: usize,
        temperature: f32,
    ) -> Result<GenerateResult, MlError> {
        if prompt.is_empty() {
            return Err(MlError::InvalidInput("empty prompt".into()));
        }

        ensure_model_loaded(model_path)?;
        let guard = GEN_MODEL.lock().map_err(|e| MlError::InferenceFailed(format!("lock: {e}")))?;
        let state = guard.as_ref().ok_or_else(|| MlError::InferenceFailed("model not loaded".into()))?;

        let _suppress = SuppressStderr::new();
        let backend = ensure_backend();

        // Create a fresh context for this generation.
        let ctx_size = std::cmp::min(max_tokens + 2048, 8192) as u32;
        let ctx_params = LlamaContextParams::default()
            .with_n_ctx(std::num::NonZeroU32::new(ctx_size));
        let mut ctx = state.model.new_context(backend, ctx_params)
            .map_err(|e| MlError::InferenceFailed(format!("context: {e}")))?;

        // Apply LoRA adapter if one is loaded.
        // The adapter is created per-context and dropped with the context,
        // avoiding any use-after-free between adapter and context lifetimes.
        let _adapter_guard = if let Some(ref lora_path) = state.lora_path {
            let lora_file = PathBuf::from(lora_path);
            let mut adapter = state.model.lora_adapter_init(&lora_file)
                .map_err(|e| MlError::LoraAdapterLoadFailed(format!("{e}")))?;
            ctx.lora_adapter_set(&mut adapter)
                .map_err(|e| MlError::LoraAdapterSetFailed(format!("{e}")))?;
            Some(adapter) // keep alive until ctx is dropped
        } else {
            None
        };

        // Tokenize prompt.
        let prompt_tokens = state.model.str_to_token(prompt, AddBos::Always)
            .map_err(|e| MlError::InferenceFailed(format!("tokenize: {e}")))?;

        // Feed prompt.
        let n_prompt = prompt_tokens.len();
        let mut batch = LlamaBatch::new(n_prompt.max(1), 1);
        for (i, &token) in prompt_tokens.iter().enumerate() {
            batch.add(token, i as i32, &[0], i == n_prompt - 1)
                .map_err(|e| MlError::InferenceFailed(format!("batch: {e}")))?;
        }
        ctx.decode(&mut batch)
            .map_err(|e| MlError::InferenceFailed(format!("decode prompt: {e}")))?;

        // Autoregressive generation (no logprob tracking).
        let mut generated_tokens: Vec<llama_cpp_2::token::LlamaToken> = Vec::with_capacity(max_tokens);

        for step in 0..max_tokens {
            let candidates_arr = ctx.candidates_ith((if step == 0 { n_prompt - 1 } else { 0 }) as i32);

            // Sample.
            let new_token = if temperature <= 0.0 {
                ctx.sample_token_greedy(candidates_arr)
            } else {
                let mut arr = candidates_arr;
                ctx.sample_temp(&mut arr, temperature);
                ctx.sample_softmax(&mut arr);
                ctx.sample_token(&mut arr)
            };

            if state.model.is_eog_token(new_token) {
                break;
            }
            generated_tokens.push(new_token);

            // Next step.
            batch.clear();
            batch.add(new_token, (n_prompt + step) as i32, &[0], true)
                .map_err(|e| MlError::InferenceFailed(format!("gen batch: {e}")))?;
            ctx.decode(&mut batch)
                .map_err(|e| MlError::InferenceFailed(format!("gen decode: {e}")))?;
        }

        // Detokenize.
        let text = generated_tokens.iter()
            .filter_map(|&id| {
                state.model.token_to_str(id, llama_cpp_2::model::Special::Tokenize).ok()
            })
            .collect::<String>();

        Ok(GenerateResult {
            token_count: generated_tokens.len(),
            text,
            model: state.model_name.clone(),
        })
    }
}

#[cfg(not(feature = "llama"))]
mod inner {
    use super::*;

    pub fn load_lora_adapter(_model_path: &str, _lora_path: &str) -> Result<LoraAdapterInfo, MlError> {
        Err(MlError::BackendUnavailable)
    }

    pub fn unload_lora_adapter(_model_path: &str) -> Result<(), MlError> {
        Err(MlError::BackendUnavailable)
    }

    pub fn generate(
        _model_path: &str,
        _prompt: &str,
        _max_tokens: usize,
        _temperature: f32,
    ) -> Result<GenerateResult, MlError> {
        Err(MlError::BackendUnavailable)
    }
}

pub use inner::*;
