//! Context assembly state machine — DAG-aware token budgeting.
//!
//! The `AssemblyEngine` drives context selection as a state machine that yields
//! I/O commands (`AssemblyCommand`) to the host (TypeScript/Go). The host
//! executes the command (DB read, message resolution) and feeds the result
//! back via `AssemblyResponse`, then calls `step()` for the next command.
//!
//! This mirrors the compaction sweep pattern: all algorithmic logic in Rust,
//! all I/O in the host.

use super::*;
use serde::{Deserialize, Serialize};

// ── I/O protocol ─────────────────────────────────────────────────────────────

/// A context item record from the DB (for assembly).
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AssemblyContextItem {
    pub ordinal: u64,
    pub item_type: ResolvedItemKind,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub message_id: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub summary_id: Option<String>,
    pub token_count: u64,
    /// Summary depth (0 for messages and leaf summaries).
    #[serde(default)]
    pub depth: u32,
    /// Whether this is a condensed summary.
    #[serde(default)]
    pub is_condensed: bool,
}

/// Command yielded by the assembly engine for the host to execute.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "camelCase")]
pub enum AssemblyCommand {
    /// Fetch all context items for the conversation (ordered by ordinal).
    FetchContextItems { conversation_id: u64 },

    /// Fetch summary stats for system prompt guidance.
    FetchSummaryStats { conversation_id: u64 },

    /// Assembly is complete. The host should use these results.
    Done { result: AssembleResult },
}

/// Response from the host after executing an assembly command.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "camelCase")]
pub enum AssemblyResponse {
    /// Context items for the conversation.
    ContextItems { items: Vec<AssemblyContextItem> },

    /// Summary statistics for system prompt guidance.
    SummaryStats { stats: SummaryStats },
}

// ── Internal state ───────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
enum Phase {
    /// Initial: fetch context items.
    Init,
    /// Context items received, compute selection.
    FetchingSummaryStats,
    /// Done.
    Done,
}

/// The assembly state machine engine.
///
/// Implements the DAG-aware context selection algorithm:
/// 1. Fetch all context items (ordered by ordinal)
/// 2. Split into fresh tail (protected) + evictable (budget-driven)
/// 3. Walk evictable newest→oldest, accumulating tokens until budget full
/// 4. Emit selected items + system prompt guidance
#[derive(Debug)]
pub struct AssemblyEngine {
    conversation_id: u64,
    token_budget: u64,
    fresh_tail_count: u32,
    phase: Phase,

    // Computed state
    context_items: Vec<AssemblyContextItem>,
    selected_items: Vec<AssemblyContextItem>,
    estimated_tokens: u64,
    summary_stats: Option<SummaryStats>,
}

impl AssemblyEngine {
    /// Create a new assembly engine.
    pub fn new(conversation_id: u64, token_budget: u64, fresh_tail_count: u32) -> Self {
        Self {
            conversation_id,
            token_budget,
            fresh_tail_count,
            phase: Phase::Init,
            context_items: Vec::new(),
            selected_items: Vec::new(),
            estimated_tokens: 0,
            summary_stats: None,
        }
    }

    /// Start the assembly. Returns the first I/O command.
    pub fn start(&mut self) -> AssemblyCommand {
        self.phase = Phase::Init;
        AssemblyCommand::FetchContextItems {
            conversation_id: self.conversation_id,
        }
    }

    /// Advance the state machine with a host response. Returns the next command.
    pub fn step(&mut self, response: AssemblyResponse) -> AssemblyCommand {
        match self.phase.clone() {
            Phase::Init => self.handle_context_items(response),
            Phase::FetchingSummaryStats => self.handle_summary_stats(response),
            Phase::Done => self.done_result(),
        }
    }

    // ── Phase handlers ───────────────────────────────────────────────────────

    fn handle_context_items(&mut self, response: AssemblyResponse) -> AssemblyCommand {
        if let AssemblyResponse::ContextItems { items } = response {
            self.context_items = items;

            if self.context_items.is_empty() {
                self.phase = Phase::Done;
                return self.done_result();
            }

            // Run the selection algorithm
            self.compute_selection();

            // Check if we have summaries — if so, fetch stats for system prompt
            let has_summaries = self
                .selected_items
                .iter()
                .any(|item| item.item_type == ResolvedItemKind::Summary);

            if has_summaries {
                self.phase = Phase::FetchingSummaryStats;
                return AssemblyCommand::FetchSummaryStats {
                    conversation_id: self.conversation_id,
                };
            }

            self.phase = Phase::Done;
            return self.done_result();
        }

        self.phase = Phase::Done;
        self.done_result()
    }

    fn handle_summary_stats(&mut self, response: AssemblyResponse) -> AssemblyCommand {
        if let AssemblyResponse::SummaryStats { stats } = response {
            self.summary_stats = Some(stats);
        }
        self.phase = Phase::Done;
        self.done_result()
    }

    // ── Core algorithm ───────────────────────────────────────────────────────

    /// DAG-aware context selection under token budget.
    ///
    /// Algorithm:
    /// 1. Split items into fresh_tail (last N, always included) + evictable (rest)
    /// 2. Fresh tail tokens are always included even if exceeding budget
    /// 3. Fill remaining budget from evictable, newest→oldest
    /// 4. Reverse to restore chronological order
    fn compute_selection(&mut self) {
        let total = self.context_items.len();
        let tail_count = (self.fresh_tail_count as usize).min(total);
        let split_idx = total.saturating_sub(tail_count);

        let evictable = &self.context_items[..split_idx];
        let fresh_tail = &self.context_items[split_idx..];

        // Fresh tail tokens (always included)
        let fresh_tail_tokens: u64 = fresh_tail.iter().map(|item| item.token_count).sum();

        let remaining_budget = self.token_budget.saturating_sub(fresh_tail_tokens);

        // Walk evictable newest→oldest, accumulating tokens
        let mut selected_evictable: Vec<&AssemblyContextItem> = Vec::new();
        let mut evictable_tokens: u64 = 0;

        for item in evictable.iter().rev() {
            if evictable_tokens + item.token_count > remaining_budget
                && !selected_evictable.is_empty()
            {
                // Budget exhausted — stop including older items
                break;
            }
            selected_evictable.push(item);
            evictable_tokens += item.token_count;
        }

        // Reverse to restore chronological order (oldest first)
        selected_evictable.reverse();

        // Build final selected list
        self.selected_items = selected_evictable
            .iter()
            .map(|item| (*item).clone())
            .chain(fresh_tail.iter().cloned())
            .collect();

        self.estimated_tokens = evictable_tokens + fresh_tail_tokens;
    }

    // ── System prompt guidance ───────────────────────────────────────────────

    /// Build the system prompt addition based on compaction state.
    fn build_system_prompt_addition(&self) -> Option<String> {
        let stats = self.summary_stats.as_ref()?;

        if stats.leaf_count == 0 && stats.condensed_count == 0 {
            return None;
        }

        // Minimal guidance for shallow compaction
        let is_deep = stats.max_depth >= 2 || stats.condensed_count >= 2;

        if !is_deep {
            return Some(MINIMAL_AURORA_GUIDANCE.to_string());
        }

        Some(FULL_AURORA_GUIDANCE.to_string())
    }

    // ── Terminal ──────────────────────────────────────────────────────────────

    fn done_result(&self) -> AssemblyCommand {
        let mut raw_message_count = 0u32;
        let mut summary_count = 0u32;

        let selected_item_ids: Vec<String> = self
            .selected_items
            .iter()
            .map(|item| match item.item_type {
                ResolvedItemKind::Message => {
                    raw_message_count += 1;
                    format!("msg_{}", item.message_id.unwrap_or(0))
                }
                ResolvedItemKind::Summary => {
                    summary_count += 1;
                    item.summary_id.clone().unwrap_or_default()
                }
            })
            .collect();

        AssemblyCommand::Done {
            result: AssembleResult {
                estimated_tokens: self.estimated_tokens,
                raw_message_count,
                summary_count,
                total_context_items: self.context_items.len() as u32,
                system_prompt_addition: self.build_system_prompt_addition(),
                selected_item_ids,
            },
        }
    }
}

// ── System prompt guidance constants ─────────────────────────────────────────

const MINIMAL_AURORA_GUIDANCE: &str = "\
## Context Recall (Aurora)

Some earlier conversation has been summarized. If you need details from \
summarized sections, use the available recall tools:

1. `aurora_grep` — Search messages and summaries (regex or full-text)
2. `aurora_describe` — Inspect a summary's lineage and metadata
3. `aurora_expand_query` — Deep recall via sub-agent expansion (~120s)

Do not guess from summaries — expand or search first when precision matters.";

const FULL_AURORA_GUIDANCE: &str = "\
## Context Recall (Aurora)

Significant conversation history has been hierarchically summarized. \
Condensed summaries compress multiple layers of earlier context.

### Recall Workflow (cheapest → most thorough)

1. **`aurora_grep`** — Regex or full-text search across messages and summaries. \
   Use `scope: \"both\"` for broad searches, `scope: \"messages\"` for exact quotes.

2. **`aurora_describe`** — Inspect a summary's lineage: parents, children, \
   descendant counts, time range. Cheap metadata lookup.

3. **`aurora_expand_query`** — Deep recall that spawns a bounded sub-agent to \
   traverse the summary DAG and answer your question (~120s). Use when:
   - You need details spanning a wide time range
   - Multiple summary layers must be traversed
   - `aurora_grep` results are insufficient

### Usage Patterns

- `aurora_expand_query` with `summaryIds`: expand specific summaries
- `aurora_expand_query` with `query`: search-then-expand in one call
- Add `maxTokens`, `conversationId`, or `allConversations` for scoping

### Precision Rules

When context is heavily compacted (condensed summaries present):
- **Never** present condensed-summary content as exact facts
- **Always** grep → expand → cite before making specific factual claims
- If uncertain whether a detail is exact, note it as \"from summarized context\"

### Uncertainty Checklist

Before stating a fact from a condensed summary, verify:
- [ ] Is this an exact quote or a compression artifact?
- [ ] Could the summary have merged distinct events?
- [ ] Would the user expect this level of precision?

If any answer is uncertain, expand first.";

// ── Tests ────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    fn make_message_item(ordinal: u64, tokens: u64) -> AssemblyContextItem {
        AssemblyContextItem {
            ordinal,
            item_type: ResolvedItemKind::Message,
            message_id: Some(ordinal),
            summary_id: None,
            token_count: tokens,
            depth: 0,
            is_condensed: false,
        }
    }

    fn make_summary_item(ordinal: u64, id: &str, tokens: u64, depth: u32) -> AssemblyContextItem {
        AssemblyContextItem {
            ordinal,
            item_type: ResolvedItemKind::Summary,
            message_id: None,
            summary_id: Some(id.to_string()),
            token_count: tokens,
            depth,
            is_condensed: depth > 0,
        }
    }

    #[test]
    fn test_assembly_empty_items_returns_done() {
        let mut engine = AssemblyEngine::new(1, 10_000, 8);
        let cmd = engine.start();
        assert!(matches!(cmd, AssemblyCommand::FetchContextItems { .. }));

        let cmd = engine.step(AssemblyResponse::ContextItems { items: vec![] });
        match cmd {
            AssemblyCommand::Done { result } => {
                assert_eq!(result.estimated_tokens, 0);
                assert_eq!(result.raw_message_count, 0);
                assert_eq!(result.total_context_items, 0);
            }
            _ => panic!("Expected Done, got {:?}", cmd),
        }
    }

    #[test]
    fn test_assembly_all_messages_fit() {
        let mut engine = AssemblyEngine::new(1, 10_000, 8);
        let _ = engine.start();

        let items: Vec<AssemblyContextItem> = (0..5).map(|i| make_message_item(i, 100)).collect();

        let cmd = engine.step(AssemblyResponse::ContextItems { items });
        // No summaries → straight to Done
        match cmd {
            AssemblyCommand::Done { result } => {
                assert_eq!(result.estimated_tokens, 500);
                assert_eq!(result.raw_message_count, 5);
                assert_eq!(result.summary_count, 0);
                assert_eq!(result.selected_item_ids.len(), 5);
                assert!(result.system_prompt_addition.is_none());
            }
            _ => panic!("Expected Done, got {:?}", cmd),
        }
    }

    #[test]
    fn test_assembly_budget_truncation() {
        let mut engine = AssemblyEngine::new(1, 350, 2);
        let _ = engine.start();

        // 5 messages, 100 tokens each = 500 total
        // Fresh tail = 2 (items 3,4 = 200 tokens)
        // Remaining budget = 150
        // Evictable (items 0,1,2): walk newest first → item 2 (100), item 1 (100 would exceed 150+100=200 > 150... wait)
        // Actually: evictable_tokens starts at 0, item 2 = 100, now 100 < 150, continue
        // item 1 = 100, 100 + 100 = 200 > 150 and selected is non-empty, so stop
        let items: Vec<AssemblyContextItem> = (0..5).map(|i| make_message_item(i, 100)).collect();

        let cmd = engine.step(AssemblyResponse::ContextItems { items });
        match cmd {
            AssemblyCommand::Done { result } => {
                // Fresh tail (2 items = 200) + 1 evictable (100) = 300
                assert_eq!(result.estimated_tokens, 300);
                assert_eq!(result.selected_item_ids.len(), 3);
                // Should be items 2, 3, 4
                assert_eq!(result.selected_item_ids[0], "msg_2");
                assert_eq!(result.selected_item_ids[1], "msg_3");
                assert_eq!(result.selected_item_ids[2], "msg_4");
            }
            _ => panic!("Expected Done, got {:?}", cmd),
        }
    }

    #[test]
    fn test_assembly_fresh_tail_always_included() {
        // Budget = 50, but fresh tail has 200 tokens → still included
        // Evictable item 0 (100 tokens) also included because we always
        // include at least one evictable item when any exist.
        let mut engine = AssemblyEngine::new(1, 50, 2);
        let _ = engine.start();

        let items: Vec<AssemblyContextItem> = (0..3).map(|i| make_message_item(i, 100)).collect();

        let cmd = engine.step(AssemblyResponse::ContextItems { items });
        match cmd {
            AssemblyCommand::Done { result } => {
                // Fresh tail (items 1,2 = 200) + 1 evictable (item 0 = 100) = 300
                assert_eq!(result.estimated_tokens, 300);
                assert_eq!(result.selected_item_ids.len(), 3);
            }
            _ => panic!("Expected Done, got {:?}", cmd),
        }
    }

    #[test]
    fn test_assembly_with_summaries_fetches_stats() {
        let mut engine = AssemblyEngine::new(1, 10_000, 8);
        let _ = engine.start();

        let items = vec![
            make_summary_item(0, "sum_abc", 500, 1),
            make_message_item(1, 100),
            make_message_item(2, 100),
        ];

        let cmd = engine.step(AssemblyResponse::ContextItems { items });
        // Has summaries → should fetch summary stats
        assert!(matches!(cmd, AssemblyCommand::FetchSummaryStats { .. }));

        // Provide stats
        let cmd = engine.step(AssemblyResponse::SummaryStats {
            stats: SummaryStats {
                max_depth: 1,
                condensed_count: 1,
                leaf_count: 0,
                total_summary_tokens: 500,
            },
        });

        match cmd {
            AssemblyCommand::Done { result } => {
                assert_eq!(result.estimated_tokens, 700);
                assert_eq!(result.summary_count, 1);
                assert_eq!(result.raw_message_count, 2);
                // Has summaries → system prompt addition
                assert!(result.system_prompt_addition.is_some());
            }
            _ => panic!("Expected Done, got {:?}", cmd),
        }
    }

    #[test]
    fn test_assembly_deep_compaction_full_guidance() {
        let mut engine = AssemblyEngine::new(1, 10_000, 8);
        let _ = engine.start();

        let items = vec![
            make_summary_item(0, "sum_deep", 500, 3),
            make_message_item(1, 100),
        ];

        let cmd = engine.step(AssemblyResponse::ContextItems { items });
        assert!(matches!(cmd, AssemblyCommand::FetchSummaryStats { .. }));

        let cmd = engine.step(AssemblyResponse::SummaryStats {
            stats: SummaryStats {
                max_depth: 3,
                condensed_count: 5,
                leaf_count: 10,
                total_summary_tokens: 5000,
            },
        });

        match cmd {
            AssemblyCommand::Done { result } => {
                let guidance = result.system_prompt_addition.unwrap();
                // Deep compaction → full guidance with precision rules
                assert!(guidance.contains("Precision Rules"));
                assert!(guidance.contains("Uncertainty Checklist"));
            }
            _ => panic!("Expected Done"),
        }
    }

    #[test]
    fn test_assembly_shallow_compaction_minimal_guidance() {
        let mut engine = AssemblyEngine::new(1, 10_000, 8);
        let _ = engine.start();

        let items = vec![
            make_summary_item(0, "sum_leaf", 500, 0),
            make_message_item(1, 100),
        ];

        let cmd = engine.step(AssemblyResponse::ContextItems { items });
        assert!(matches!(cmd, AssemblyCommand::FetchSummaryStats { .. }));

        let cmd = engine.step(AssemblyResponse::SummaryStats {
            stats: SummaryStats {
                max_depth: 0,
                condensed_count: 0,
                leaf_count: 1,
                total_summary_tokens: 500,
            },
        });

        match cmd {
            AssemblyCommand::Done { result } => {
                let guidance = result.system_prompt_addition.unwrap();
                // Shallow → minimal guidance
                assert!(guidance.contains("Context Recall (Aurora)"));
                assert!(!guidance.contains("Precision Rules"));
            }
            _ => panic!("Expected Done"),
        }
    }

    #[test]
    fn test_assembly_command_serde_roundtrip() {
        let cmd = AssemblyCommand::FetchContextItems {
            conversation_id: 42,
        };
        let json = serde_json::to_string(&cmd).unwrap();
        let parsed: AssemblyCommand = serde_json::from_str(&json).unwrap();
        match parsed {
            AssemblyCommand::FetchContextItems { conversation_id } => {
                assert_eq!(conversation_id, 42);
            }
            _ => panic!("Wrong variant"),
        }
    }

    #[test]
    fn test_assembly_response_serde_roundtrip() {
        let resp = AssemblyResponse::ContextItems {
            items: vec![make_message_item(0, 100)],
        };
        let json = serde_json::to_string(&resp).unwrap();
        let parsed: AssemblyResponse = serde_json::from_str(&json).unwrap();
        match parsed {
            AssemblyResponse::ContextItems { items } => {
                assert_eq!(items.len(), 1);
                assert_eq!(items[0].token_count, 100);
            }
            _ => panic!("Wrong variant"),
        }
    }
}
