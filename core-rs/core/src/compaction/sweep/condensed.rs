//! Condensed pass phase handlers for the sweep engine.
//!
//! Phase 2 of compaction: merge existing summaries into higher-depth summaries.
//! Entered after leaf iterations are exhausted or no more leaf chunks remain.
//! Follows the fetch-depths → find-candidate → fetch-prior → summarize
//! (normal → aggressive → fallback) → persist → post-token-count → persist-event cycle.

use super::super::*;
use super::engine::{Phase, SweepEngine};
use super::types::*;
use std::sync::Arc;

impl SweepEngine {
    pub(super) fn transition_to_condensed_phase(&mut self) -> SweepCommand {
        self.pass.condensed_iter = 0;
        self.phase = Phase::CondensedFetchDepths;
        SweepCommand::FetchDistinctDepths {
            conversation_id: self.conversation_id,
            max_ordinal: self.fresh_tail_ordinal,
        }
    }

    pub(super) fn handle_condensed_fetch_depths(
        &mut self,
        response: SweepResponse,
    ) -> SweepCommand {
        let max_iter = resolve_max_sweep_iterations(&self.config);
        if self.pass.condensed_iter >= max_iter {
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
            if let Some((target_depth, ordinals, summary_ids, _tokens)) =
                find_shallowest_condensation_candidate(
                    &self.context_items,
                    &self.summaries,
                    &depths,
                    self.fresh_tail_ordinal,
                    &self.config,
                    self.hard_trigger,
                )
            {
                self.pass.target_depth = target_depth;
                self.pass.chunk_ordinals = ordinals;
                self.pass.chunk_summary_ids = summary_ids;
                return self.prepare_condensed_summarize();
            }
        }

        self.phase = Phase::Done;
        self.fetch_final_token_count()
    }

    pub(super) fn handle_condensed_fetch_summaries(
        &mut self,
        response: SweepResponse,
    ) -> SweepCommand {
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

        if let Some((target_depth, ordinals, summary_ids, _tokens)) =
            find_shallowest_condensation_candidate(
                &self.context_items,
                &self.summaries,
                &depths,
                self.fresh_tail_ordinal,
                &self.config,
                self.hard_trigger,
            )
        {
            self.pass.target_depth = target_depth;
            self.pass.chunk_ordinals = ordinals;
            self.pass.chunk_summary_ids = summary_ids;
            return self.prepare_condensed_summarize();
        }

        self.phase = Phase::Done;
        self.fetch_final_token_count()
    }

    fn prepare_condensed_summarize(&mut self) -> SweepCommand {
        // Check if we need prior summary context (for depth 0)
        if self.pass.target_depth == 0 {
            let start_ordinal = self.pass.chunk_ordinals.iter().copied().min().unwrap_or(0);
            let prior_ids = resolve_prior_summary_ids_at_depth(
                &self.context_items,
                &self.summaries,
                start_ordinal,
                self.pass.target_depth,
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
                .filter(|s| s.depth == self.pass.target_depth)
                .map(|s| s.content.trim().to_string())
                .filter(|s| !s.is_empty())
                .collect();
            if !context.is_empty() {
                self.pass.previous_summary = Some(Arc::from(context.join("\n\n")));
            } else {
                self.pass.previous_summary = None;
            }
        } else {
            self.pass.previous_summary = None;
        }

        self.emit_condensed_summarize()
    }

    pub(super) fn handle_condensed_fetch_prior_summaries(
        &mut self,
        response: SweepResponse,
    ) -> SweepCommand {
        if let SweepResponse::Summaries { summaries } = response {
            for (id, s) in summaries {
                self.summaries.insert(id, s);
            }
        }

        // Build context
        let start_ordinal = self.pass.chunk_ordinals.iter().copied().min().unwrap_or(0);
        let prior_ids = resolve_prior_summary_ids_at_depth(
            &self.context_items,
            &self.summaries,
            start_ordinal,
            self.pass.target_depth,
            2,
        );
        let context: Vec<String> = prior_ids
            .iter()
            .filter_map(|id| self.summaries.get(id))
            .filter(|s| s.depth == self.pass.target_depth)
            .map(|s| s.content.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect();
        if !context.is_empty() {
            self.pass.previous_summary = Some(Arc::from(context.join("\n\n")));
        }

        self.emit_condensed_summarize()
    }

    fn emit_condensed_summarize(&mut self) -> SweepCommand {
        let recs: Vec<SummaryRecord> = self
            .pass
            .chunk_summary_ids
            .iter()
            .filter_map(|id| self.summaries.get(id).cloned())
            .collect();

        let tz = resolve_timezone(&self.config);
        self.pass.source_text = Arc::from(build_condensed_source_text(&recs, tz));
        self.pass.source_tokens = estimate_tokens(&self.pass.source_text).max(1);

        if self.pass.source_text.trim().is_empty() {
            self.pass.summary_content =
                deterministic_fallback(&self.pass.source_text, self.pass.source_tokens);
            self.level = Some(CompactionLevel::Fallback);
            return self.prepare_condensed_persist();
        }

        self.phase = Phase::CondensedSummarizeNormal;
        SweepCommand::Summarize {
            text: self.pass.source_text.clone(),
            aggressive: false,
            options: Some(SummarizeOptions {
                previous_summary: self.pass.previous_summary.clone(),
                is_condensed: Some(true),
                depth: Some(self.pass.target_depth + 1),
                target_tokens: None,
            }),
        }
    }

    pub(super) fn handle_condensed_summarize_normal(
        &mut self,
        response: SweepResponse,
    ) -> SweepCommand {
        if let SweepResponse::SummaryText { text } = response {
            let summary_tokens = estimate_tokens(&text);
            if summary_tokens < self.pass.source_tokens {
                self.pass.summary_content = text;
                self.level = Some(CompactionLevel::Normal);
                return self.prepare_condensed_persist();
            }

            self.phase = Phase::CondensedSummarizeAggressive;
            return SweepCommand::Summarize {
                text: self.pass.source_text.clone(),
                aggressive: true,
                options: Some(SummarizeOptions {
                    previous_summary: self.pass.previous_summary.clone(),
                    is_condensed: Some(true),
                    depth: Some(self.pass.target_depth + 1),
                    target_tokens: None,
                }),
            };
        }
        self.pass.summary_content =
            deterministic_fallback(&self.pass.source_text, self.pass.source_tokens);
        self.level = Some(CompactionLevel::Fallback);
        self.prepare_condensed_persist()
    }

    pub(super) fn handle_condensed_summarize_aggressive(
        &mut self,
        response: SweepResponse,
    ) -> SweepCommand {
        if let SweepResponse::SummaryText { text } = response {
            let summary_tokens = estimate_tokens(&text);
            if summary_tokens < self.pass.source_tokens {
                self.pass.summary_content = text;
                self.level = Some(CompactionLevel::Aggressive);
                return self.prepare_condensed_persist();
            }
        }
        self.pass.summary_content =
            deterministic_fallback(&self.pass.source_text, self.pass.source_tokens);
        self.level = Some(CompactionLevel::Fallback);
        self.prepare_condensed_persist()
    }

    fn prepare_condensed_persist(&mut self) -> SweepCommand {
        let summary_id = generate_summary_id(&self.pass.summary_content, self.now_ms);
        let token_count = estimate_tokens(&self.pass.summary_content);

        let recs: Vec<&SummaryRecord> = self
            .pass
            .chunk_summary_ids
            .iter()
            .filter_map(|id| self.summaries.get(id))
            .collect();

        let (descendant_count, descendant_token_count, source_message_token_count) =
            compute_descendant_counts(&recs);

        let earliest_at = recs
            .iter()
            .map(|s| s.earliest_at.unwrap_or(s.created_at))
            .min();
        let latest_at = recs
            .iter()
            .map(|s| s.latest_at.unwrap_or(s.created_at))
            .max();

        let start_ordinal = self.pass.chunk_ordinals.iter().copied().min().unwrap_or(0);
        let end_ordinal = self.pass.chunk_ordinals.iter().copied().max().unwrap_or(0);

        self.created_summary_id = Some(summary_id.clone());
        self.pass.tokens_before = self.previous_tokens;

        self.phase = Phase::CondensedPersist;
        SweepCommand::PersistCondensedSummary {
            input: PersistCondensedInput {
                summary_id,
                conversation_id: self.conversation_id,
                depth: self.pass.target_depth + 1,
                content: self.pass.summary_content.clone(),
                token_count,
                file_ids: Vec::new(), // File ID extraction stays in host
                earliest_at,
                latest_at,
                descendant_count,
                descendant_token_count,
                source_message_token_count,
                parent_summary_ids: self.pass.chunk_summary_ids.clone(),
                start_ordinal,
                end_ordinal,
            },
        }
    }

    pub(super) fn handle_condensed_persist(&mut self, _response: SweepResponse) -> SweepCommand {
        self.action_taken = true;
        self.condensed = true;
        self.pass.condensed_iter += 1;

        self.phase = Phase::CondensedPostTokenCount;
        SweepCommand::FetchTokenCount {
            conversation_id: self.conversation_id,
        }
    }

    pub(super) fn handle_condensed_post_token_count(
        &mut self,
        response: SweepResponse,
    ) -> SweepCommand {
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
                tokens_before: self.pass.tokens_before,
                tokens_after,
                created_summary_id: self.created_summary_id.clone().unwrap_or_default(),
            },
        }
    }

    pub(super) fn handle_condensed_persist_event(
        &mut self,
        _response: SweepResponse,
    ) -> SweepCommand {
        // Re-fetch items and continue condensed phase
        self.phase = Phase::FetchingItems;
        SweepCommand::FetchContextItems {
            conversation_id: self.conversation_id,
        }
    }
}
