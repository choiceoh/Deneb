//! napi-rs bindings for the context engine.
//!
//! Provides handle-based state machines for:
//! 1. **Assembly engine** — context selection under token budget
//! 2. **Expand engine** — DAG traversal with token budgeting
//! 3. **Pure functions** — grep/describe delegated directly to host

#[cfg(test)]
use super::assembler::AssemblyCommand;
use super::assembler::{AssemblyEngine, AssemblyResponse};
#[cfg(test)]
use super::retrieval::RetrievalCommand;
use super::retrieval::{
    DescribeEngine, ExpandEngine, GrepEngine, GrepMode, GrepScope, RetrievalResponse,
};
use super::{estimate_tokens, AuroraConfig};
#[cfg(feature = "napi_binding")]
use napi::bindgen_prelude::*;
use std::sync::Mutex;

// ── Handle stores ────────────────────────────────────────────────────────────

enum EngineInstance {
    Assembly(AssemblyEngine),
    Expand(ExpandEngine),
    Grep(GrepEngine),
    Describe(DescribeEngine),
}

static CONTEXT_ENGINES: std::sync::LazyLock<Mutex<ContextEngineStore>> =
    std::sync::LazyLock::new(|| Mutex::new(ContextEngineStore::new()));

struct ContextEngineStore {
    engines: std::collections::HashMap<u32, EngineInstance>,
    next_handle: u32,
}

impl ContextEngineStore {
    fn new() -> Self {
        Self {
            engines: std::collections::HashMap::new(),
            next_handle: 1,
        }
    }

    fn insert(&mut self, engine: EngineInstance) -> u32 {
        let handle = self.next_handle;
        self.next_handle = self.next_handle.wrapping_add(1);
        if self.next_handle == 0 {
            self.next_handle = 1;
        }
        self.engines.insert(handle, engine);
        handle
    }

    fn remove(&mut self, handle: u32) {
        self.engines.remove(&handle);
    }
}

/// Lock the context engine store.
fn lock_context_store() -> std::sync::MutexGuard<'static, ContextEngineStore> {
    CONTEXT_ENGINES.lock().expect("context engine store lock poisoned")
}

// ── Assembly engine exports ──────────────────────────────────────────────────

/// Create a new assembly engine. Returns a handle (u32).
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_assembly_new(conversation_id: u32, token_budget: u32, fresh_tail_count: u32) -> u32 {
    let engine = AssemblyEngine::new(
        conversation_id as u64,
        token_budget as u64,
        fresh_tail_count,
    );
    let mut store = lock_context_store();
    store.insert(EngineInstance::Assembly(engine))
}

/// Start an assembly engine. Returns the first `AssemblyCommand` as JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_assembly_start(handle: u32) -> String {
    let mut store = lock_context_store();
    match store.engines.get_mut(&handle) {
        Some(EngineInstance::Assembly(engine)) => {
            let cmd = engine.start();
            serde_json::to_string(&cmd).unwrap_or_else(|e| format!(r#"{{"error":"{e}"}}"#))
        }
        _ => empty_assembly_done(),
    }
}

/// Step an assembly engine with a host response. Returns the next command as JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_assembly_step(handle: u32, response_json: String) -> String {
    let response: AssemblyResponse = match serde_json::from_str(&response_json) {
        Ok(r) => r,
        Err(e) => return format!(r#"{{"type":"done","result":{{"error":"{e}"}}}}"#),
    };
    let mut store = lock_context_store();
    match store.engines.get_mut(&handle) {
        Some(EngineInstance::Assembly(engine)) => {
            let cmd = engine.step(response);
            serde_json::to_string(&cmd).unwrap_or_else(|e| format!(r#"{{"error":"{e}"}}"#))
        }
        _ => empty_assembly_done(),
    }
}

fn empty_assembly_done() -> String {
    r#"{"type":"done","result":{"estimatedTokens":0,"rawMessageCount":0,"summaryCount":0,"totalContextItems":0,"selectedItemIds":[]}}"#.to_string()
}

// ── Expand engine exports ────────────────────────────────────────────────────

/// Create a new expand engine. Returns a handle (u32).
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_expand_new(
    summary_id: String,
    max_depth: u32,
    include_messages: bool,
    token_cap: u32,
) -> u32 {
    let engine = ExpandEngine::new(summary_id, max_depth, include_messages, token_cap as u64);
    let mut store = lock_context_store();
    store.insert(EngineInstance::Expand(engine))
}

/// Start an expand engine. Returns the first `RetrievalCommand` as JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_expand_start(handle: u32) -> String {
    let mut store = lock_context_store();
    match store.engines.get_mut(&handle) {
        Some(EngineInstance::Expand(engine)) => {
            let cmd = engine.start();
            serde_json::to_string(&cmd).unwrap_or_else(|e| format!(r#"{{"error":"{e}"}}"#))
        }
        _ => empty_expand_done(),
    }
}

/// Step an expand engine with a host response. Returns the next command as JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_expand_step(handle: u32, response_json: String) -> String {
    let response: RetrievalResponse = match serde_json::from_str(&response_json) {
        Ok(r) => r,
        Err(e) => return format!(r#"{{"type":"expandDone","result":{{"error":"{e}"}}}}"#),
    };
    let mut store = lock_context_store();
    match store.engines.get_mut(&handle) {
        Some(EngineInstance::Expand(engine)) => {
            let cmd = engine.step(response);
            serde_json::to_string(&cmd).unwrap_or_else(|e| format!(r#"{{"error":"{e}"}}"#))
        }
        _ => empty_expand_done(),
    }
}

fn empty_expand_done() -> String {
    r#"{"type":"expandDone","result":{"children":[],"messages":[],"estimatedTokens":0,"truncated":false}}"#.to_string()
}

// ── Grep engine exports ──────────────────────────────────────────────────────

/// Create a new grep engine. Returns a handle (u32).
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_grep_new(
    query: String,
    mode: String,
    scope: String,
    conversation_id: Option<u32>,
    since_ms: Option<f64>,
    before_ms: Option<f64>,
    limit: Option<u32>,
) -> u32 {
    let grep_mode = match mode.as_str() {
        "full_text" => GrepMode::FullText,
        _ => GrepMode::Regex,
    };
    let grep_scope = match scope.as_str() {
        "messages" => GrepScope::Messages,
        "summaries" => GrepScope::Summaries,
        _ => GrepScope::Both,
    };
    let engine = GrepEngine::new(
        query,
        grep_mode,
        grep_scope,
        conversation_id.map(|id| id as u64),
        since_ms.map(|ms| ms as i64),
        before_ms.map(|ms| ms as i64),
        limit,
    );
    let mut store = lock_context_store();
    store.insert(EngineInstance::Grep(engine))
}

/// Start a grep engine. Returns the `RetrievalCommand` as JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_grep_start(handle: u32) -> String {
    let store = lock_context_store();
    match store.engines.get(&handle) {
        Some(EngineInstance::Grep(engine)) => {
            let cmd = engine.start();
            serde_json::to_string(&cmd).unwrap_or_else(|e| format!(r#"{{"error":"{e}"}}"#))
        }
        _ => empty_grep_done(),
    }
}

/// Step a grep engine with a host response. Returns the result as JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_grep_step(handle: u32, response_json: String) -> String {
    let response: RetrievalResponse = match serde_json::from_str(&response_json) {
        Ok(r) => r,
        Err(e) => return format!(r#"{{"type":"grepDone","result":{{"error":"{e}"}}}}"#),
    };
    let mut store = lock_context_store();
    match store.engines.get_mut(&handle) {
        Some(EngineInstance::Grep(engine)) => {
            let cmd = engine.step(response);
            serde_json::to_string(&cmd).unwrap_or_else(|e| format!(r#"{{"error":"{e}"}}"#))
        }
        _ => empty_grep_done(),
    }
}

fn empty_grep_done() -> String {
    r#"{"type":"grepDone","result":{"matches":[],"totalMatches":0}}"#.to_string()
}

// ── Describe engine exports ──────────────────────────────────────────────────

/// Create a new describe engine. Returns a handle (u32).
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_describe_new(id: String) -> u32 {
    let engine = DescribeEngine::new(id);
    let mut store = lock_context_store();
    store.insert(EngineInstance::Describe(engine))
}

/// Start a describe engine. Returns the `RetrievalCommand` as JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_describe_start(handle: u32) -> String {
    let store = lock_context_store();
    match store.engines.get(&handle) {
        Some(EngineInstance::Describe(engine)) => {
            let cmd = engine.start();
            serde_json::to_string(&cmd).unwrap_or_else(|e| format!(r#"{{"error":"{e}"}}"#))
        }
        _ => empty_describe_done(),
    }
}

/// Step a describe engine with a host response. Returns the result as JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_describe_step(handle: u32, response_json: String) -> String {
    let response: RetrievalResponse = match serde_json::from_str(&response_json) {
        Ok(r) => r,
        Err(e) => return format!(r#"{{"type":"describeDone","result":{{"error":"{e}"}}}}"#),
    };
    let mut store = lock_context_store();
    match store.engines.get_mut(&handle) {
        Some(EngineInstance::Describe(engine)) => {
            let cmd = engine.step(response);
            serde_json::to_string(&cmd).unwrap_or_else(|e| format!(r#"{{"error":"{e}"}}"#))
        }
        _ => empty_describe_done(),
    }
}

fn empty_describe_done() -> String {
    r#"{"type":"describeDone","result":{"id":"","itemType":"summary","parents":[],"children":[],"messageIds":[],"subtree":[]}}"#.to_string()
}

// ── Drop ─────────────────────────────────────────────────────────────────────

/// Drop any context engine, freeing its resources.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_engine_drop(handle: u32) {
    let mut store = lock_context_store();
    store.remove(handle);
}

// ── Pure function exports ────────────────────────────────────────────────────

/// Validate and resolve an Aurora config from JSON. Returns validated config JSON.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_resolve_config(config_json: String) -> String {
    match serde_json::from_str::<AuroraConfig>(&config_json) {
        Ok(config) => {
            let validated = config.validated();
            serde_json::to_string(&validated).unwrap_or_else(|e| format!(r#"{{"error":"{e}"}}"#))
        }
        Err(e) => {
            // Return defaults on parse failure
            let default_config = AuroraConfig::default();
            serde_json::to_string(&default_config)
                .unwrap_or_else(|_| format!(r#"{{"error":"{e}"}}"#))
        }
    }
}

/// Estimate token count from text.
#[cfg_attr(feature = "napi_binding", napi)]
pub fn context_estimate_tokens(text: String) -> u32 {
    estimate_tokens(&text) as u32
}

// ── Tests ────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_assembly_lifecycle() -> napi::Result<()> {
        let handle = context_assembly_new(1, 10_000, 8);
        assert!(handle > 0);

        let cmd_json = context_assembly_start(handle);
        let cmd: AssemblyCommand = serde_json::from_str(&cmd_json)?;
        assert!(matches!(cmd, AssemblyCommand::FetchContextItems { .. }));

        // Empty items → Done
        let resp = serde_json::to_string(&AssemblyResponse::ContextItems { items: vec![] })?;
        let cmd_json = context_assembly_step(handle, resp);
        let cmd: AssemblyCommand = serde_json::from_str(&cmd_json)?;
        assert!(matches!(cmd, AssemblyCommand::Done { .. }));

        context_engine_drop(handle);
        Ok(())
    }

    #[test]
    fn test_expand_lifecycle() -> napi::Result<()> {
        let handle = context_expand_new("sum_abc".to_string(), 1, true, 10_000);
        assert!(handle > 0);

        let cmd_json = context_expand_start(handle);
        let cmd: RetrievalCommand = serde_json::from_str(&cmd_json)?;
        assert!(matches!(cmd, RetrievalCommand::FetchSummary { .. }));

        // Leaf summary → fetch messages
        let resp = serde_json::to_string(&RetrievalResponse::Summary {
            summary_id: "sum_abc".to_string(),
            kind: "leaf".to_string(),
            depth: 0,
            content: "content".to_string(),
            token_count: 100,
        })?;
        let cmd_json = context_expand_step(handle, resp);
        let cmd: RetrievalCommand = serde_json::from_str(&cmd_json)?;
        assert!(matches!(cmd, RetrievalCommand::FetchSourceMessages { .. }));

        context_engine_drop(handle);
        Ok(())
    }

    #[test]
    fn test_grep_lifecycle() -> napi::Result<()> {
        let handle = context_grep_new(
            "test".to_string(),
            "regex".to_string(),
            "both".to_string(),
            None,
            None,
            None,
            Some(10),
        );
        assert!(handle > 0);

        let cmd_json = context_grep_start(handle);
        let cmd: RetrievalCommand = serde_json::from_str(&cmd_json)?;
        assert!(matches!(cmd, RetrievalCommand::Grep { .. }));

        context_engine_drop(handle);
        Ok(())
    }

    #[test]
    fn test_describe_lifecycle() -> napi::Result<()> {
        let handle = context_describe_new("sum_xyz".to_string());
        assert!(handle > 0);

        let cmd_json = context_describe_start(handle);
        let cmd: RetrievalCommand = serde_json::from_str(&cmd_json)?;
        assert!(matches!(cmd, RetrievalCommand::FetchLineage { .. }));

        context_engine_drop(handle);
        Ok(())
    }

    #[test]
    fn test_resolve_config_valid() -> napi::Result<()> {
        let json = r#"{"contextThreshold": 0.5, "freshTailCount": 16}"#;
        let result = context_resolve_config(json.to_string());
        let config: AuroraConfig = serde_json::from_str(&result)?;
        assert_eq!(config.context_threshold, 0.5);
        assert_eq!(config.fresh_tail_count, 16);
        Ok(())
    }

    #[test]
    fn test_resolve_config_invalid_returns_defaults() -> napi::Result<()> {
        let result = context_resolve_config("not-json".to_string());
        let config: AuroraConfig = serde_json::from_str(&result)?;
        assert_eq!(config.context_threshold, 0.75);
        Ok(())
    }

    #[test]
    fn test_estimate_tokens_napi() {
        assert_eq!(context_estimate_tokens("hello world".into()), 3);
    }
}
