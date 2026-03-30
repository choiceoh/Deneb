//! Grep engine — delegates search to host, returns results.
//!
//! Simple 2-step state machine:
//! ```text
//!  start() → Grep{query, mode, scope, ...}
//!  [Start] ──► host executes Grep ──► step(GrepResults) ──► GrepDone
//! ```

use super::types::*;

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
