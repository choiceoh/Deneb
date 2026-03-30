//! Leaf pass phase handlers for the sweep engine.
//!
//! Phase 1 of compaction: summarize raw messages into depth-0 summaries.
//! Follows the select → fetch prior → summarize (normal → aggressive → fallback)
//! → persist → post-token-count → persist-event cycle.

use super::super::*;
use super::engine::{Phase, SweepEngine};
use super::types::*;
use std::sync::Arc;

impl SweepEngine {
    pub(super) fn start_leaf_select(&mut self) -> SweepCommand {
        let max_iter = resolve_max_sweep_iterations(&self.config);
        if self.pass.leaf_iter >= max_iter {
            return self.transition_to_condensed_phase();
        }

        let chunk_limit = resolve_leaf_chunk_tokens(&self.config);
        let (ordinals, message_ids, _tokens) = select_leaf_chunk(
            &self.context_items,
            &self.messages,
            self.fresh_tail_ordinal,
            chunk_limit,
        );

        if ordinals.is_empty() {
            return self.transition_to_condensed_phase();
        }

        self.pass.chunk_ordinals = ordinals;
        self.pass.chunk_message_ids = message_ids;

        // Check if we need to fetch prior summaries
        let start_ordinal = self.pass.chunk_ordinals.iter().copied().min().unwrap_or(0);
        let prior_ids = resolve_prior_summary_ids(&self.context_items, start_ordinal, 2);

        if !prior_ids.is_empty() && self.pass.previous_summary.is_none() {
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
                self.pass.previous_summary = Some(Arc::from(context.join("\n\n")));
            }
        }

        self.prepare_leaf_summarize()
    }

    pub(super) fn handle_leaf_fetch_prior_summaries(
        &mut self,
        response: SweepResponse,
    ) -> SweepCommand {
        if let SweepResponse::Summaries { summaries } = response {
            for (id, s) in summaries {
                self.summaries.insert(id, s);
            }
        }

        // Build context from now-cached summaries
        let start_ordinal = self.pass.chunk_ordinals.iter().copied().min().unwrap_or(0);
        let prior_ids = resolve_prior_summary_ids(&self.context_items, start_ordinal, 2);
        let context: Vec<String> = prior_ids
            .iter()
            .filter_map(|id| self.summaries.get(id))
            .map(|s| s.content.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect();
        if !context.is_empty() {
            self.pass.previous_summary = Some(Arc::from(context.join("\n\n")));
        }

        self.prepare_leaf_summarize()
    }

    fn prepare_leaf_summarize(&mut self) -> SweepCommand {
        // Build source text from messages in chunk
        let msgs: Vec<MessageRecord> = self
            .pass
            .chunk_message_ids
            .iter()
            .filter_map(|id| self.messages.get(id).cloned())
            .collect();

        let tz = resolve_timezone(&self.config);
        self.pass.source_text = Arc::from(build_leaf_source_text(&msgs, tz));
        self.pass.source_tokens = estimate_tokens(&self.pass.source_text).max(1);

        if self.pass.source_text.trim().is_empty() {
            // Nothing to summarize — use fallback
            self.pass.summary_content =
                deterministic_fallback(&self.pass.source_text, self.pass.source_tokens);
            self.level = Some(CompactionLevel::Fallback);
            return self.prepare_leaf_persist();
        }

        self.phase = Phase::LeafSummarizeNormal;
        SweepCommand::Summarize {
            text: self.pass.source_text.clone(),
            aggressive: false,
            options: Some(SummarizeOptions {
                previous_summary: self.pass.previous_summary.clone(),
                is_condensed: Some(false),
                depth: None,
                target_tokens: None,
            }),
        }
    }

    pub(super) fn handle_leaf_summarize_normal(&mut self, response: SweepResponse) -> SweepCommand {
        if let SweepResponse::SummaryText { text } = response {
            let summary_tokens = estimate_tokens(&text);
            if summary_tokens < self.pass.source_tokens {
                // Normal succeeded
                self.pass.summary_content = text;
                self.level = Some(CompactionLevel::Normal);
                return self.prepare_leaf_persist();
            }

            // Escalate to aggressive
            self.phase = Phase::LeafSummarizeAggressive;
            return SweepCommand::Summarize {
                text: self.pass.source_text.clone(),
                aggressive: true,
                options: Some(SummarizeOptions {
                    previous_summary: self.pass.previous_summary.clone(),
                    is_condensed: Some(false),
                    depth: None,
                    target_tokens: None,
                }),
            };
        }
        // Unexpected response — fallback
        self.pass.summary_content =
            deterministic_fallback(&self.pass.source_text, self.pass.source_tokens);
        self.level = Some(CompactionLevel::Fallback);
        self.prepare_leaf_persist()
    }

    pub(super) fn handle_leaf_summarize_aggressive(
        &mut self,
        response: SweepResponse,
    ) -> SweepCommand {
        if let SweepResponse::SummaryText { text } = response {
            let summary_tokens = estimate_tokens(&text);
            if summary_tokens < self.pass.source_tokens {
                self.pass.summary_content = text;
                self.level = Some(CompactionLevel::Aggressive);
                return self.prepare_leaf_persist();
            }
        }
        // Deterministic fallback
        self.pass.summary_content =
            deterministic_fallback(&self.pass.source_text, self.pass.source_tokens);
        self.level = Some(CompactionLevel::Fallback);
        self.prepare_leaf_persist()
    }

    fn prepare_leaf_persist(&mut self) -> SweepCommand {
        let summary_id = generate_summary_id(&self.pass.summary_content, self.now_ms);
        let token_count = estimate_tokens(&self.pass.summary_content);

        let msgs: Vec<&MessageRecord> = self
            .pass
            .chunk_message_ids
            .iter()
            .filter_map(|id| self.messages.get(id))
            .collect();

        let source_message_tokens: u64 = msgs.iter().map(|m| resolve_message_token_count(m)).sum();

        let earliest_at = msgs.iter().map(|m| m.created_at).min();
        let latest_at = msgs.iter().map(|m| m.created_at).max();

        let start_ordinal = self.pass.chunk_ordinals.iter().copied().min().unwrap_or(0);
        let end_ordinal = self.pass.chunk_ordinals.iter().copied().max().unwrap_or(0);

        self.created_summary_id = Some(summary_id.clone());
        self.pass.previous_summary = Some(Arc::from(self.pass.summary_content.as_str()));
        self.pass.tokens_before = self.previous_tokens;

        self.phase = Phase::LeafPersist;
        SweepCommand::PersistLeafSummary {
            input: PersistLeafInput {
                summary_id,
                conversation_id: self.conversation_id,
                content: self.pass.summary_content.clone(),
                token_count,
                file_ids: Vec::new(), // File ID extraction stays in host
                earliest_at,
                latest_at,
                source_message_token_count: source_message_tokens,
                message_ids: self.pass.chunk_message_ids.clone(),
                start_ordinal,
                end_ordinal,
            },
        }
    }

    pub(super) fn handle_leaf_persist(&mut self, _response: SweepResponse) -> SweepCommand {
        self.action_taken = true;
        self.pass.leaf_iter += 1;

        // Fetch updated token count
        self.phase = Phase::LeafPostTokenCount;
        SweepCommand::FetchTokenCount {
            conversation_id: self.conversation_id,
        }
    }

    pub(super) fn handle_leaf_post_token_count(&mut self, response: SweepResponse) -> SweepCommand {
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
                tokens_before: self.pass.tokens_before,
                tokens_after,
                created_summary_id: self.created_summary_id.clone().unwrap_or_default(),
            },
        }
    }

    pub(super) fn handle_leaf_persist_event(&mut self, _response: SweepResponse) -> SweepCommand {
        // Re-fetch context items after mutation
        self.phase = Phase::FetchingItems;
        SweepCommand::FetchContextItems {
            conversation_id: self.conversation_id,
        }
    }
}
