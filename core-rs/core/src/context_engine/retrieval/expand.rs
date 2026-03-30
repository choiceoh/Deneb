//! Expand engine — recursive DAG traversal with token budgeting.
//!
//! Multi-step state machine that traverses the summary DAG depth-first:
//! - Condensed summaries: fetch children, add to result, recurse
//! - Leaf summaries: fetch source messages if requested
//! - Respects token cap and sets `truncated` flag
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

use super::types::*;

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
        match self.phase.clone() {
            ExpandPhase::FetchRoot => self.handle_fetch_root(response),
            ExpandPhase::FetchingChildren => self.handle_fetching_children(response),
            ExpandPhase::ExpandingChild { child_index } => {
                self.handle_expanding_child(response, child_index)
            }
            ExpandPhase::FetchingMessages => self.handle_fetching_messages(response),
            ExpandPhase::Done => self.done_result(),
        }
    }

    pub fn is_done(&self) -> bool {
        matches!(self.phase, ExpandPhase::Done)
    }

    // ── Phase handlers ───────────────────────────────────────────────────────

    fn handle_fetch_root(&mut self, response: RetrievalResponse) -> RetrievalCommand {
        if let RetrievalResponse::Summary { kind, .. } = &response {
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

        if let Some((_summary_id, remaining_depth)) = self.expand_stack.pop() {
            if remaining_depth > 0 {
                self.phase = ExpandPhase::ExpandingChild {
                    child_index: self.child_index,
                };
                return RetrievalCommand::FetchChildren {
                    summary_id: _summary_id,
                };
            }
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
