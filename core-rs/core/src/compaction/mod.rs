//! Context compaction engine — Rust implementation of the Aurora hierarchical
//! summarization algorithm.
//!
//! This module provides:
//! - Core data types matching TypeScript `compaction.ts`
//! - Pure algorithmic functions (chunk selection, fresh-tail, token estimation)
//! - A step-based sweep state machine for cross-language I/O orchestration
//!
//! The sweep engine yields I/O commands (`SweepCommand`) to the host (Go),
//! which executes DB/LLM operations and feeds results back via
//! `SweepResponse`. This avoids callbacks across FFI boundaries.

pub mod handle;
pub mod sweep;
pub mod timestamp;

use rustc_hash::FxHashMap;
use serde::{Deserialize, Serialize};
use sha2::Digest;
use thiserror::Error;

/// Errors that can occur during compaction operations.
#[derive(Error, Debug)]
pub enum CompactionError {
    #[error("invalid config JSON: {0}")]
    InvalidConfig(#[from] serde_json::Error),

    #[error("sweep engine not found: handle {0}")]
    EngineNotFound(u32),

    #[error("unexpected response type for current phase")]
    UnexpectedResponse,
}

// ── Constants ───────────────────────────────────────────────────────────────

/// Maximum characters for the deterministic fallback truncation (512 tokens * 4 chars).
const FALLBACK_MAX_CHARS: usize = 512 * 4;

/// Default maximum source tokens per leaf/condensed chunk.
const DEFAULT_LEAF_CHUNK_TOKENS: u32 = 20_000;

/// Minimum condensed input ratio before running another condensed pass.
const CONDENSED_MIN_INPUT_RATIO: f64 = 0.1;

// ── Public types ────────────────────────────────────────────────────────────

/// Compaction configuration matching TypeScript `CompactionConfig`.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CompactionConfig {
    /// Context threshold as fraction of budget (default 0.80).
    #[serde(default = "default_context_threshold")]
    pub context_threshold: f64,
    /// Number of fresh tail turns to protect (default 8).
    #[serde(default = "default_fresh_tail_count")]
    pub fresh_tail_count: u32,
    /// Minimum number of depth-0 summaries needed for condensation (default 8).
    #[serde(default = "default_leaf_min_fanout")]
    pub leaf_min_fanout: u32,
    /// Minimum number of depth>=1 summaries needed for condensation (default 4).
    #[serde(default = "default_condensed_min_fanout")]
    pub condensed_min_fanout: u32,
    /// Relaxed minimum fanout for hard-trigger sweeps (default 2).
    #[serde(default = "default_condensed_min_fanout_hard")]
    pub condensed_min_fanout_hard: u32,
    /// Incremental depth passes after each leaf compaction (default 0, -1 = infinity).
    #[serde(default)]
    pub incremental_max_depth: i32,
    /// Max source tokens to compact per leaf/condensed chunk (default 20000).
    #[serde(default)]
    pub leaf_chunk_tokens: Option<u32>,
    /// Target tokens for leaf summaries (default 600).
    #[serde(default = "default_leaf_target_tokens")]
    pub leaf_target_tokens: u32,
    /// Target tokens for condensed summaries (default 900).
    #[serde(default = "default_condensed_target_tokens")]
    pub condensed_target_tokens: u32,
    /// Maximum compaction rounds (default 10).
    #[serde(default = "default_max_rounds")]
    pub max_rounds: u32,
    /// Maximum iterations per phase in full sweep (default 50).
    #[serde(default)]
    pub max_sweep_iterations: Option<u32>,
    /// IANA timezone for timestamps in summaries (default: UTC).
    #[serde(default)]
    pub timezone: Option<String>,
}

fn default_context_threshold() -> f64 {
    0.80
}
fn default_fresh_tail_count() -> u32 {
    8
}
fn default_leaf_min_fanout() -> u32 {
    8
}
fn default_condensed_min_fanout() -> u32 {
    4
}
fn default_condensed_min_fanout_hard() -> u32 {
    2
}
fn default_leaf_target_tokens() -> u32 {
    600
}
fn default_condensed_target_tokens() -> u32 {
    900
}
fn default_max_rounds() -> u32 {
    10
}

impl Default for CompactionConfig {
    fn default() -> Self {
        Self {
            context_threshold: 0.80,
            fresh_tail_count: 8,
            leaf_min_fanout: 8,
            condensed_min_fanout: 4,
            condensed_min_fanout_hard: 2,
            incremental_max_depth: 0,
            leaf_chunk_tokens: None,
            leaf_target_tokens: 600,
            condensed_target_tokens: 900,
            max_rounds: 10,
            max_sweep_iterations: None,
            timezone: None,
        }
    }
}

/// Compaction decision reason — why the sweep was triggered.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum CompactionReason {
    /// Context token count exceeded the configured threshold fraction.
    Threshold,
    /// Operator explicitly requested compaction.
    Manual,
    /// Compaction was evaluated but not needed.
    None,
}

/// Result of evaluating whether compaction is needed.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CompactionDecision {
    pub should_compact: bool,
    pub reason: CompactionReason,
    pub current_tokens: u64,
    pub threshold: u64,
}

/// Summarization escalation level — controls compression aggressiveness.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum CompactionLevel {
    /// Standard LLM summarization at the configured target token count.
    Normal,
    /// Tighter compression when normal summaries still exceed the budget.
    Aggressive,
    /// Deterministic truncation when LLM summarization fails or is unavailable.
    Fallback,
}

/// Summary kind — position in the hierarchical summary tree.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SummaryKind {
    /// Depth-0 summary created directly from raw messages.
    Leaf,
    /// Higher-depth summary created by condensing existing summaries.
    Condensed,
}

/// Result of a compaction operation.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CompactionResult {
    pub action_taken: bool,
    pub tokens_before: u64,
    pub tokens_after: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub created_summary_id: Option<String>,
    pub condensed: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub level: Option<CompactionLevel>,
}

/// Type of a context item.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ContextItemType {
    Message,
    Summary,
}

/// A context item record from the DB.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ContextItem {
    pub conversation_id: u64,
    pub ordinal: u64,
    pub item_type: ContextItemType,
    pub message_id: Option<u64>,
    pub summary_id: Option<String>,
    /// Epoch milliseconds.
    pub created_at: i64,
}

/// A message record from the DB.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MessageRecord {
    pub message_id: u64,
    pub conversation_id: u64,
    pub seq: u64,
    pub role: String,
    pub content: String,
    pub token_count: u64,
    /// Epoch milliseconds.
    pub created_at: i64,
}

/// A summary record from the DB.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SummaryRecord {
    pub summary_id: String,
    pub conversation_id: u64,
    pub kind: SummaryKind,
    pub depth: u32,
    pub content: String,
    pub token_count: u64,
    pub file_ids: Vec<String>,
    /// Epoch milliseconds (nullable).
    pub earliest_at: Option<i64>,
    /// Epoch milliseconds (nullable).
    pub latest_at: Option<i64>,
    pub descendant_count: u64,
    pub descendant_token_count: u64,
    pub source_message_token_count: u64,
    /// Epoch milliseconds.
    pub created_at: i64,
}

// ── Pure functions ──────────────────────────────────────────────────────────

/// Estimate token count from Unicode char count.
/// Korean BPE averages ~2 chars/token; English ~4 chars/token.
/// Divisor 2 is calibrated for Korean (the primary language) and accepts
/// a 2x overestimate for ASCII-only content. Matches Go `estimateTokens`.
#[inline]
pub fn estimate_tokens(text: &str) -> u64 {
    let chars = text.chars().count() as u64;
    (chars / 2).max(1)
}

/// Evaluate whether compaction is needed.
pub fn evaluate(
    config: &CompactionConfig,
    stored_tokens: u64,
    live_tokens: u64,
    token_budget: u64,
) -> CompactionDecision {
    let current_tokens = stored_tokens.max(live_tokens);
    let threshold = (config.context_threshold * token_budget as f64).floor() as u64;

    if current_tokens > threshold {
        CompactionDecision {
            should_compact: true,
            reason: CompactionReason::Threshold,
            current_tokens,
            threshold,
        }
    } else {
        CompactionDecision {
            should_compact: false,
            reason: CompactionReason::None,
            current_tokens,
            threshold,
        }
    }
}

/// Resolve the configured leaf chunk token limit.
pub fn resolve_leaf_chunk_tokens(config: &CompactionConfig) -> u32 {
    match config.leaf_chunk_tokens {
        Some(v) if v > 0 => v,
        _ => DEFAULT_LEAF_CHUNK_TOKENS,
    }
}

/// Resolve the configured fresh tail count.
pub fn resolve_fresh_tail_count(config: &CompactionConfig) -> u32 {
    if config.fresh_tail_count > 0 {
        config.fresh_tail_count
    } else {
        0
    }
}

/// Resolve the effective incremental max depth.
pub fn resolve_incremental_max_depth(config: &CompactionConfig) -> u32 {
    if config.incremental_max_depth < 0 {
        u32::MAX // infinity
    } else {
        config.incremental_max_depth as u32
    }
}

/// Resolve the required fanout for a given depth and trigger type.
pub fn resolve_fanout_for_depth(
    config: &CompactionConfig,
    target_depth: u32,
    hard_trigger: bool,
) -> u32 {
    if hard_trigger {
        config.condensed_min_fanout_hard.max(1)
    } else if target_depth == 0 {
        config.leaf_min_fanout.max(1)
    } else {
        config.condensed_min_fanout.max(1)
    }
}

/// Minimum condensed input size before running another condensed pass.
pub fn resolve_condensed_min_chunk_tokens(config: &CompactionConfig) -> u64 {
    let chunk_target = resolve_leaf_chunk_tokens(config) as u64;
    let ratio_floor = (chunk_target as f64 * CONDENSED_MIN_INPUT_RATIO).floor() as u64;
    (config.condensed_target_tokens as u64).max(ratio_floor)
}

/// Max sweep iterations per phase.
pub fn resolve_max_sweep_iterations(config: &CompactionConfig) -> u32 {
    config.max_sweep_iterations.unwrap_or(50)
}

/// Resolve the timezone string (defaults to "UTC").
pub fn resolve_timezone(config: &CompactionConfig) -> &str {
    config.timezone.as_deref().unwrap_or("UTC")
}

/// Compute the ordinal boundary for protected fresh messages.
///
/// Messages with ordinal >= returned value are preserved as fresh tail.
/// Returns `u64::MAX` if no messages exist or fresh tail count is 0.
pub fn resolve_fresh_tail_ordinal(items: &[ContextItem], fresh_tail_count: u32) -> u64 {
    if fresh_tail_count == 0 {
        return u64::MAX;
    }

    let raw_message_items: Vec<&ContextItem> = items
        .iter()
        .filter(|item| item.item_type == ContextItemType::Message && item.message_id.is_some())
        .collect();

    if raw_message_items.is_empty() {
        return u64::MAX;
    }

    let tail_start_idx = raw_message_items
        .len()
        .saturating_sub(fresh_tail_count as usize);
    raw_message_items
        .get(tail_start_idx)
        .map_or(u64::MAX, |item| item.ordinal)
}

/// Resolve token count for a message with content-length fallback.
pub fn resolve_message_token_count(msg: &MessageRecord) -> u64 {
    if msg.token_count > 0 {
        msg.token_count
    } else {
        estimate_tokens(&msg.content)
    }
}

/// Resolve token count for a summary with content-length fallback.
pub fn resolve_summary_token_count(summary: &SummaryRecord) -> u64 {
    if summary.token_count > 0 {
        summary.token_count
    } else {
        estimate_tokens(&summary.content)
    }
}

/// Select the oldest contiguous raw-message chunk outside fresh tail.
///
/// Returns `(ordinals, message_ids, tokens)` collected in a single pass.
/// The chunk is capped by `chunk_tokens_limit` but always includes at least
/// one message when any compactable message exists.
pub fn select_leaf_chunk(
    items: &[ContextItem],
    messages: &FxHashMap<u64, MessageRecord>,
    fresh_tail_ordinal: u64,
    chunk_tokens_limit: u32,
) -> (Vec<u64>, Vec<u64>, u64) {
    let limit = chunk_tokens_limit as u64;
    let mut ordinals: Vec<u64> = Vec::new();
    let mut message_ids: Vec<u64> = Vec::new();
    let mut chunk_tokens: u64 = 0;
    let mut started = false;

    for item in items {
        if item.ordinal >= fresh_tail_ordinal {
            break;
        }

        if !started {
            if item.item_type != ContextItemType::Message || item.message_id.is_none() {
                continue;
            }
            started = true;
        } else if item.item_type != ContextItemType::Message || item.message_id.is_none() {
            break;
        }

        let Some(msg_id) = item.message_id else {
            continue;
        };

        let msg_tokens = messages.get(&msg_id).map_or(0, resolve_message_token_count);

        if !ordinals.is_empty() && chunk_tokens + msg_tokens > limit {
            break;
        }

        ordinals.push(item.ordinal);
        message_ids.push(msg_id);
        chunk_tokens += msg_tokens;
        if chunk_tokens >= limit {
            break;
        }
    }

    (ordinals, message_ids, chunk_tokens)
}

/// Count raw message tokens outside the fresh tail.
pub fn count_raw_tokens_outside_fresh_tail(
    items: &[ContextItem],
    messages: &FxHashMap<u64, MessageRecord>,
    fresh_tail_ordinal: u64,
) -> u64 {
    let mut total: u64 = 0;
    for item in items {
        if item.ordinal >= fresh_tail_ordinal {
            break;
        }
        if item.item_type != ContextItemType::Message {
            continue;
        }
        if let Some(msg_id) = item.message_id {
            total += messages.get(&msg_id).map_or(0, resolve_message_token_count);
        }
    }
    total
}

/// Select the oldest contiguous summary chunk at a specific depth.
///
/// Once selection starts, any non-summary item or depth mismatch terminates
/// the chunk. Capped by `chunk_tokens_limit`.
///
/// Returns `(ordinals, summary_ids, tokens)` collected in a single pass.
pub fn select_condensed_chunk(
    items: &[ContextItem],
    summaries: &FxHashMap<String, SummaryRecord>,
    target_depth: u32,
    fresh_tail_ordinal: u64,
    chunk_tokens_limit: u32,
) -> (Vec<u64>, Vec<String>, u64) {
    let limit = chunk_tokens_limit as u64;
    let mut ordinals: Vec<u64> = Vec::new();
    let mut summary_ids: Vec<String> = Vec::new();
    let mut summary_tokens: u64 = 0;

    for item in items {
        if item.ordinal >= fresh_tail_ordinal {
            break;
        }
        if item.item_type != ContextItemType::Summary {
            if !ordinals.is_empty() {
                break;
            }
            continue;
        }

        let Some(summary_id) = &item.summary_id else {
            if !ordinals.is_empty() {
                break;
            }
            continue;
        };

        let Some(summary) = summaries.get(summary_id) else {
            if !ordinals.is_empty() {
                break;
            }
            continue;
        };

        if summary.depth != target_depth {
            if !ordinals.is_empty() {
                break;
            }
            continue;
        }

        let token_count = resolve_summary_token_count(summary);
        if !ordinals.is_empty() && summary_tokens + token_count > limit {
            break;
        }

        ordinals.push(item.ordinal);
        summary_ids.push(summary_id.clone());
        summary_tokens += token_count;
        if summary_tokens >= limit {
            break;
        }
    }

    (ordinals, summary_ids, summary_tokens)
}

/// Find the shallowest depth with an eligible condensation chunk.
///
/// Returns `(target_depth, ordinals, summary_ids, chunk_tokens)` or `None`.
pub fn find_shallowest_condensation_candidate(
    items: &[ContextItem],
    summaries: &FxHashMap<String, SummaryRecord>,
    depth_levels: &[u32],
    fresh_tail_ordinal: u64,
    config: &CompactionConfig,
    hard_trigger: bool,
) -> Option<(u32, Vec<u64>, Vec<String>, u64)> {
    let chunk_limit = resolve_leaf_chunk_tokens(config);
    let min_chunk_tokens = resolve_condensed_min_chunk_tokens(config);

    for &target_depth in depth_levels {
        let fanout = resolve_fanout_for_depth(config, target_depth, hard_trigger);
        let (ordinals, summary_ids, tokens) = select_condensed_chunk(
            items,
            summaries,
            target_depth,
            fresh_tail_ordinal,
            chunk_limit,
        );

        if (ordinals.len() as u32) < fanout {
            continue;
        }
        if tokens < min_chunk_tokens {
            continue;
        }
        return Some((target_depth, ordinals, summary_ids, tokens));
    }

    None
}

/// Build leaf source text by concatenating messages with timestamps and roles.
///
/// Format: `[YYYY-MM-DD HH:mm TZ | role]\n{content}\n\n...`
///
/// Including the role helps the summarizer understand conversational flow
/// and preserve episodic structure (who said/did what, in what order).
pub fn build_leaf_source_text(messages: &[MessageRecord], timezone: &str) -> String {
    if messages.is_empty() {
        return String::new();
    }
    // Pre-estimate capacity: timestamp ~25 chars + role ~15 chars + brackets/pipes/newline + content.
    let cap: usize = messages.iter().map(|m| m.content.len() + 45).sum();
    let mut out = String::with_capacity(cap);
    for (i, msg) in messages.iter().enumerate() {
        if i > 0 {
            out.push_str("\n\n");
        }
        let ts = timestamp::format_timestamp(msg.created_at, timezone);
        out.push('[');
        out.push_str(&ts);
        out.push_str(" | ");
        out.push_str(&msg.role);
        out.push_str("]\n");
        out.push_str(&msg.content);
    }
    out
}

/// Build condensed source text by concatenating summaries with time ranges.
///
/// Format: `[start - end]\n{content}\n\n...`
pub fn build_condensed_source_text(summaries: &[SummaryRecord], timezone: &str) -> String {
    if summaries.is_empty() {
        return String::new();
    }
    // Pre-estimate capacity: two timestamps (~25 chars each) + brackets/dashes + content.
    let cap: usize = summaries.iter().map(|s| s.content.len() + 60).sum();
    let mut out = String::with_capacity(cap);
    for (i, summary) in summaries.iter().enumerate() {
        if i > 0 {
            out.push_str("\n\n");
        }
        let earliest = summary.earliest_at.unwrap_or(summary.created_at);
        let latest = summary.latest_at.unwrap_or(summary.created_at);
        out.push('[');
        out.push_str(&timestamp::format_timestamp(earliest, timezone));
        out.push_str(" - ");
        out.push_str(&timestamp::format_timestamp(latest, timezone));
        out.push_str("]\n");
        out.push_str(&summary.content);
    }
    out
}

/// Generate a unique summary ID from content + current timestamp.
pub fn generate_summary_id(content: &str, now_ms: i64) -> String {
    use std::io::Write;
    let mut hasher = sha2::Sha256::new();
    sha2::Digest::update(&mut hasher, content.as_bytes());
    // Write timestamp as string bytes
    let mut ts_buf = Vec::with_capacity(20);
    let _ = write!(ts_buf, "{now_ms}");
    sha2::Digest::update(&mut hasher, &ts_buf);
    let hash = sha2::Digest::finalize(hasher);
    let hex = hex_encode(&hash);
    format!("sum_{}", &hex[..16])
}

/// Deterministic fallback for when LLM summarization fails to reduce tokens.
///
/// Truncates to `FALLBACK_MAX_CHARS` and appends a truncation notice.
pub fn deterministic_fallback(source: &str, input_tokens: u64) -> String {
    let trimmed = source.trim();
    if trimmed.is_empty() {
        return "[Truncated from 0 tokens]".to_string();
    }
    let truncated = if trimmed.len() > FALLBACK_MAX_CHARS {
        &trimmed[..safe_char_boundary(trimmed, FALLBACK_MAX_CHARS)]
    } else {
        trimmed
    };
    format!("{truncated}\n[Truncated from {input_tokens} tokens]")
}

/// Deduplicate IDs preserving order.
pub fn dedupe_ordered_ids(ids: &[&str]) -> Vec<String> {
    let mut seen = std::collections::HashSet::new();
    let mut result = Vec::new();
    for &id in ids {
        if seen.insert(id) {
            result.push(id.to_string());
        }
    }
    result
}

/// Compute aggregate descendant counts for a condensed summary from child summaries.
///
/// Returns `(descendant_count, descendant_token_count, source_message_token_count)`.
pub fn compute_descendant_counts(summaries: &[&SummaryRecord]) -> (u64, u64, u64) {
    let mut descendant_count: u64 = 0;
    let mut descendant_token_count: u64 = 0;
    let mut source_message_token_count: u64 = 0;

    for s in summaries {
        descendant_count += s.descendant_count + 1;
        descendant_token_count += s.token_count + s.descendant_token_count;
        source_message_token_count += s.source_message_token_count;
    }

    (
        descendant_count,
        descendant_token_count,
        source_message_token_count,
    )
}

/// Resolve prior leaf summary context: up to 2 most recent summary items
/// that precede the given start ordinal.
pub fn resolve_prior_summary_ids(
    items: &[ContextItem],
    start_ordinal: u64,
    max_count: usize,
) -> Vec<String> {
    items
        .iter()
        .filter(|item| item.ordinal < start_ordinal && item.item_type == ContextItemType::Summary)
        .filter_map(|item| item.summary_id.clone())
        .collect::<Vec<_>>()
        .into_iter()
        .rev()
        .take(max_count)
        .collect::<Vec<_>>()
        .into_iter()
        .rev()
        .collect()
}

/// Resolve prior summary context at a specific depth: up to `max_count`
/// most recent same-depth summaries before `start_ordinal`.
pub fn resolve_prior_summary_ids_at_depth(
    items: &[ContextItem],
    summaries: &FxHashMap<String, SummaryRecord>,
    start_ordinal: u64,
    target_depth: u32,
    max_count: usize,
) -> Vec<String> {
    items
        .iter()
        .filter(|item| item.ordinal < start_ordinal && item.item_type == ContextItemType::Summary)
        .filter(|item| {
            item.summary_id
                .as_ref()
                .and_then(|id| summaries.get(id))
                .is_some_and(|s| s.depth == target_depth)
        })
        .filter_map(|item| item.summary_id.clone())
        .collect::<Vec<_>>()
        .into_iter()
        .rev()
        .take(max_count)
        .collect::<Vec<_>>()
        .into_iter()
        .rev()
        .collect()
}

// ── Internal helpers ────────────────────────────────────────────────────────

/// Find a safe char boundary at or before `max_bytes`.
fn safe_char_boundary(s: &str, max_bytes: usize) -> usize {
    if max_bytes >= s.len() {
        return s.len();
    }
    let mut boundary = max_bytes;
    while boundary > 0 && !s.is_char_boundary(boundary) {
        boundary -= 1;
    }
    boundary
}

/// Encode bytes as hex string.
fn hex_encode(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

// ── Tests ───────────────────────────────────────────────────────────────────
