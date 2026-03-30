//! Core sweep engine struct, state machine phases, and dispatch logic.

use super::super::*;
use super::types::*;
use rustc_hash::FxHashMap;
use std::sync::Arc;

// ── Internal state ──────────────────────────────────────────────────────────

/// State machine phases for the compaction sweep.
///
/// The sweep proceeds in two phases:
/// 1. **Leaf passes** — summarize raw messages into depth-0 summaries.
/// 2. **Condensed passes** — merge existing summaries into higher-depth summaries.
///
/// Each phase follows: select chunk → fetch context → LLM summarize (normal →
/// aggressive → fallback) → persist → check if budget is satisfied → repeat.
///
/// See the module-level doc for the full state diagram.
///
/// Key loops:
/// - After each leaf persist: `LeafPersistEvent → FetchingItems → LeafSelect`
/// - After each condensed persist: `CondensedPersistEvent → FetchingItems → LeafSelect → CondensedFetchDepths`
/// - Loop exits when no more chunks (leaf) or no more candidates (condensed).
#[derive(Debug, Clone)]
pub(super) enum Phase {
    /// Initial: fetch token count to evaluate threshold.
    Init,
    /// Fetch context items for the sweep.
    FetchingItems,
    /// Prefetch messages referenced by context items.
    PrefetchingMessages,

    // Phase 1: Leaf passes — summarize raw messages into depth-0 summaries.
    /// Select leaf chunk and prepare for summarization.
    LeafSelect,
    /// Fetch prior summary context for the leaf pass.
    LeafFetchPriorSummaries,
    /// Waiting for LLM normal summarization result.
    LeafSummarizeNormal,
    /// Waiting for LLM aggressive summarization result.
    LeafSummarizeAggressive,
    /// Persist the leaf summary.
    LeafPersist,
    /// Fetch token count after leaf persist.
    LeafPostTokenCount,
    /// Persist leaf compaction event.
    LeafPersistEvent,

    // Phase 2: Condensed passes — merge existing summaries into higher-depth summaries.
    /// Fetch distinct depths for condensation candidate search.
    CondensedFetchDepths,
    /// Fetch summaries for the selected condensation chunk.
    CondensedFetchSummaries,
    /// Fetch prior summaries for condensed context.
    CondensedFetchPriorSummaries,
    /// Waiting for LLM normal summarization for condensed pass.
    CondensedSummarizeNormal,
    /// Waiting for LLM aggressive summarization for condensed pass.
    CondensedSummarizeAggressive,
    /// Persist the condensed summary.
    CondensedPersist,
    /// Fetch token count after condensed persist.
    CondensedPostTokenCount,
    /// Persist condensed compaction event.
    CondensedPersistEvent,

    /// Done.
    Done,
}

/// Per-pass mutable state for the sweep engine. Groups fields that are
/// reset/rewritten on each leaf or condensed compaction pass.
#[derive(Debug)]
pub(super) struct PassState {
    pub(super) leaf_iter: u32,
    pub(super) condensed_iter: u32,
    pub(super) chunk_ordinals: Vec<u64>,
    pub(super) chunk_message_ids: Vec<u64>,
    pub(super) chunk_summary_ids: Vec<String>,
    pub(super) source_text: Arc<str>,
    pub(super) source_tokens: u64,
    pub(super) summary_content: String,
    pub(super) tokens_before: u64,
    pub(super) previous_summary: Option<Arc<str>>,
    pub(super) target_depth: u32,
}

impl Default for PassState {
    fn default() -> Self {
        Self {
            leaf_iter: 0,
            condensed_iter: 0,
            chunk_ordinals: Vec::new(),
            chunk_message_ids: Vec::new(),
            chunk_summary_ids: Vec::new(),
            source_text: Arc::from(""),
            source_tokens: 0,
            summary_content: String::new(),
            tokens_before: 0,
            previous_summary: None,
            target_depth: 0,
        }
    }
}

/// The sweep state machine engine.
#[derive(Debug)]
pub struct SweepEngine {
    pub(super) config: CompactionConfig,
    pub(super) conversation_id: u64,
    pub(super) token_budget: u64,
    pub(super) force: bool,
    pub(super) hard_trigger: bool,
    pub(super) phase: Phase,

    // Accumulated state
    pub(super) tokens_before: u64,
    pub(super) previous_tokens: u64,
    pub(super) action_taken: bool,
    pub(super) condensed: bool,
    pub(super) created_summary_id: Option<String>,
    pub(super) level: Option<CompactionLevel>,

    // Cached data from host
    pub(super) context_items: Vec<ContextItem>,
    pub(super) messages: FxHashMap<u64, MessageRecord>,
    pub(super) summaries: FxHashMap<String, SummaryRecord>,
    pub(super) fresh_tail_ordinal: u64, // u64::MAX = sentinel for "no fresh tail protection"

    // Per-pass state
    pub(super) pass: PassState,

    // Timestamp for summary ID generation
    pub(super) now_ms: i64,
}

impl SweepEngine {
    /// Create a new sweep engine.
    pub fn new(
        config: CompactionConfig,
        conversation_id: u64,
        token_budget: u64,
        force: bool,
        hard_trigger: bool,
        now_ms: i64,
    ) -> Self {
        Self {
            config,
            conversation_id,
            token_budget,
            force,
            hard_trigger,
            phase: Phase::Init,
            tokens_before: 0,
            previous_tokens: 0,
            action_taken: false,
            condensed: false,
            created_summary_id: None,
            level: None,
            context_items: Vec::new(),
            messages: FxHashMap::default(),
            summaries: FxHashMap::default(),
            fresh_tail_ordinal: u64::MAX,
            pass: PassState::default(),
            now_ms,
        }
    }

    /// Start the sweep. Returns the first I/O command.
    pub fn start(&mut self) -> SweepCommand {
        self.phase = Phase::Init;
        SweepCommand::FetchTokenCount {
            conversation_id: self.conversation_id,
        }
    }

    /// Advance the state machine with a host response. Returns the next command.
    pub fn step(&mut self, response: SweepResponse) -> SweepCommand {
        match self.phase.clone() {
            Phase::Init => self.handle_init(response),
            Phase::FetchingItems => self.handle_fetching_items(response),
            Phase::PrefetchingMessages => self.handle_prefetching_messages(response),

            Phase::LeafSelect => self.start_leaf_select(),
            Phase::LeafFetchPriorSummaries => self.handle_leaf_fetch_prior_summaries(response),
            Phase::LeafSummarizeNormal => self.handle_leaf_summarize_normal(response),
            Phase::LeafSummarizeAggressive => self.handle_leaf_summarize_aggressive(response),
            Phase::LeafPersist => self.handle_leaf_persist(response),
            Phase::LeafPostTokenCount => self.handle_leaf_post_token_count(response),
            Phase::LeafPersistEvent => self.handle_leaf_persist_event(response),

            Phase::CondensedFetchDepths => self.handle_condensed_fetch_depths(response),
            Phase::CondensedFetchSummaries => self.handle_condensed_fetch_summaries(response),
            Phase::CondensedFetchPriorSummaries => {
                self.handle_condensed_fetch_prior_summaries(response)
            }
            Phase::CondensedSummarizeNormal => self.handle_condensed_summarize_normal(response),
            Phase::CondensedSummarizeAggressive => {
                self.handle_condensed_summarize_aggressive(response)
            }
            Phase::CondensedPersist => self.handle_condensed_persist(response),
            Phase::CondensedPostTokenCount => self.handle_condensed_post_token_count(response),
            Phase::CondensedPersistEvent => self.handle_condensed_persist_event(response),

            Phase::Done => self.done_result(),
        }
    }

    // ── Init & fetch handlers ──────────────────────────────────────────────

    pub(super) fn handle_init(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::TokenCount { count } = response {
            self.tokens_before = count;
            self.previous_tokens = count;

            let threshold =
                (self.config.context_threshold * self.token_budget as f64).floor() as u64;

            if !self.force && count <= threshold {
                self.phase = Phase::Done;
                return self.done_result();
            }

            self.phase = Phase::FetchingItems;
            return SweepCommand::FetchContextItems {
                conversation_id: self.conversation_id,
            };
        }
        self.phase = Phase::Done;
        self.done_result()
    }

    pub(super) fn handle_fetching_items(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::ContextItems { items } = response {
            if items.is_empty() {
                self.phase = Phase::Done;
                return self.done_result();
            }

            let ftc = resolve_fresh_tail_count(&self.config);
            self.fresh_tail_ordinal = resolve_fresh_tail_ordinal(&items, ftc);
            self.context_items = items;

            // Collect all message IDs for batch prefetch
            let msg_ids: Vec<u64> = self
                .context_items
                .iter()
                .filter_map(|item| item.message_id)
                .collect();

            if msg_ids.is_empty() {
                // No messages to prefetch, go directly to leaf select
                self.phase = Phase::LeafSelect;
                return self.start_leaf_select();
            }

            self.phase = Phase::PrefetchingMessages;
            return SweepCommand::FetchMessages {
                message_ids: msg_ids,
            };
        }
        self.phase = Phase::Done;
        self.done_result()
    }

    pub(super) fn handle_prefetching_messages(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::Messages { messages } = response {
            self.messages = messages;
        }
        self.phase = Phase::LeafSelect;
        self.start_leaf_select()
    }

    // ── Terminal ────────────────────────────────────────────────────────────

    pub(super) fn fetch_final_token_count(&mut self) -> SweepCommand {
        // We already have the latest token count from the last persist check.
        // Return done directly.
        self.done_result()
    }

    pub(super) fn done_result(&self) -> SweepCommand {
        SweepCommand::Done {
            result: CompactionResult {
                action_taken: self.action_taken,
                tokens_before: self.tokens_before,
                tokens_after: self.previous_tokens,
                created_summary_id: self.created_summary_id.clone(),
                condensed: self.condensed,
                level: self.level,
            },
        }
    }
}
