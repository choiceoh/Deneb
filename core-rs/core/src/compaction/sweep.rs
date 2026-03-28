//! Step-based sweep state machine for context compaction.
//!
//! The `SweepEngine` drives the compaction algorithm as a state machine that
//! yields I/O commands (`SweepCommand`) to the host language (TypeScript/Go).
//! The host executes the command (DB read, LLM call, DB write) and feeds the
//! result back via `SweepResponse`, then calls `step()` for the next command.
//!
//! This design avoids callbacks across FFI boundaries while keeping all
//! algorithmic logic in Rust.
//!
//! ## State Machine Diagram
//!
//! ```text
//!  ┌────────────────────────────────────────────────────────────────────────┐
//!  │                         SweepEngine FSM                                │
//!  └────────────────────────────────────────────────────────────────────────┘
//!
//!  start() → FetchTokenCount
//!
//!  ┌──────┐
//!  │ Init │──(count ≤ threshold && !force)──────────────────────────► Done
//!  └──────┘
//!      │ (count > threshold || force)
//!      ▼
//!  ┌────────────────┐
//!  │ FetchingItems  │──(items empty)──────────────────────────────── ► Done
//!  └────────────────┘
//!      │ (msg IDs present)       │ (no msg IDs)
//!      ▼                         │
//!  ┌──────────────────────┐      │
//!  │ PrefetchingMessages  │      │
//!  └──────────────────────┘      │
//!      └──────────────────────────┘
//!      ▼
//!  ══════════════════════════════ Phase 1: Leaf passes ══════════════════════
//!
//!  ┌────────────┐ ◄─────────────────────────────────────────────────────────┐
//!  │ LeafSelect │──(iter ≥ max || no chunk)──► transition_to_condensed_phase │
//!  └────────────┘                                                            │
//!      │ (prior summaries needed & uncached)                                 │
//!      ▼                                                                     │
//!  ┌───────────────────────┐                                                 │
//!  │ LeafFetchPriorSumm.   │                                                 │
//!  └───────────────────────┘                                                 │
//!      │ (either path above)                                                 │
//!      ▼  [prepare_leaf_summarize]                                           │
//!  ┌──────────────────────┐                                                  │
//!  │ LeafSummarizeNormal  │──(summary < source tokens)──► LeafPersist path  │
//!  └──────────────────────┘                                                  │
//!      │ (not reduced enough)                                                │
//!      ▼                                                                     │
//!  ┌────────────────────────────┐                                            │
//!  │ LeafSummarizeAggressive    │──(any outcome)─────► LeafPersist path     │
//!  └────────────────────────────┘                                            │
//!      ▼  [prepare_leaf_persist]                                             │
//!  ┌─────────────┐                                                           │
//!  │ LeafPersist │                                                           │
//!  └─────────────┘                                                           │
//!      ▼                                                                     │
//!  ┌──────────────────────┐                                                  │
//!  │ LeafPostTokenCount   │                                                  │
//!  └──────────────────────┘                                                  │
//!      ▼                                                                     │
//!  ┌───────────────────┐                                                     │
//!  │ LeafPersistEvent  │──► FetchingItems ──────────────────────────────────┘
//!  └───────────────────┘    (re-fetch after mutation, loop back to LeafSelect)
//!
//!  ══════════════════════════════ Phase 2: Condensed passes ════════════════
//!  (entered when leaf iterations exhausted or no more leaf chunks)
//!
//!  ┌───────────────────────┐
//!  │ CondensedFetchDepths  │──(iter ≥ max || no candidate)─────────► Done
//!  └───────────────────────┘
//!      │ (summaries missing from cache)
//!      ▼
//!  ┌─────────────────────────┐
//!  │ CondensedFetchSummaries │──(no candidate found)──────────────► Done
//!  └─────────────────────────┘
//!      │ (candidate found; either path above converges here)
//!      ▼  [prepare_condensed_summarize]
//!  ┌──────────────────────────────┐
//!  │ CondensedFetchPriorSummaries │  (depth=0 only, if uncached)
//!  └──────────────────────────────┘
//!      │ (either path above)
//!      ▼  [emit_condensed_summarize]
//!  ┌─────────────────────────────┐
//!  │ CondensedSummarizeNormal    │──(summary < source tokens)──► Condensed persist path
//!  └─────────────────────────────┘
//!      │ (not reduced enough)
//!      ▼
//!  ┌─────────────────────────────────┐
//!  │ CondensedSummarizeAggressive    │──(any outcome)──► Condensed persist path
//!  └─────────────────────────────────┘
//!      ▼  [prepare_condensed_persist]
//!  ┌──────────────────┐
//!  │ CondensedPersist │
//!  └──────────────────┘
//!      ▼
//!  ┌─────────────────────────┐
//!  │ CondensedPostTokenCount │
//!  └─────────────────────────┘
//!      ▼
//!  ┌──────────────────────────┐
//!  │ CondensedPersistEvent    │──► FetchingItems ──► LeafSelect
//!  └──────────────────────────┘    (re-fetch; LeafSelect re-enters condensed
//!                                   phase since leaf_iter already at max)
//!  ┌──────┐
//!  │ Done │  (yields SweepCommand::Done { result: CompactionResult })
//!  └──────┘
//! ```
//!
//! ### Summarization escalation (both phases)
//!
//! Each summarization step tries three levels in order, stopping at the first
//! that produces a smaller token count than the source:
//!
//! ```text
//!  Normal ──(not smaller)──► Aggressive ──(not smaller)──► Fallback (deterministic)
//! ```

use super::*;
use serde::{Deserialize, Serialize};
use std::sync::Arc;

// ── I/O protocol ────────────────────────────────────────────────────────────

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
        messages: HashMap<u64, MessageRecord>,
    },
    Summaries {
        summaries: HashMap<String, SummaryRecord>,
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
enum Phase {
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

/// The sweep state machine engine.
#[derive(Debug)]
pub struct SweepEngine {
    config: CompactionConfig,
    conversation_id: u64,
    token_budget: u64,
    force: bool,
    hard_trigger: bool,
    phase: Phase,

    // Accumulated state
    tokens_before: u64,
    previous_tokens: u64,
    action_taken: bool,
    condensed: bool,
    created_summary_id: Option<String>,
    level: Option<CompactionLevel>,

    // Cached data from host
    context_items: Vec<ContextItem>,
    messages: HashMap<u64, MessageRecord>,
    summaries: HashMap<String, SummaryRecord>,
    fresh_tail_ordinal: u64, // u64::MAX = sentinel for "no fresh tail protection"

    // Per-pass state
    leaf_iter: u32,
    condensed_iter: u32,
    current_chunk_ordinals: Vec<u64>,
    current_chunk_message_ids: Vec<u64>,
    current_chunk_summary_ids: Vec<String>,
    current_source_text: Arc<str>,
    current_source_tokens: u64,
    current_summary_content: String,
    current_pass_tokens_before: u64,
    previous_summary_content: Option<Arc<str>>,
    current_target_depth: u32,

    // Timestamp for summary ID generation
    now_ms: i64,
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
            messages: HashMap::new(),
            summaries: HashMap::new(),
            fresh_tail_ordinal: u64::MAX,
            leaf_iter: 0,
            condensed_iter: 0,
            current_chunk_ordinals: Vec::new(),
            current_chunk_message_ids: Vec::new(),
            current_chunk_summary_ids: Vec::new(),
            current_source_text: Arc::from(""),
            current_source_tokens: 0,
            current_summary_content: String::new(),
            current_pass_tokens_before: 0,
            previous_summary_content: None,
            current_target_depth: 0,
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

    // ── Phase handlers ──────────────────────────────────────────────────────

    fn handle_init(&mut self, response: SweepResponse) -> SweepCommand {
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

    fn handle_fetching_items(&mut self, response: SweepResponse) -> SweepCommand {
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

    fn handle_prefetching_messages(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::Messages { messages } = response {
            self.messages = messages;
        }
        self.phase = Phase::LeafSelect;
        self.start_leaf_select()
    }

    // ── Phase 1: Leaf passes ────────────────────────────────────────────────

    fn start_leaf_select(&mut self) -> SweepCommand {
        let max_iter = resolve_max_sweep_iterations(&self.config);
        if self.leaf_iter >= max_iter {
            return self.transition_to_condensed_phase();
        }

        let chunk_limit = resolve_leaf_chunk_tokens(&self.config);
        let (chunk, _tokens) = select_leaf_chunk(
            &self.context_items,
            &self.messages,
            self.fresh_tail_ordinal,
            chunk_limit,
        );

        if chunk.is_empty() {
            return self.transition_to_condensed_phase();
        }

        // Collect chunk ordinals and message IDs
        self.current_chunk_ordinals = chunk.iter().map(|item| item.ordinal).collect();
        self.current_chunk_message_ids = chunk.iter().filter_map(|item| item.message_id).collect();

        // Check if we need to fetch prior summaries
        let start_ordinal = self
            .current_chunk_ordinals
            .iter()
            .copied()
            .min()
            .unwrap_or(0);
        let prior_ids = resolve_prior_summary_ids(&self.context_items, start_ordinal, 2);

        if !prior_ids.is_empty() && self.previous_summary_content.is_none() {
            // Need to fetch prior summaries
            let unfetched: Vec<String> = prior_ids
                .iter()
                .filter(|id| !self.summaries.contains_key(*id))
                .cloned()
                .collect();

            if !unfetched.is_empty() {
                self.phase = Phase::LeafFetchPriorSummaries;
                return SweepCommand::FetchSummaries {
                    summary_ids: unfetched,
                };
            }

            // All already cached, build context
            let context: Vec<String> = prior_ids
                .iter()
                .filter_map(|id| self.summaries.get(id))
                .map(|s| s.content.trim().to_string())
                .filter(|s| !s.is_empty())
                .collect();
            if !context.is_empty() {
                self.previous_summary_content = Some(Arc::from(context.join("\n\n")));
            }
        }

        self.prepare_leaf_summarize()
    }

    fn handle_leaf_fetch_prior_summaries(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::Summaries { summaries } = response {
            for (id, s) in summaries {
                self.summaries.insert(id, s);
            }
        }

        // Build context from now-cached summaries
        let start_ordinal = self
            .current_chunk_ordinals
            .iter()
            .copied()
            .min()
            .unwrap_or(0);
        let prior_ids = resolve_prior_summary_ids(&self.context_items, start_ordinal, 2);
        let context: Vec<String> = prior_ids
            .iter()
            .filter_map(|id| self.summaries.get(id))
            .map(|s| s.content.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect();
        if !context.is_empty() {
            self.previous_summary_content = Some(Arc::from(context.join("\n\n")));
        }

        self.prepare_leaf_summarize()
    }

    fn prepare_leaf_summarize(&mut self) -> SweepCommand {
        // Build source text from messages in chunk
        let msgs: Vec<MessageRecord> = self
            .current_chunk_message_ids
            .iter()
            .filter_map(|id| self.messages.get(id).cloned())
            .collect();

        let tz = resolve_timezone(&self.config);
        self.current_source_text = Arc::from(build_leaf_source_text(&msgs, tz));
        self.current_source_tokens = estimate_tokens(&self.current_source_text).max(1);

        if self.current_source_text.trim().is_empty() {
            // Nothing to summarize — use fallback
            self.current_summary_content =
                deterministic_fallback(&self.current_source_text, self.current_source_tokens);
            self.level = Some(CompactionLevel::Fallback);
            return self.prepare_leaf_persist();
        }

        self.phase = Phase::LeafSummarizeNormal;
        SweepCommand::Summarize {
            text: self.current_source_text.clone(),
            aggressive: false,
            options: Some(SummarizeOptions {
                previous_summary: self.previous_summary_content.clone(),
                is_condensed: Some(false),
                depth: None,
                target_tokens: None,
            }),
        }
    }

    fn handle_leaf_summarize_normal(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::SummaryText { text } = response {
            let summary_tokens = estimate_tokens(&text);
            if summary_tokens < self.current_source_tokens {
                // Normal succeeded
                self.current_summary_content = text;
                self.level = Some(CompactionLevel::Normal);
                return self.prepare_leaf_persist();
            }

            // Escalate to aggressive
            self.phase = Phase::LeafSummarizeAggressive;
            return SweepCommand::Summarize {
                text: self.current_source_text.clone(),
                aggressive: true,
                options: Some(SummarizeOptions {
                    previous_summary: self.previous_summary_content.clone(),
                    is_condensed: Some(false),
                    depth: None,
                    target_tokens: None,
                }),
            };
        }
        // Unexpected response — fallback
        self.current_summary_content =
            deterministic_fallback(&self.current_source_text, self.current_source_tokens);
        self.level = Some(CompactionLevel::Fallback);
        self.prepare_leaf_persist()
    }

    fn handle_leaf_summarize_aggressive(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::SummaryText { text } = response {
            let summary_tokens = estimate_tokens(&text);
            if summary_tokens < self.current_source_tokens {
                self.current_summary_content = text;
                self.level = Some(CompactionLevel::Aggressive);
                return self.prepare_leaf_persist();
            }
        }
        // Deterministic fallback
        self.current_summary_content =
            deterministic_fallback(&self.current_source_text, self.current_source_tokens);
        self.level = Some(CompactionLevel::Fallback);
        self.prepare_leaf_persist()
    }

    fn prepare_leaf_persist(&mut self) -> SweepCommand {
        let summary_id = generate_summary_id(&self.current_summary_content, self.now_ms);
        let token_count = estimate_tokens(&self.current_summary_content);

        let msgs: Vec<&MessageRecord> = self
            .current_chunk_message_ids
            .iter()
            .filter_map(|id| self.messages.get(id))
            .collect();

        let source_message_tokens: u64 = msgs.iter().map(|m| resolve_message_token_count(m)).sum();

        let earliest_at = msgs.iter().map(|m| m.created_at).min();
        let latest_at = msgs.iter().map(|m| m.created_at).max();

        let start_ordinal = self
            .current_chunk_ordinals
            .iter()
            .copied()
            .min()
            .unwrap_or(0);
        let end_ordinal = self
            .current_chunk_ordinals
            .iter()
            .copied()
            .max()
            .unwrap_or(0);

        self.created_summary_id = Some(summary_id.clone());
        self.previous_summary_content = Some(Arc::from(self.current_summary_content.as_str()));
        self.current_pass_tokens_before = self.previous_tokens;

        self.phase = Phase::LeafPersist;
        SweepCommand::PersistLeafSummary {
            input: PersistLeafInput {
                summary_id,
                conversation_id: self.conversation_id,
                content: self.current_summary_content.clone(),
                token_count,
                file_ids: Vec::new(), // File ID extraction stays in host
                earliest_at,
                latest_at,
                source_message_token_count: source_message_tokens,
                message_ids: self.current_chunk_message_ids.clone(),
                start_ordinal,
                end_ordinal,
            },
        }
    }

    fn handle_leaf_persist(&mut self, _response: SweepResponse) -> SweepCommand {
        self.action_taken = true;
        self.leaf_iter += 1;

        // Fetch updated token count
        self.phase = Phase::LeafPostTokenCount;
        SweepCommand::FetchTokenCount {
            conversation_id: self.conversation_id,
        }
    }

    fn handle_leaf_post_token_count(&mut self, response: SweepResponse) -> SweepCommand {
        let tokens_after = match response {
            SweepResponse::TokenCount { count } => count,
            _ => self.previous_tokens,
        };

        // Persist event (best-effort)
        self.phase = Phase::LeafPersistEvent;
        SweepCommand::PersistEvent {
            input: PersistEventInput {
                conversation_id: self.conversation_id,
                pass: "leaf".to_string(),
                level: self.level.unwrap_or(CompactionLevel::Normal),
                tokens_before: self.current_pass_tokens_before,
                tokens_after,
                created_summary_id: self.created_summary_id.clone().unwrap_or_default(),
            },
        }
    }

    fn handle_leaf_persist_event(&mut self, _response: SweepResponse) -> SweepCommand {
        // Re-fetch context items after mutation
        self.phase = Phase::FetchingItems;
        SweepCommand::FetchContextItems {
            conversation_id: self.conversation_id,
        }
    }

    // ── Phase 2: Condensed passes ───────────────────────────────────────────

    fn transition_to_condensed_phase(&mut self) -> SweepCommand {
        self.condensed_iter = 0;
        self.phase = Phase::CondensedFetchDepths;
        SweepCommand::FetchDistinctDepths {
            conversation_id: self.conversation_id,
            max_ordinal: self.fresh_tail_ordinal,
        }
    }

    fn handle_condensed_fetch_depths(&mut self, response: SweepResponse) -> SweepCommand {
        let max_iter = resolve_max_sweep_iterations(&self.config);
        if self.condensed_iter >= max_iter {
            self.phase = Phase::Done;
            return self.fetch_final_token_count();
        }

        if let SweepResponse::DistinctDepths { depths } = response {
            // We need summaries to evaluate candidates
            let summary_ids: Vec<String> = self
                .context_items
                .iter()
                .filter(|item| {
                    item.ordinal < self.fresh_tail_ordinal
                        && item.item_type == ContextItemType::Summary
                })
                .filter_map(|item| item.summary_id.clone())
                .filter(|id| !self.summaries.contains_key(id))
                .collect();

            if !summary_ids.is_empty() {
                // Need to fetch missing summaries before evaluating candidates
                self.phase = Phase::CondensedFetchSummaries;
                return SweepCommand::FetchSummaries { summary_ids };
            }

            // All summaries cached — find candidate
            if let Some((target_depth, chunk, _tokens)) = find_shallowest_condensation_candidate(
                &self.context_items,
                &self.summaries,
                &depths,
                self.fresh_tail_ordinal,
                &self.config,
                self.hard_trigger,
            ) {
                self.current_target_depth = target_depth;
                self.current_chunk_ordinals = chunk.iter().map(|item| item.ordinal).collect();
                self.current_chunk_summary_ids = chunk
                    .iter()
                    .filter_map(|item| item.summary_id.clone())
                    .collect();
                return self.prepare_condensed_summarize();
            }
        }

        self.phase = Phase::Done;
        self.fetch_final_token_count()
    }

    fn handle_condensed_fetch_summaries(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::Summaries { summaries } = response {
            for (id, s) in summaries {
                self.summaries.insert(id, s);
            }
        }

        // Now find candidate with all summaries available
        let depths: Vec<u32> = {
            let mut d: Vec<u32> = self
                .context_items
                .iter()
                .filter(|item| {
                    item.ordinal < self.fresh_tail_ordinal
                        && item.item_type == ContextItemType::Summary
                })
                .filter_map(|item| {
                    item.summary_id
                        .as_ref()
                        .and_then(|id| self.summaries.get(id))
                        .map(|s| s.depth)
                })
                .collect();
            d.sort_unstable();
            d.dedup();
            d
        };

        if let Some((target_depth, chunk, _tokens)) = find_shallowest_condensation_candidate(
            &self.context_items,
            &self.summaries,
            &depths,
            self.fresh_tail_ordinal,
            &self.config,
            self.hard_trigger,
        ) {
            self.current_target_depth = target_depth;
            self.current_chunk_ordinals = chunk.iter().map(|item| item.ordinal).collect();
            self.current_chunk_summary_ids = chunk
                .iter()
                .filter_map(|item| item.summary_id.clone())
                .collect();
            return self.prepare_condensed_summarize();
        }

        self.phase = Phase::Done;
        self.fetch_final_token_count()
    }

    fn prepare_condensed_summarize(&mut self) -> SweepCommand {
        // Check if we need prior summary context (for depth 0)
        if self.current_target_depth == 0 {
            let start_ordinal = self
                .current_chunk_ordinals
                .iter()
                .copied()
                .min()
                .unwrap_or(0);
            let prior_ids = resolve_prior_summary_ids_at_depth(
                &self.context_items,
                &self.summaries,
                start_ordinal,
                self.current_target_depth,
                2,
            );
            let unfetched: Vec<String> = prior_ids
                .iter()
                .filter(|id| !self.summaries.contains_key(*id))
                .cloned()
                .collect();
            if !unfetched.is_empty() {
                self.phase = Phase::CondensedFetchPriorSummaries;
                return SweepCommand::FetchSummaries {
                    summary_ids: unfetched,
                };
            }

            let context: Vec<String> = prior_ids
                .iter()
                .filter_map(|id| self.summaries.get(id))
                .filter(|s| s.depth == self.current_target_depth)
                .map(|s| s.content.trim().to_string())
                .filter(|s| !s.is_empty())
                .collect();
            if !context.is_empty() {
                self.previous_summary_content = Some(Arc::from(context.join("\n\n")));
            } else {
                self.previous_summary_content = None;
            }
        } else {
            self.previous_summary_content = None;
        }

        self.emit_condensed_summarize()
    }

    fn handle_condensed_fetch_prior_summaries(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::Summaries { summaries } = response {
            for (id, s) in summaries {
                self.summaries.insert(id, s);
            }
        }

        // Build context
        let start_ordinal = self
            .current_chunk_ordinals
            .iter()
            .copied()
            .min()
            .unwrap_or(0);
        let prior_ids = resolve_prior_summary_ids_at_depth(
            &self.context_items,
            &self.summaries,
            start_ordinal,
            self.current_target_depth,
            2,
        );
        let context: Vec<String> = prior_ids
            .iter()
            .filter_map(|id| self.summaries.get(id))
            .filter(|s| s.depth == self.current_target_depth)
            .map(|s| s.content.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect();
        if !context.is_empty() {
            self.previous_summary_content = Some(Arc::from(context.join("\n\n")));
        }

        self.emit_condensed_summarize()
    }

    fn emit_condensed_summarize(&mut self) -> SweepCommand {
        let recs: Vec<SummaryRecord> = self
            .current_chunk_summary_ids
            .iter()
            .filter_map(|id| self.summaries.get(id).cloned())
            .collect();

        let tz = resolve_timezone(&self.config);
        self.current_source_text = Arc::from(build_condensed_source_text(&recs, tz));
        self.current_source_tokens = estimate_tokens(&self.current_source_text).max(1);

        if self.current_source_text.trim().is_empty() {
            self.current_summary_content =
                deterministic_fallback(&self.current_source_text, self.current_source_tokens);
            self.level = Some(CompactionLevel::Fallback);
            return self.prepare_condensed_persist();
        }

        self.phase = Phase::CondensedSummarizeNormal;
        SweepCommand::Summarize {
            text: self.current_source_text.clone(),
            aggressive: false,
            options: Some(SummarizeOptions {
                previous_summary: self.previous_summary_content.clone(),
                is_condensed: Some(true),
                depth: Some(self.current_target_depth + 1),
                target_tokens: None,
            }),
        }
    }

    fn handle_condensed_summarize_normal(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::SummaryText { text } = response {
            let summary_tokens = estimate_tokens(&text);
            if summary_tokens < self.current_source_tokens {
                self.current_summary_content = text;
                self.level = Some(CompactionLevel::Normal);
                return self.prepare_condensed_persist();
            }

            self.phase = Phase::CondensedSummarizeAggressive;
            return SweepCommand::Summarize {
                text: self.current_source_text.clone(),
                aggressive: true,
                options: Some(SummarizeOptions {
                    previous_summary: self.previous_summary_content.clone(),
                    is_condensed: Some(true),
                    depth: Some(self.current_target_depth + 1),
                    target_tokens: None,
                }),
            };
        }
        self.current_summary_content =
            deterministic_fallback(&self.current_source_text, self.current_source_tokens);
        self.level = Some(CompactionLevel::Fallback);
        self.prepare_condensed_persist()
    }

    fn handle_condensed_summarize_aggressive(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::SummaryText { text } = response {
            let summary_tokens = estimate_tokens(&text);
            if summary_tokens < self.current_source_tokens {
                self.current_summary_content = text;
                self.level = Some(CompactionLevel::Aggressive);
                return self.prepare_condensed_persist();
            }
        }
        self.current_summary_content =
            deterministic_fallback(&self.current_source_text, self.current_source_tokens);
        self.level = Some(CompactionLevel::Fallback);
        self.prepare_condensed_persist()
    }

    fn prepare_condensed_persist(&mut self) -> SweepCommand {
        let summary_id = generate_summary_id(&self.current_summary_content, self.now_ms);
        let token_count = estimate_tokens(&self.current_summary_content);

        let recs: Vec<&SummaryRecord> = self
            .current_chunk_summary_ids
            .iter()
            .filter_map(|id| self.summaries.get(id))
            .collect();

        let (descendant_count, descendant_token_count, source_message_token_count) =
            compute_descendant_counts(&recs.iter().map(|r| (*r).clone()).collect::<Vec<_>>());

        let earliest_at = recs
            .iter()
            .map(|s| s.earliest_at.unwrap_or(s.created_at))
            .min();
        let latest_at = recs
            .iter()
            .map(|s| s.latest_at.unwrap_or(s.created_at))
            .max();

        let start_ordinal = self
            .current_chunk_ordinals
            .iter()
            .copied()
            .min()
            .unwrap_or(0);
        let end_ordinal = self
            .current_chunk_ordinals
            .iter()
            .copied()
            .max()
            .unwrap_or(0);

        self.created_summary_id = Some(summary_id.clone());
        self.current_pass_tokens_before = self.previous_tokens;

        self.phase = Phase::CondensedPersist;
        SweepCommand::PersistCondensedSummary {
            input: PersistCondensedInput {
                summary_id,
                conversation_id: self.conversation_id,
                depth: self.current_target_depth + 1,
                content: self.current_summary_content.clone(),
                token_count,
                file_ids: Vec::new(), // File ID extraction stays in host
                earliest_at,
                latest_at,
                descendant_count,
                descendant_token_count,
                source_message_token_count,
                parent_summary_ids: self.current_chunk_summary_ids.clone(),
                start_ordinal,
                end_ordinal,
            },
        }
    }

    fn handle_condensed_persist(&mut self, _response: SweepResponse) -> SweepCommand {
        self.action_taken = true;
        self.condensed = true;
        self.condensed_iter += 1;

        self.phase = Phase::CondensedPostTokenCount;
        SweepCommand::FetchTokenCount {
            conversation_id: self.conversation_id,
        }
    }

    fn handle_condensed_post_token_count(&mut self, response: SweepResponse) -> SweepCommand {
        let tokens_after = match response {
            SweepResponse::TokenCount { count } => count,
            _ => self.previous_tokens,
        };

        self.phase = Phase::CondensedPersistEvent;
        SweepCommand::PersistEvent {
            input: PersistEventInput {
                conversation_id: self.conversation_id,
                pass: "condensed".to_string(),
                level: self.level.unwrap_or(CompactionLevel::Normal),
                tokens_before: self.current_pass_tokens_before,
                tokens_after,
                created_summary_id: self.created_summary_id.clone().unwrap_or_default(),
            },
        }
    }

    fn handle_condensed_persist_event(&mut self, _response: SweepResponse) -> SweepCommand {
        // Re-fetch items and continue condensed phase
        self.phase = Phase::FetchingItems;
        SweepCommand::FetchContextItems {
            conversation_id: self.conversation_id,
        }
    }

    // ── Terminal ─────────────────────────────────────────────────────────────

    fn fetch_final_token_count(&mut self) -> SweepCommand {
        // We already have the latest token count from the last persist check.
        // Return done directly.
        self.done_result()
    }

    fn done_result(&self) -> SweepCommand {
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

// ── Tests ───────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    fn default_config() -> CompactionConfig {
        CompactionConfig::default()
    }

    #[test]
    fn test_sweep_below_threshold_returns_done() {
        let mut engine = SweepEngine::new(default_config(), 1, 1000, false, false, 1000);
        let cmd = engine.start();
        assert!(matches!(cmd, SweepCommand::FetchTokenCount { .. }));

        // Report tokens below threshold (750)
        let cmd = engine.step(SweepResponse::TokenCount { count: 500 });
        match cmd {
            SweepCommand::Done { result } => {
                assert!(!result.action_taken);
                assert_eq!(result.tokens_before, 500);
            }
            _ => panic!("Expected Done, got {:?}", cmd),
        }
    }

    #[test]
    fn test_sweep_empty_items_returns_done() {
        let mut engine = SweepEngine::new(default_config(), 1, 1000, false, false, 1000);
        let _ = engine.start();
        let cmd = engine.step(SweepResponse::TokenCount { count: 800 });
        assert!(matches!(cmd, SweepCommand::FetchContextItems { .. }));

        let cmd = engine.step(SweepResponse::ContextItems { items: vec![] });
        match cmd {
            SweepCommand::Done { result } => {
                assert!(!result.action_taken);
            }
            _ => panic!("Expected Done, got {:?}", cmd),
        }
    }

    #[test]
    fn test_sweep_force_skips_threshold() {
        let mut engine = SweepEngine::new(default_config(), 1, 1000, true, false, 1000);
        let _ = engine.start();

        // Even below threshold, force should proceed
        let cmd = engine.step(SweepResponse::TokenCount { count: 500 });
        assert!(matches!(cmd, SweepCommand::FetchContextItems { .. }));
    }

    #[test]
    fn test_sweep_command_serde_roundtrip() {
        let cmd = SweepCommand::FetchMessages {
            message_ids: vec![1, 2, 3],
        };
        let json = serde_json::to_string(&cmd).unwrap();
        let parsed: SweepCommand = serde_json::from_str(&json).unwrap();
        match parsed {
            SweepCommand::FetchMessages { message_ids } => {
                assert_eq!(message_ids, vec![1, 2, 3]);
            }
            _ => panic!("Wrong variant"),
        }
    }

    #[test]
    fn test_sweep_response_serde_roundtrip() {
        let resp = SweepResponse::TokenCount { count: 42 };
        let json = serde_json::to_string(&resp).unwrap();
        let parsed: SweepResponse = serde_json::from_str(&json).unwrap();
        match parsed {
            SweepResponse::TokenCount { count } => assert_eq!(count, 42),
            _ => panic!("Wrong variant"),
        }
    }
}
