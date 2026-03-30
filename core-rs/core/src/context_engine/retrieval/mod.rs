//! Retrieval operations — grep, describe, expand via step-based I/O protocol.
//!
//! The retrieval module provides three operations for querying the context DAG:
//! - **grep**: Search messages and/or summaries (regex or full-text)
//! - **describe**: Fetch summary/file lineage metadata
//! - **expand**: Traverse the DAG with token budgeting
//!
//! Each operation uses the same command/response pattern as the assembly and
//! compaction sweep engines: Rust drives the algorithm, the host executes I/O.
//!
//! ## State Machine Diagrams
//!
//! ### GrepEngine (2 steps, trivial)
//!
//! ```text
//!  start() → Grep{query, mode, scope, ...}
//!
//!  [Start] ──► host executes Grep ──► step(GrepResults) ──► GrepDone
//! ```
//!
//! ### DescribeEngine (2 steps, trivial)
//!
//! ```text
//!  start() → FetchLineage{summary_id}
//!
//!  [Start] ──► host fetches lineage ──► step(Lineage) ──► DescribeDone
//! ```
//!
//! ### ExpandEngine (multi-step DAG traversal)
//!
//! See `expand.rs` module docs for the full state diagram.

mod describe;
mod expand;
mod grep;
pub mod types;

pub use describe::DescribeEngine;
pub use expand::ExpandEngine;
pub use grep::GrepEngine;
pub use types::*;

// ── Tests ────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_grep_engine_basic() {
        let engine = GrepEngine::new(
            "test query".to_string(),
            GrepMode::Regex,
            GrepScope::Both,
            None,
            None,
            None,
            Some(10),
        );

        let cmd = engine.start();
        match cmd {
            RetrievalCommand::Grep {
                query,
                mode,
                scope,
                limit,
                ..
            } => {
                assert_eq!(query, "test query");
                assert_eq!(mode, GrepMode::Regex);
                assert_eq!(scope, GrepScope::Both);
                assert_eq!(limit, Some(10));
            }
            _ => panic!("Expected Grep command"),
        }
    }

    #[test]
    fn test_grep_engine_step() {
        let mut engine = GrepEngine::new(
            "test".to_string(),
            GrepMode::FullText,
            GrepScope::Messages,
            None,
            None,
            None,
            None,
        );

        let _ = engine.start();
        let cmd = engine.step(RetrievalResponse::GrepResults {
            matches: vec![GrepMatch {
                source: "message".to_string(),
                id: "msg_1".to_string(),
                snippet: "test snippet".to_string(),
                token_count: 10,
                created_at: 1000,
                rank: Some(1.5),
            }],
            total_matches: 1,
        });

        match cmd {
            RetrievalCommand::GrepDone { result } => {
                assert_eq!(result.matches.len(), 1);
                assert_eq!(result.total_matches, 1);
                assert_eq!(result.matches[0].snippet, "test snippet");
            }
            _ => panic!("Expected GrepDone"),
        }
        assert!(engine.is_done());
    }

    #[test]
    fn test_describe_engine_basic() {
        let engine = DescribeEngine::new("sum_abc".to_string());
        let cmd = engine.start();
        match cmd {
            RetrievalCommand::FetchLineage { summary_id } => {
                assert_eq!(summary_id, "sum_abc");
            }
            _ => panic!("Expected FetchLineage"),
        }
    }

    #[test]
    fn test_describe_engine_step() {
        let mut engine = DescribeEngine::new("sum_abc".to_string());
        let _ = engine.start();

        let cmd = engine.step(RetrievalResponse::Lineage {
            node: Some(LineageNode {
                summary_id: "sum_abc".to_string(),
                kind: "leaf".to_string(),
                depth: 0,
                token_count: 500,
                descendant_count: 0,
                earliest_at: Some(1000),
                latest_at: Some(2000),
            }),
            parents: vec!["sum_parent".to_string()],
            children: vec![],
            message_ids: vec![1, 2, 3],
            subtree: vec![],
        });

        match cmd {
            RetrievalCommand::DescribeDone { result } => {
                assert_eq!(result.id, "sum_abc");
                assert_eq!(result.item_type, "summary");
                assert!(result.node.is_some());
                assert_eq!(result.parents.len(), 1);
                assert_eq!(result.message_ids, vec![1, 2, 3]);
            }
            _ => panic!("Expected DescribeDone"),
        }
        assert!(engine.is_done());
    }

    #[test]
    fn test_describe_engine_file_type() {
        let mut engine = DescribeEngine::new("file_xyz".to_string());
        let _ = engine.start();

        let cmd = engine.step(RetrievalResponse::Lineage {
            node: None,
            parents: vec![],
            children: vec![],
            message_ids: vec![],
            subtree: vec![],
        });

        match cmd {
            RetrievalCommand::DescribeDone { result } => {
                assert_eq!(result.item_type, "file");
            }
            _ => panic!("Expected DescribeDone"),
        }
    }

    #[test]
    fn test_expand_engine_leaf_with_messages() {
        let mut engine = ExpandEngine::new("sum_leaf".to_string(), 1, true, 10_000);
        let cmd = engine.start();
        assert!(matches!(cmd, RetrievalCommand::FetchSummary { .. }));

        // Root is a leaf → should fetch source messages
        let cmd = engine.step(RetrievalResponse::Summary {
            summary_id: "sum_leaf".to_string(),
            kind: "leaf".to_string(),
            depth: 0,
            content: "leaf content".to_string(),
            token_count: 500,
        });
        assert!(matches!(cmd, RetrievalCommand::FetchSourceMessages { .. }));

        // Provide messages
        let cmd = engine.step(RetrievalResponse::SourceMessages {
            messages: vec![
                ExpandMessage {
                    message_id: 1,
                    role: "user".to_string(),
                    content: "hello".to_string(),
                    token_count: 10,
                },
                ExpandMessage {
                    message_id: 2,
                    role: "assistant".to_string(),
                    content: "world".to_string(),
                    token_count: 10,
                },
            ],
        });

        match cmd {
            RetrievalCommand::ExpandDone { result } => {
                assert_eq!(result.messages.len(), 2);
                assert_eq!(result.estimated_tokens, 20);
                assert!(!result.truncated);
            }
            _ => panic!("Expected ExpandDone"),
        }
    }

    #[test]
    fn test_expand_engine_condensed_with_children() {
        let mut engine = ExpandEngine::new("sum_cond".to_string(), 2, false, 10_000);
        let cmd = engine.start();
        assert!(matches!(cmd, RetrievalCommand::FetchSummary { .. }));

        // Root is condensed → should fetch children
        let cmd = engine.step(RetrievalResponse::Summary {
            summary_id: "sum_cond".to_string(),
            kind: "condensed".to_string(),
            depth: 1,
            content: "condensed content".to_string(),
            token_count: 500,
        });
        assert!(matches!(cmd, RetrievalCommand::FetchChildren { .. }));

        // Provide children
        let cmd = engine.step(RetrievalResponse::Children {
            children: vec![
                ExpandChild {
                    summary_id: "sum_c1".to_string(),
                    kind: "leaf".to_string(),
                    content: "child 1".to_string(),
                    token_count: 200,
                },
                ExpandChild {
                    summary_id: "sum_c2".to_string(),
                    kind: "leaf".to_string(),
                    content: "child 2".to_string(),
                    token_count: 300,
                },
            ],
        });

        match cmd {
            RetrievalCommand::ExpandDone { result } => {
                assert_eq!(result.children.len(), 2);
                assert_eq!(result.estimated_tokens, 500);
                assert!(!result.truncated);
            }
            _ => panic!("Expected ExpandDone"),
        }
    }

    #[test]
    fn test_expand_engine_token_cap_truncation() {
        let mut engine = ExpandEngine::new("sum_cond".to_string(), 2, false, 250);
        let _ = engine.start();

        let cmd = engine.step(RetrievalResponse::Summary {
            summary_id: "sum_cond".to_string(),
            kind: "condensed".to_string(),
            depth: 1,
            content: "condensed".to_string(),
            token_count: 100,
        });
        assert!(matches!(cmd, RetrievalCommand::FetchChildren { .. }));

        let cmd = engine.step(RetrievalResponse::Children {
            children: vec![
                ExpandChild {
                    summary_id: "sum_c1".to_string(),
                    kind: "leaf".to_string(),
                    content: "child 1".to_string(),
                    token_count: 200,
                },
                ExpandChild {
                    summary_id: "sum_c2".to_string(),
                    kind: "leaf".to_string(),
                    content: "child 2".to_string(),
                    token_count: 200,
                },
            ],
        });

        match cmd {
            RetrievalCommand::ExpandDone { result } => {
                // First child fits (200 <= 250), second doesn't (200+200=400 > 250)
                assert_eq!(result.children.len(), 1);
                assert!(result.truncated);
                assert_eq!(result.estimated_tokens, 200);
            }
            _ => panic!("Expected ExpandDone"),
        }
    }

    #[test]
    fn test_expand_engine_zero_depth() {
        let mut engine = ExpandEngine::new("sum_cond".to_string(), 0, false, 10_000);
        let _ = engine.start();

        // Condensed but depth=0 → don't expand children
        let cmd = engine.step(RetrievalResponse::Summary {
            summary_id: "sum_cond".to_string(),
            kind: "condensed".to_string(),
            depth: 1,
            content: "condensed".to_string(),
            token_count: 100,
        });

        match cmd {
            RetrievalCommand::ExpandDone { result } => {
                assert_eq!(result.children.len(), 0);
                assert_eq!(result.messages.len(), 0);
            }
            _ => panic!("Expected ExpandDone"),
        }
    }

    #[test]
    fn test_grep_mode_serde() -> Result<(), Box<dyn std::error::Error>> {
        let mode = GrepMode::Regex;
        let json = serde_json::to_string(&mode)?;
        assert_eq!(json, "\"regex\"");

        let mode: GrepMode = serde_json::from_str("\"full_text\"")?;
        assert_eq!(mode, GrepMode::FullText);
        Ok(())
    }

    #[test]
    fn test_grep_scope_serde() -> Result<(), Box<dyn std::error::Error>> {
        let scope = GrepScope::Both;
        let json = serde_json::to_string(&scope)?;
        assert_eq!(json, "\"both\"");
        Ok(())
    }

    #[test]
    fn test_retrieval_command_serde_roundtrip() -> Result<(), Box<dyn std::error::Error>> {
        let cmd = RetrievalCommand::Grep {
            query: "test".to_string(),
            mode: GrepMode::Regex,
            scope: GrepScope::Messages,
            conversation_id: Some(42),
            since_ms: None,
            before_ms: None,
            limit: Some(10),
        };
        let json = serde_json::to_string(&cmd)?;
        let parsed: RetrievalCommand = serde_json::from_str(&json)?;
        match parsed {
            RetrievalCommand::Grep {
                query,
                conversation_id,
                limit,
                ..
            } => {
                assert_eq!(query, "test");
                assert_eq!(conversation_id, Some(42));
                assert_eq!(limit, Some(10));
            }
            _ => panic!("Wrong variant"),
        }
        Ok(())
    }
}
