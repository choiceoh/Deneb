//! napi-rs bindings for the compaction engine.
//!
//! Provides two integration surfaces:
//! 1. **Pure functions** — stateless helpers callable from Node.js
//! 2. **Sweep engine** — handle-based state machine for full compaction sweeps

use super::*;
use super::sweep::{SweepEngine, SweepResponse};
#[cfg(test)]
use super::sweep::SweepCommand;
use std::sync::Mutex;

// ── Handle store ────────────────────────────────────────────────────────────

/// Global store for active sweep engine instances.
/// Each instance is identified by a monotonically increasing handle.
static ENGINES: once_cell::sync::Lazy<Mutex<EngineStore>> =
    once_cell::sync::Lazy::new(|| Mutex::new(EngineStore::new()));

struct EngineStore {
    engines: std::collections::HashMap<u32, SweepEngine>,
    next_handle: u32,
}

impl EngineStore {
    fn new() -> Self {
        Self {
            engines: std::collections::HashMap::new(),
            next_handle: 1,
        }
    }

    fn insert(&mut self, engine: SweepEngine) -> u32 {
        let handle = self.next_handle;
        self.next_handle = self.next_handle.wrapping_add(1);
        if self.next_handle == 0 {
            self.next_handle = 1;
        }
        self.engines.insert(handle, engine);
        handle
    }

    fn get_mut(&mut self, handle: u32) -> Option<&mut SweepEngine> {
        self.engines.get_mut(&handle)
    }

    fn remove(&mut self, handle: u32) {
        self.engines.remove(&handle);
    }
}

// ── Pure function exports ───────────────────────────────────────────────────

/// Estimate token count from text (ceil(len/4)).
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_estimate_tokens(text: String) -> u32 {
    estimate_tokens(&text) as u32
}

/// Format an epoch-millisecond timestamp as `YYYY-MM-DD HH:mm TZ`.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_format_timestamp(epoch_ms: i64, tz: String) -> String {
    timestamp::format_timestamp(epoch_ms, &tz)
}

/// Evaluate whether compaction is needed. Returns JSON CompactionDecision.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_evaluate(
    config_json: String,
    stored_tokens: u32,
    live_tokens: u32,
    token_budget: u32,
) -> String {
    match compaction_evaluate_impl(&config_json, stored_tokens, live_tokens, token_budget) {
        Ok(json) => json,
        Err(e) => serde_json::json!({"error": e.to_string()}).to_string(),
    }
}

fn compaction_evaluate_impl(
    config_json: &str,
    stored_tokens: u32,
    live_tokens: u32,
    token_budget: u32,
) -> Result<String, CompactionError> {
    let config: CompactionConfig = serde_json::from_str(config_json)?;
    let decision = evaluate(
        &config,
        stored_tokens as u64,
        live_tokens as u64,
        token_budget as u64,
    );
    Ok(serde_json::to_string(&decision)?)
}

/// Resolve fresh tail ordinal from context items JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_resolve_fresh_tail_ordinal(items_json: String, fresh_tail_count: u32) -> f64 {
    let items: Vec<ContextItem> = match serde_json::from_str(&items_json) {
        Ok(i) => i,
        Err(_) => return f64::INFINITY,
    };
    let ordinal = resolve_fresh_tail_ordinal(&items, fresh_tail_count);
    if ordinal == u64::MAX {
        f64::INFINITY
    } else {
        ordinal as f64
    }
}

/// Build leaf source text from messages JSON. Returns formatted text.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_build_leaf_source_text(messages_json: String, tz: String) -> String {
    let messages: Vec<MessageRecord> = match serde_json::from_str(&messages_json) {
        Ok(m) => m,
        Err(e) => return format!("error: {}", e),
    };
    build_leaf_source_text(&messages, &tz)
}

/// Build condensed source text from summaries JSON. Returns formatted text.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_build_condensed_source_text(summaries_json: String, tz: String) -> String {
    let summaries: Vec<SummaryRecord> = match serde_json::from_str(&summaries_json) {
        Ok(s) => s,
        Err(e) => return format!("error: {}", e),
    };
    build_condensed_source_text(&summaries, &tz)
}

/// Generate a summary ID from content and timestamp.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_generate_summary_id(content: String, now_ms: f64) -> String {
    generate_summary_id(&content, now_ms as i64)
}

/// Deterministic fallback when LLM summarization fails.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_deterministic_fallback(source: String, input_tokens: u32) -> String {
    deterministic_fallback(&source, input_tokens as u64)
}

// ── Sweep engine exports ────────────────────────────────────────────────────

/// Create a new sweep engine. Returns a handle (u32).
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_sweep_new(
    config_json: String,
    conversation_id: u32,
    token_budget: u32,
    force: bool,
    hard_trigger: bool,
    now_ms: f64,
) -> u32 {
    let config: CompactionConfig = serde_json::from_str(&config_json).unwrap_or_default();
    let engine = SweepEngine::new(
        config,
        conversation_id as u64,
        token_budget as u64,
        force,
        hard_trigger,
        now_ms as i64,
    );
    let mut store = ENGINES.lock().unwrap();
    store.insert(engine)
}

/// Start a sweep engine. Returns the first SweepCommand as JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_sweep_start(handle: u32) -> String {
    let mut store = ENGINES.lock().unwrap();
    match store.get_mut(handle) {
        Some(engine) => {
            let cmd = engine.start();
            serde_json::to_string(&cmd).unwrap_or_else(|e| format!(r#"{{"error":"{}"}}"#, e))
        }
        None => r#"{"type":"done","result":{"actionTaken":false,"tokensBefore":0,"tokensAfter":0,"condensed":false}}"#.to_string(),
    }
}

/// Step a sweep engine with a host response. Returns the next SweepCommand as JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_sweep_step(handle: u32, response_json: String) -> String {
    let response: SweepResponse = match serde_json::from_str(&response_json) {
        Ok(r) => r,
        Err(e) => return format!(r#"{{"type":"done","result":{{"error":"{}"}}}}"#, e),
    };
    let mut store = ENGINES.lock().unwrap();
    match store.get_mut(handle) {
        Some(engine) => {
            let cmd = engine.step(response);
            serde_json::to_string(&cmd).unwrap_or_else(|e| format!(r#"{{"error":"{}"}}"#, e))
        }
        None => r#"{"type":"done","result":{"actionTaken":false,"tokensBefore":0,"tokensAfter":0,"condensed":false}}"#.to_string(),
    }
}

/// Drop a sweep engine, freeing its resources.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn compaction_sweep_drop(handle: u32) {
    let mut store = ENGINES.lock().unwrap();
    store.remove(handle);
}

// ── Tests ───────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_compaction_estimate_tokens_napi() {
        assert_eq!(compaction_estimate_tokens("hello world".into()), 3);
        assert_eq!(compaction_estimate_tokens("".into()), 0);
    }

    #[test]
    fn test_compaction_evaluate_napi() {
        let config_json = serde_json::to_string(&CompactionConfig::default()).unwrap();
        let result = compaction_evaluate(config_json, 800, 0, 1000);
        let decision: CompactionDecision = serde_json::from_str(&result).unwrap();
        assert!(decision.should_compact);
    }

    #[test]
    fn test_sweep_lifecycle() {
        let config_json = serde_json::to_string(&CompactionConfig::default()).unwrap();
        let handle = compaction_sweep_new(config_json, 1, 1000, false, false, 1000.0);
        assert!(handle > 0);

        let cmd_json = compaction_sweep_start(handle);
        let cmd: SweepCommand = serde_json::from_str(&cmd_json).unwrap();
        assert!(matches!(cmd, SweepCommand::FetchTokenCount { .. }));

        // Feed below-threshold tokens to get Done
        let resp = serde_json::to_string(&SweepResponse::TokenCount { count: 500 }).unwrap();
        let cmd_json = compaction_sweep_step(handle, resp);
        let cmd: SweepCommand = serde_json::from_str(&cmd_json).unwrap();
        assert!(matches!(cmd, SweepCommand::Done { .. }));

        compaction_sweep_drop(handle);
    }

    #[test]
    fn test_compaction_format_timestamp_napi() {
        let result = compaction_format_timestamp(1711324800000, "UTC".into());
        assert_eq!(result, "2024-03-25 00:00 UTC");
    }

    #[test]
    fn test_compaction_generate_summary_id_napi() {
        let id = compaction_generate_summary_id("hello".into(), 1000.0);
        assert!(id.starts_with("sum_"));
        assert_eq!(id.len(), 20);
    }
}
