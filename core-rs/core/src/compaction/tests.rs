use super::*;

#[test]
fn test_estimate_tokens() {
    // chars / 2, min 1 — calibrated for Korean BPE (~2 chars/token).
    assert_eq!(estimate_tokens(""), 1); // min clamp
    assert_eq!(estimate_tokens("a"), 1); // 1/2=0 → min 1
    assert_eq!(estimate_tokens("ab"), 1);
    assert_eq!(estimate_tokens("abc"), 1);
    assert_eq!(estimate_tokens("abcd"), 2);
    assert_eq!(estimate_tokens("abcde"), 2);
    assert_eq!(estimate_tokens("abcdefgh"), 4);
    assert_eq!(estimate_tokens("abcdefghi"), 4);
    // Korean: 5 chars → 2 tokens (accurate for BPE)
    assert_eq!(estimate_tokens("안녕하세요"), 2);
}

#[test]
fn test_evaluate_below_threshold() {
    let config = CompactionConfig::default();
    let decision = evaluate(&config, 500, 0, 1000);
    assert!(!decision.should_compact);
    assert_eq!(decision.reason, CompactionReason::None);
    assert_eq!(decision.current_tokens, 500);
    assert_eq!(decision.threshold, 800); // 0.80 * 1000
}

#[test]
fn test_evaluate_above_threshold() {
    let config = CompactionConfig::default();
    let decision = evaluate(&config, 810, 0, 1000); // above 0.80 threshold
    assert!(decision.should_compact);
    assert_eq!(decision.reason, CompactionReason::Threshold);
}

#[test]
fn test_evaluate_uses_max_of_stored_and_live() {
    let config = CompactionConfig::default();
    let decision = evaluate(&config, 100, 810, 1000); // live > 0.80 threshold
    assert!(decision.should_compact);
    assert_eq!(decision.current_tokens, 810);
}

#[test]
fn test_resolve_fresh_tail_ordinal_empty() {
    assert_eq!(resolve_fresh_tail_ordinal(&[], 8), u64::MAX);
}

#[test]
fn test_resolve_fresh_tail_ordinal_protects_tail() {
    let items = (0..10)
        .map(|i| ContextItem {
            conversation_id: 1,
            ordinal: i,
            item_type: ContextItemType::Message,
            message_id: Some(i),
            summary_id: None,
            created_at: 1000 + i as i64,
        })
        .collect::<Vec<_>>();
    // With 3 fresh tail, ordinal of item at index 7 (ordinal=7) should be protected
    assert_eq!(resolve_fresh_tail_ordinal(&items, 3), 7);
    // With 10, all protected
    assert_eq!(resolve_fresh_tail_ordinal(&items, 10), 0);
    // With 0, none protected
    assert_eq!(resolve_fresh_tail_ordinal(&items, 0), u64::MAX);
}

#[test]
fn test_select_leaf_chunk_basic() {
    let items: Vec<ContextItem> = (0..5)
        .map(|i| ContextItem {
            conversation_id: 1,
            ordinal: i,
            item_type: ContextItemType::Message,
            message_id: Some(i),
            summary_id: None,
            created_at: 1000 + i as i64,
        })
        .collect();
    let mut messages = FxHashMap::default();
    for i in 0..5u64 {
        messages.insert(
            i,
            MessageRecord {
                message_id: i,
                conversation_id: 1,
                seq: i,
                role: "user".to_string(),
                content: "x".repeat(400), // 100 tokens each
                token_count: 100,
                created_at: 1000 + i as i64,
            },
        );
    }

    // With limit of 250, should select 2 messages (200 tokens < 250, adding 3rd = 300 > 250)
    let (ordinals, message_ids, tokens) = select_leaf_chunk(&items, &messages, u64::MAX, 250);
    assert_eq!(ordinals.len(), 2);
    assert_eq!(message_ids.len(), 2);
    assert_eq!(tokens, 200);
}

#[test]
fn test_select_leaf_chunk_respects_fresh_tail() {
    let items: Vec<ContextItem> = (0..5)
        .map(|i| ContextItem {
            conversation_id: 1,
            ordinal: i,
            item_type: ContextItemType::Message,
            message_id: Some(i),
            summary_id: None,
            created_at: 1000 + i as i64,
        })
        .collect();
    let mut messages = FxHashMap::default();
    for i in 0..5u64 {
        messages.insert(
            i,
            MessageRecord {
                message_id: i,
                conversation_id: 1,
                seq: i,
                role: "user".to_string(),
                content: "x".repeat(40),
                token_count: 10,
                created_at: 1000 + i as i64,
            },
        );
    }

    // Fresh tail at ordinal 3 means only 0,1,2 are compactable
    let (ordinals, _message_ids, _) = select_leaf_chunk(&items, &messages, 3, 1000);
    assert_eq!(ordinals.len(), 3);
    assert!(ordinals.iter().all(|&o| o < 3));
}

#[test]
fn test_select_condensed_chunk_basic() {
    let items: Vec<ContextItem> = (0..4)
        .map(|i| ContextItem {
            conversation_id: 1,
            ordinal: i,
            item_type: ContextItemType::Summary,
            message_id: None,
            summary_id: Some(format!("sum_{}", i)),
            created_at: 1000 + i as i64,
        })
        .collect();
    let mut summaries = FxHashMap::default();
    for i in 0..4u64 {
        summaries.insert(
            format!("sum_{}", i),
            SummaryRecord {
                summary_id: format!("sum_{}", i),
                conversation_id: 1,
                kind: SummaryKind::Leaf,
                depth: 0,
                content: "summary content".to_string(),
                token_count: 100,
                file_ids: vec![],
                earliest_at: Some(1000 + i as i64),
                latest_at: Some(2000 + i as i64),
                descendant_count: 0,
                descendant_token_count: 0,
                source_message_token_count: 500,
                created_at: 1000 + i as i64,
            },
        );
    }

    let (ordinals, summary_ids, tokens) =
        select_condensed_chunk(&items, &summaries, 0, u64::MAX, 350);
    assert_eq!(ordinals.len(), 3); // 300 tokens, adding 4th = 400 > 350
    assert_eq!(summary_ids.len(), 3);
    assert_eq!(tokens, 300);
}

#[test]
fn test_deterministic_fallback() {
    let result = deterministic_fallback("hello world", 3);
    assert!(result.contains("hello world"));
    assert!(result.contains("[Truncated from 3 tokens]"));
}

#[test]
fn test_deterministic_fallback_truncation() {
    let long = "a".repeat(4000);
    let result = deterministic_fallback(&long, 1000);
    assert!(result.len() < 4000);
    assert!(result.contains("[Truncated from 1000 tokens]"));
}

#[test]
fn test_dedupe_ordered_ids() {
    let ids = vec!["a", "b", "a", "c", "b"];
    let result = dedupe_ordered_ids(&ids);
    assert_eq!(result, vec!["a", "b", "c"]);
}

#[test]
fn test_compute_descendant_counts() {
    let summaries = vec![
        SummaryRecord {
            summary_id: "a".into(),
            conversation_id: 1,
            kind: SummaryKind::Leaf,
            depth: 0,
            content: "".into(),
            token_count: 100,
            file_ids: vec![],
            earliest_at: None,
            latest_at: None,
            descendant_count: 2,
            descendant_token_count: 50,
            source_message_token_count: 500,
            created_at: 0,
        },
        SummaryRecord {
            summary_id: "b".into(),
            conversation_id: 1,
            kind: SummaryKind::Leaf,
            depth: 0,
            content: "".into(),
            token_count: 200,
            file_ids: vec![],
            earliest_at: None,
            latest_at: None,
            descendant_count: 3,
            descendant_token_count: 80,
            source_message_token_count: 600,
            created_at: 0,
        },
    ];

    let refs: Vec<&SummaryRecord> = summaries.iter().collect();
    let (dc, dtc, smt) = compute_descendant_counts(&refs);
    // descendant_count = (2+1) + (3+1) = 7
    assert_eq!(dc, 7);
    // descendant_token_count = (100+50) + (200+80) = 430
    assert_eq!(dtc, 430);
    // source_message_token_count = 500 + 600 = 1100
    assert_eq!(smt, 1100);
}

#[test]
fn test_generate_summary_id() {
    let id = generate_summary_id("hello world", 1234567890);
    assert!(id.starts_with("sum_"));
    assert_eq!(id.len(), 4 + 16); // "sum_" + 16 hex chars
}

#[test]
fn test_build_leaf_source_text() {
    let messages = vec![MessageRecord {
        message_id: 1,
        conversation_id: 1,
        seq: 1,
        role: "user".into(),
        content: "Hello world".into(),
        token_count: 3,
        created_at: 1711324800000, // 2024-03-25 00:00:00 UTC
    }];
    let text = build_leaf_source_text(&messages, "UTC");
    assert!(text.contains("Hello world"));
    assert!(text.contains("2024"));
    assert!(text.contains("| user]"));
}

#[test]
fn test_build_leaf_source_text_multi_role() {
    let messages = vec![
        MessageRecord {
            message_id: 1,
            conversation_id: 1,
            seq: 1,
            role: "user".into(),
            content: "Fix the bug".into(),
            token_count: 3,
            created_at: 1711324800000,
        },
        MessageRecord {
            message_id: 2,
            conversation_id: 1,
            seq: 2,
            role: "assistant".into(),
            content: "[Tools used: read_file x1, edit x1]\n\nFixed the null check.".into(),
            token_count: 10,
            created_at: 1711324860000,
        },
    ];
    let text = build_leaf_source_text(&messages, "UTC");
    assert!(text.contains("| user]"));
    assert!(text.contains("| assistant]"));
    // Verify ordering: user before assistant
    let user_pos = text.find("| user]").expect("user role");
    let asst_pos = text.find("| assistant]").expect("assistant role");
    assert!(user_pos < asst_pos);
}

#[test]
fn test_config_serde_roundtrip() -> Result<(), Box<dyn std::error::Error>> {
    let config = CompactionConfig::default();
    let json = serde_json::to_string(&config)?;
    let parsed: CompactionConfig = serde_json::from_str(&json)?;
    assert_eq!(parsed.context_threshold, 0.80);
    assert_eq!(parsed.fresh_tail_count, 8);
    assert_eq!(parsed.max_rounds, 10);
    Ok(())
}

#[test]
fn test_config_from_partial_json() -> Result<(), Box<dyn std::error::Error>> {
    let json = r#"{"contextThreshold": 0.5}"#;
    let config: CompactionConfig = serde_json::from_str(json)?;
    assert_eq!(config.context_threshold, 0.5);
    assert_eq!(config.fresh_tail_count, 8); // default
    assert_eq!(config.max_rounds, 10); // default
    Ok(())
}

#[test]
fn test_resolve_prior_summary_ids() {
    let items = vec![
        ContextItem {
            conversation_id: 1,
            ordinal: 0,
            item_type: ContextItemType::Summary,
            message_id: None,
            summary_id: Some("s0".into()),
            created_at: 100,
        },
        ContextItem {
            conversation_id: 1,
            ordinal: 1,
            item_type: ContextItemType::Summary,
            message_id: None,
            summary_id: Some("s1".into()),
            created_at: 200,
        },
        ContextItem {
            conversation_id: 1,
            ordinal: 2,
            item_type: ContextItemType::Message,
            message_id: Some(10),
            summary_id: None,
            created_at: 300,
        },
    ];

    let ids = resolve_prior_summary_ids(&items, 2, 2);
    assert_eq!(ids, vec!["s0", "s1"]);

    let ids = resolve_prior_summary_ids(&items, 1, 2);
    assert_eq!(ids, vec!["s0"]);
}
