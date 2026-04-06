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
        match &self.phase {
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
    /// 1. Split items into `fresh_tail` (last N, always included) + evictable (rest)
    /// 2. Fresh tail tokens are always included even if exceeding budget
    /// 3. Fill remaining budget from evictable, newest→oldest
    /// 4. Reverse to restore chronological order
    fn compute_selection(&mut self) {
        let total = self.context_items.len();
        let tail_count = (self.fresh_tail_count as usize).min(total);
        let split_idx = total.saturating_sub(tail_count);

        // Fresh tail tokens (always included)
        let fresh_tail_tokens: u64 = self.context_items[split_idx..]
            .iter()
            .map(|item| item.token_count)
            .sum();

        let remaining_budget = self.token_budget.saturating_sub(fresh_tail_tokens);

        // Walk evictable newest→oldest, tracking the start index of the selected range.
        // Using an index avoids an intermediate Vec<&AssemblyContextItem>; the
        // selected items are always a contiguous suffix of `evictable`, so we only
        // need to record where that suffix begins.
        let mut evictable_tokens: u64 = 0;
        // evict_start == split_idx means no evictable items selected yet.
        let mut evict_start = split_idx;

        for i in (0..split_idx).rev() {
            let count = self.context_items[i].token_count;
            if evictable_tokens + count > remaining_budget && evict_start < split_idx {
                // Budget exhausted after at least one item has been selected — stop.
                break;
            }
            evictable_tokens += count;
            evict_start = i;
        }

        // Clone once from the selected contiguous range (evictable suffix + fresh_tail).
        // items[evict_start..split_idx] are already in chronological order.
        self.selected_items = self.context_items[evict_start..].to_vec();

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

Some earlier conversation has been summarized with structured sections \
(Goal, Progress, Key Decisions, Next Steps, Critical Context). \
Check **Goal** for the overarching objective and **Next Steps** for \
unresolved items from earlier context.

If you need details from summarized sections, use recall tools:

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

### Structured Sections

Summaries contain structured sections: Goal, Progress, Key Decisions, \
Relevant Files, Next Steps, Critical Context.
- Check **Goal** for the user's overarching objective — this anchors all work
- Check **Key Decisions** before re-discussing topics that were already resolved
- Check **Next Steps** for items that still need attention
- **Critical Context** contains verbatim values (errors, configs, versions) that must not be paraphrased

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
