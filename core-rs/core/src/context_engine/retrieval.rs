//! Retrieval operations — grep, describe, expand via step-based I/O protocol.
//!
//! The `RetrievalEngine` provides three operations for querying the context DAG:
//! - **grep**: Search messages and/or summaries (regex or full-text)
//! - **describe**: Fetch summary/file lineage metadata
//! - **expand**: Traverse the DAG with token budgeting
//!
//! Each operation uses the same command/response pattern as the assembly and
//! compaction sweep engines: Rust drives the algorithm, the host executes I/O.
//!
//! ## State Machine Diagrams
//!
//! ### `GrepEngine` (2 steps, trivial)
//!
//! ```text
//!  start() → Grep{query, mode, scope, ...}
//!
//!  [Start] ──► host executes Grep ──► step(GrepResults) ──► GrepDone
//! ```
//!
//! ### `DescribeEngine` (2 steps, trivial)
//!
//! ```text
//!  start() → FetchLineage{summary_id}
//!
//!  [Start] ──► host fetches lineage ──► step(Lineage) ──► DescribeDone
//! ```
//!
//! ### `ExpandEngine` (multi-step DAG traversal)
//!
//! ```text
//!  start() → FetchSummary{root_summary_id}
//!
//!  ┌───────────┐
//!  │ FetchRoot │
//!  └───────────┘
//!      │ kind="condensed" && max_depth > 0        │ kind="leaf" && include_messages
//!      ▼                                           ▼
//!  ┌──────────────────┐                    ┌─────────────────┐
//!  │ FetchingChildren │                    │ FetchingMessages│
//!  └──────────────────┘                    └─────────────────┘
//!      │                                           │
//!      ▼  [process_next_child]                     ▼
//!   iterate children:                         accumulate messages
//!   • add child to result                     (respects token cap)
//!   • if condensed child && depth>1:               │
//!     push to expand_stack                         ▼
//!   • if token cap hit → truncated=true       ┌──────┐
//!   • when all children done:                 │ Done │
//!     [process_expand_stack]                  └──────┘
//!      │
//!      ▼  [process_expand_stack]
//!   pop from expand_stack:
//!   ┌─────────────────────┐
//!   │ ExpandingChild      │──► FetchChildren{summary_id}
//!   └─────────────────────┘
//!      │ (host returns children; merge into result with token cap check)
//!      │ loop back to process_expand_stack
//!      │
//!      ▼  (stack empty)
//!   if include_messages && any leaf children:
//!   ┌─────────────────┐
//!   │ FetchingMessages│──► accumulate messages ──► Done
//!   └─────────────────┘
//!      │ (no leaf children)
//!      ▼
//!   ┌──────┐
//!   │ Done │  (yields ExpandDone { children, messages, estimated_tokens, truncated })
//!   └──────┘
//!
//!  Early exits (any phase):
//!    • result.truncated=true  → skip remaining children/stack → Done
//!    • FetchRoot: kind neither "condensed"(expandable) nor "leaf"(with msgs) → Done
//! ```

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

// ── Grep engine ──────────────────────────────────────────────────────────────

/// Simple grep engine — delegates search to host, returns results.
#[derive(Debug)]
pub struct GrepEngine {
    query: String,
    mode: GrepMode,
    scope: GrepScope,
    conversation_id: Option<u64>,
    since_ms: Option<i64>,
    before_ms: Option<i64>,
    limit: Option<u32>,
    done: bool,
}

impl GrepEngine {
    pub fn new(
        query: String,
        mode: GrepMode,
        scope: GrepScope,
        conversation_id: Option<u64>,
        since_ms: Option<i64>,
        before_ms: Option<i64>,
        limit: Option<u32>,
    ) -> Self {
        Self {
            query,
            mode,
            scope,
            conversation_id,
            since_ms,
            before_ms,
            limit,
            done: false,
        }
    }

    /// Start the grep search.
    pub fn start(&self) -> RetrievalCommand {
        RetrievalCommand::Grep {
            query: self.query.clone(),
            mode: self.mode,
            scope: self.scope,
            conversation_id: self.conversation_id,
            since_ms: self.since_ms,
            before_ms: self.before_ms,
            limit: self.limit,
        }
    }

    /// Process the host response and return done.
    pub fn step(&mut self, response: RetrievalResponse) -> RetrievalCommand {
        self.done = true;
        if let RetrievalResponse::GrepResults {
            matches,
            total_matches,
        } = response
        {
            RetrievalCommand::GrepDone {
                result: GrepResult {
                    matches,
                    total_matches,
                },
            }
        } else {
            RetrievalCommand::GrepDone {
                result: GrepResult {
                    matches: vec![],
                    total_matches: 0,
                },
            }
        }
    }

    pub fn is_done(&self) -> bool {
        self.done
    }
}

// ── Describe engine ──────────────────────────────────────────────────────────

/// Describe engine — fetches summary lineage from the host.
#[derive(Debug)]
pub struct DescribeEngine {
    id: String,
    done: bool,
}

impl DescribeEngine {
    pub fn new(id: String) -> Self {
        Self { id, done: false }
    }

    /// Start the describe operation.
    pub fn start(&self) -> RetrievalCommand {
        RetrievalCommand::FetchLineage {
            summary_id: self.id.clone(),
        }
    }

    /// Process the host response and return done.
    pub fn step(&mut self, response: RetrievalResponse) -> RetrievalCommand {
        self.done = true;
        if let RetrievalResponse::Lineage {
            node,
            parents,
            children,
            message_ids,
            subtree,
        } = response
        {
            RetrievalCommand::DescribeDone {
                result: DescribeResult {
                    id: self.id.clone(),
                    item_type: if self.id.starts_with("file_") {
                        "file".to_string()
                    } else {
                        "summary".to_string()
                    },
                    node,
                    parents,
                    children,
                    message_ids,
                    subtree,
                },
            }
        } else {
            RetrievalCommand::DescribeDone {
                result: DescribeResult {
                    id: self.id.clone(),
                    item_type: "summary".to_string(),
                    node: None,
                    parents: vec![],
                    children: vec![],
                    message_ids: vec![],
                    subtree: vec![],
                },
            }
        }
    }

    pub fn is_done(&self) -> bool {
        self.done
    }
}

// ── Expand engine ────────────────────────────────────────────────────────────

/// Expand state machine phase.
///
/// Transitions:
/// ```text
///  FetchRoot ──(condensed && depth>0)──► FetchingChildren
///           ──(leaf && include_messages)──► FetchingMessages
///           ──(otherwise)──────────────► Done
///
///  FetchingChildren ──► [process_next_child loop]
///    • all children processed ──► [process_expand_stack]
///    • token cap hit ──────────► Done (truncated=true)
///
///  [process_expand_stack]
///    • stack not empty ──► ExpandingChild (FetchChildren for next condensed child)
///    • stack empty && leaf children && include_messages ──► FetchingMessages
///    • stack empty, no more work ──────────────────────► Done
///
///  ExpandingChild ──(host returns children)──► [merge children] ──► [process_expand_stack]
///
///  FetchingMessages ──(host returns messages)──► Done
/// ```
#[derive(Debug, Clone)]
enum ExpandPhase {
    /// Fetch the root summary to determine kind.
    FetchRoot,
    /// Fetch children of a condensed summary.
    FetchingChildren,
    /// Recursively expanding a child.
    ExpandingChild { child_index: usize },
    /// Fetch source messages of a leaf summary.
    FetchingMessages,
    /// Done.
    Done,
}

/// Expand engine — recursive DAG traversal with token budgeting.
///
/// Traverses the summary DAG depth-first:
/// - Condensed summaries: fetch children, add to result, recurse
/// - Leaf summaries: fetch source messages if requested
/// - Respects token cap and sets `truncated` flag
#[derive(Debug)]
pub struct ExpandEngine {
    root_summary_id: String,
    max_depth: u32,
    include_messages: bool,
    token_cap: u64,
    phase: ExpandPhase,

    // Accumulated result
    result: ExpandResult,

    // Per-level state
    current_children: Vec<ExpandChild>,
    child_index: usize,
    /// Stack of (`summary_id`, `remaining_depth`) for iterative DFS.
    expand_stack: Vec<(String, u32)>,
}

impl ExpandEngine {
    pub fn new(summary_id: String, max_depth: u32, include_messages: bool, token_cap: u64) -> Self {
        Self {
            root_summary_id: summary_id,
            max_depth,
            include_messages,
            token_cap,
            phase: ExpandPhase::FetchRoot,
            result: ExpandResult {
                children: vec![],
                messages: vec![],
                estimated_tokens: 0,
                truncated: false,
            },
            current_children: vec![],
            child_index: 0,
            expand_stack: vec![],
        }
    }

    /// Start the expand operation.
    pub fn start(&mut self) -> RetrievalCommand {
        self.phase = ExpandPhase::FetchRoot;
        RetrievalCommand::FetchSummary {
            summary_id: self.root_summary_id.clone(),
        }
    }

    /// Advance the state machine.
    pub fn step(&mut self, response: RetrievalResponse) -> RetrievalCommand {
        match &self.phase {
            ExpandPhase::FetchRoot => self.handle_fetch_root(&response),
            ExpandPhase::FetchingChildren => self.handle_fetching_children(response),
            ExpandPhase::ExpandingChild { child_index } => {
                self.handle_expanding_child(response, *child_index)
            }
            ExpandPhase::FetchingMessages => self.handle_fetching_messages(response),
            ExpandPhase::Done => self.done_result(),
        }
    }

    pub fn is_done(&self) -> bool {
        matches!(self.phase, ExpandPhase::Done)
    }

    // ── Phase handlers ───────────────────────────────────────────────────────

    fn handle_fetch_root(&mut self, response: &RetrievalResponse) -> RetrievalCommand {
        if let RetrievalResponse::Summary { kind, .. } = response {
            if kind == "condensed" && self.max_depth > 0 {
                // Fetch children for recursive expansion
                self.phase = ExpandPhase::FetchingChildren;
                return RetrievalCommand::FetchChildren {
                    summary_id: self.root_summary_id.clone(),
                };
            } else if kind == "leaf" && self.include_messages {
                // Fetch source messages
                self.phase = ExpandPhase::FetchingMessages;
                return RetrievalCommand::FetchSourceMessages {
                    summary_id: self.root_summary_id.clone(),
                };
            }
        }

        self.phase = ExpandPhase::Done;
        self.done_result()
    }

    fn handle_fetching_children(&mut self, response: RetrievalResponse) -> RetrievalCommand {
        if let RetrievalResponse::Children { children } = response {
            self.current_children = children;
            self.child_index = 0;
            return self.process_next_child();
        }

        self.phase = ExpandPhase::Done;
        self.done_result()
    }

    fn process_next_child(&mut self) -> RetrievalCommand {
        if self.result.truncated {
            self.phase = ExpandPhase::Done;
            return self.done_result();
        }

        if self.child_index >= self.current_children.len() {
            // All children processed — continue with expand stack
            return self.process_expand_stack();
        }

        let child = &self.current_children[self.child_index];

        // Check token cap
        if self.result.estimated_tokens + child.token_count > self.token_cap
            && !self.result.children.is_empty()
        {
            self.result.truncated = true;
            self.phase = ExpandPhase::Done;
            return self.done_result();
        }

        // Add child to result
        self.result.children.push(child.clone());
        self.result.estimated_tokens += child.token_count;

        // If depth allows, push to expand stack for later traversal
        if self.max_depth > 1 && child.kind == "condensed" {
            self.expand_stack
                .push((child.summary_id.clone(), self.max_depth - 1));
        }

        self.child_index += 1;
        self.process_next_child()
    }

    fn process_expand_stack(&mut self) -> RetrievalCommand {
        if self.result.truncated {
            self.phase = ExpandPhase::Done;
            return self.done_result();
        }

        while let Some((summary_id, remaining_depth)) = self.expand_stack.pop() {
            if remaining_depth > 0 {
                self.phase = ExpandPhase::ExpandingChild {
                    child_index: self.child_index,
                };
                return RetrievalCommand::FetchChildren { summary_id };
            }
            // remaining_depth == 0: skip this item, keep draining stack
        }

        // Stack empty — check if we should fetch messages for leaf children
        if self.include_messages {
            // Find leaf children that need message expansion
            let leaf_children: Vec<String> = self
                .result
                .children
                .iter()
                .filter(|c| c.kind == "leaf")
                .map(|c| c.summary_id.clone())
                .collect();

            if let Some(first_leaf) = leaf_children.first() {
                self.phase = ExpandPhase::FetchingMessages;
                return RetrievalCommand::FetchSourceMessages {
                    summary_id: first_leaf.clone(),
                };
            }
        }

        self.phase = ExpandPhase::Done;
        self.done_result()
    }

    fn handle_expanding_child(
        &mut self,
        response: RetrievalResponse,
        _child_index: usize,
    ) -> RetrievalCommand {
        if let RetrievalResponse::Children { children } = response {
            for child in children {
                if self.result.truncated {
                    break;
                }

                if self.result.estimated_tokens + child.token_count > self.token_cap
                    && !self.result.children.is_empty()
                {
                    self.result.truncated = true;
                    break;
                }

                self.result.estimated_tokens += child.token_count;
                self.result.children.push(child);
            }
        }

        self.process_expand_stack()
    }

    fn handle_fetching_messages(&mut self, response: RetrievalResponse) -> RetrievalCommand {
        if let RetrievalResponse::SourceMessages { messages } = response {
            for msg in messages {
                if self.result.truncated {
                    break;
                }

                if self.result.estimated_tokens + msg.token_count > self.token_cap
                    && (!self.result.messages.is_empty() || !self.result.children.is_empty())
                {
                    self.result.truncated = true;
                    break;
                }

                self.result.estimated_tokens += msg.token_count;
                self.result.messages.push(msg);
            }
        }

        self.phase = ExpandPhase::Done;
        self.done_result()
    }

    fn done_result(&self) -> RetrievalCommand {
        RetrievalCommand::ExpandDone {
            result: self.result.clone(),
        }
    }
}

// ── Tests ────────────────────────────────────────────────────────────────────
