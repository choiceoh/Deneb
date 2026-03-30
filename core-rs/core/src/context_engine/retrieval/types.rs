//! Shared types and command/response protocol for retrieval operations.
//!
//! Defines the data types used by all three retrieval engines (grep, describe,
//! expand) and the command/response enums that form the I/O protocol between
//! the retrieval engine (Rust) and the host (Go/TypeScript).

use serde::{Deserialize, Serialize};

// ── Shared types ─────────────────────────────────────────────────────────────

/// Search mode for grep operations.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum GrepMode {
    Regex,
    FullText,
}

/// Scope of a grep search.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum GrepScope {
    Messages,
    Summaries,
    Both,
}

/// A grep match result.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct GrepMatch {
    /// Source type: "message" or "summary".
    pub source: String,
    /// ID of the matched item.
    pub id: String,
    /// Matched content snippet.
    pub snippet: String,
    /// Token count of the snippet.
    pub token_count: u64,
    /// Epoch milliseconds of creation.
    pub created_at: i64,
    /// Optional FTS5 rank score.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub rank: Option<f64>,
}

/// Summary lineage node for describe results.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct LineageNode {
    pub summary_id: String,
    pub kind: String,
    pub depth: u32,
    pub token_count: u64,
    pub descendant_count: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub earliest_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub latest_at: Option<i64>,
}

/// Result of a describe operation.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DescribeResult {
    pub id: String,
    /// "summary" or "file".
    pub item_type: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub node: Option<LineageNode>,
    /// Parent summary IDs.
    pub parents: Vec<String>,
    /// Child summary IDs.
    pub children: Vec<String>,
    /// Source message IDs (for leaf summaries).
    pub message_ids: Vec<u64>,
    /// Subtree path nodes.
    pub subtree: Vec<LineageNode>,
}

/// Result of a grep operation.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct GrepResult {
    pub matches: Vec<GrepMatch>,
    pub total_matches: u32,
}

/// Child item in an expand result.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ExpandChild {
    pub summary_id: String,
    pub kind: String,
    pub content: String,
    pub token_count: u64,
}

/// Message item in an expand result.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ExpandMessage {
    pub message_id: u64,
    pub role: String,
    pub content: String,
    pub token_count: u64,
}

/// Result of an expand operation.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ExpandResult {
    pub children: Vec<ExpandChild>,
    pub messages: Vec<ExpandMessage>,
    pub estimated_tokens: u64,
    pub truncated: bool,
}

// ── Retrieval command/response protocol ──────────────────────────────────────

/// Command yielded by the retrieval engine.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "camelCase")]
pub enum RetrievalCommand {
    /// Execute a grep search.
    Grep {
        query: String,
        mode: GrepMode,
        scope: GrepScope,
        #[serde(skip_serializing_if = "Option::is_none")]
        conversation_id: Option<u64>,
        #[serde(skip_serializing_if = "Option::is_none")]
        since_ms: Option<i64>,
        #[serde(skip_serializing_if = "Option::is_none")]
        before_ms: Option<i64>,
        #[serde(skip_serializing_if = "Option::is_none")]
        limit: Option<u32>,
    },

    /// Fetch summary lineage for describe.
    FetchLineage { summary_id: String },

    /// Fetch a summary record for expand.
    FetchSummary { summary_id: String },

    /// Fetch children of a summary for expand.
    FetchChildren { summary_id: String },

    /// Fetch source messages of a leaf summary for expand.
    FetchSourceMessages { summary_id: String },

    /// Grep operation complete.
    GrepDone { result: GrepResult },

    /// Describe operation complete.
    DescribeDone { result: DescribeResult },

    /// Expand operation complete.
    ExpandDone { result: ExpandResult },
}

/// Response from the host after executing a retrieval command.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "camelCase")]
pub enum RetrievalResponse {
    /// Grep search results.
    GrepResults {
        matches: Vec<GrepMatch>,
        total_matches: u32,
    },

    /// Summary lineage for describe.
    Lineage {
        node: Option<LineageNode>,
        parents: Vec<String>,
        children: Vec<String>,
        message_ids: Vec<u64>,
        subtree: Vec<LineageNode>,
    },

    /// A single summary record.
    Summary {
        summary_id: String,
        kind: String,
        depth: u32,
        content: String,
        token_count: u64,
    },

    /// Children of a summary.
    Children { children: Vec<ExpandChild> },

    /// Source messages of a leaf summary.
    SourceMessages { messages: Vec<ExpandMessage> },
}
