//! I/O protocol types for the sweep state machine.
//!
//! Defines the command/response protocol between the sweep engine (Rust) and
//! the host (Go/TypeScript). The host executes DB/LLM operations described by
//! `SweepCommand` and feeds results back via `SweepResponse`.

use super::super::*;
use rustc_hash::FxHashMap;
use serde::{Deserialize, Serialize};
use std::sync::Arc;

// ── Persist inputs ─────────────────────────────────────────────────────────

/// Options for the LLM summarization call.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SummarizeOptions {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub previous_summary: Option<Arc<str>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub is_condensed: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub depth: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub target_tokens: Option<u32>,
}

/// Input for persisting a leaf summary.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PersistLeafInput {
    pub summary_id: String,
    pub conversation_id: u64,
    pub content: String,
    pub token_count: u64,
    pub file_ids: Vec<String>,
    pub earliest_at: Option<i64>,
    pub latest_at: Option<i64>,
    pub source_message_token_count: u64,
    pub message_ids: Vec<u64>,
    pub start_ordinal: u64,
    pub end_ordinal: u64,
}

/// Input for persisting a condensed summary.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PersistCondensedInput {
    pub summary_id: String,
    pub conversation_id: u64,
    pub depth: u32,
    pub content: String,
    pub token_count: u64,
    pub file_ids: Vec<String>,
    pub earliest_at: Option<i64>,
    pub latest_at: Option<i64>,
    pub descendant_count: u64,
    pub descendant_token_count: u64,
    pub source_message_token_count: u64,
    pub parent_summary_ids: Vec<String>,
    pub start_ordinal: u64,
    pub end_ordinal: u64,
}

/// Input for persisting a compaction event.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PersistEventInput {
    pub conversation_id: u64,
    pub pass: String,
    pub level: CompactionLevel,
    pub tokens_before: u64,
    pub tokens_after: u64,
    pub created_summary_id: String,
}

// ── Commands & Responses ───────────────────────────────────────────────────

/// Command yielded by the sweep engine for the host to execute.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "camelCase")]
pub enum SweepCommand {
    /// Fetch all context items for the conversation.
    FetchContextItems { conversation_id: u64 },
    /// Fetch messages by their IDs.
    FetchMessages { message_ids: Vec<u64> },
    /// Fetch summaries by their IDs.
    FetchSummaries { summary_ids: Vec<String> },
    /// Fetch the total context token count.
    FetchTokenCount { conversation_id: u64 },
    /// Fetch distinct summary depths in context (below `max_ordinal`).
    FetchDistinctDepths {
        conversation_id: u64,
        max_ordinal: u64,
    },
    /// Call the LLM summarizer.
    Summarize {
        text: Arc<str>,
        aggressive: bool,
        #[serde(skip_serializing_if = "Option::is_none")]
        options: Option<SummarizeOptions>,
    },
    /// Persist a leaf summary (insert + link + replace in single tx).
    PersistLeafSummary { input: PersistLeafInput },
    /// Persist a condensed summary.
    PersistCondensedSummary { input: PersistCondensedInput },
    /// Persist a compaction event (best-effort).
    PersistEvent { input: PersistEventInput },
    /// Sweep is complete.
    Done { result: CompactionResult },
}

/// Response from the host after executing a command.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "camelCase")]
pub enum SweepResponse {
    ContextItems {
        items: Vec<ContextItem>,
    },
    Messages {
        messages: FxHashMap<u64, MessageRecord>,
    },
    Summaries {
        summaries: FxHashMap<String, SummaryRecord>,
    },
    TokenCount {
        count: u64,
    },
    DistinctDepths {
        depths: Vec<u32>,
    },
    SummaryText {
        text: String,
    },
    PersistOk,
    PersistError {
        error: String,
    },
}
