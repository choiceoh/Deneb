//! Describe engine — fetches summary lineage from the host.
//!
//! Simple 2-step state machine:
//! ```text
//!  start() → FetchLineage{summary_id}
//!  [Start] ──► host fetches lineage ──► step(Lineage) ──► DescribeDone
//! ```

use super::types::*;

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
