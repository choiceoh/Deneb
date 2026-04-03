use super::*;

fn default_config() -> CompactionConfig {
    CompactionConfig::default()
}

#[test]
fn test_sweep_below_threshold_returns_done() -> Result<(), Box<dyn std::error::Error>> {
    let mut engine = SweepEngine::new(default_config(), 1, 1000, false, false, 1000);
    let cmd = engine.start();
    assert!(matches!(cmd, SweepCommand::FetchTokenCount { .. }));

    // Report tokens below threshold (800)
    let cmd = engine.step(SweepResponse::TokenCount { count: 500 });
    match cmd {
        SweepCommand::Done { result } => {
            assert!(!result.action_taken);
            assert_eq!(result.tokens_before, 500);
        }
        _ => return Err(format!("Expected Done, got {:?}", cmd).into()),
    }
    Ok(())
}

#[test]
fn test_sweep_empty_items_returns_done() -> Result<(), Box<dyn std::error::Error>> {
    let mut engine = SweepEngine::new(default_config(), 1, 1000, false, false, 1000);
    let _ = engine.start();
    let cmd = engine.step(SweepResponse::TokenCount { count: 810 }); // above 0.80 threshold
    assert!(matches!(cmd, SweepCommand::FetchContextItems { .. }));

    let cmd = engine.step(SweepResponse::ContextItems { items: vec![] });
    match cmd {
        SweepCommand::Done { result } => {
            assert!(!result.action_taken);
        }
        _ => return Err(format!("Expected Done, got {:?}", cmd).into()),
    }
    Ok(())
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
fn test_sweep_command_serde_roundtrip() -> Result<(), Box<dyn std::error::Error>> {
    let cmd = SweepCommand::FetchMessages {
        message_ids: vec![1, 2, 3],
    };
    let json = serde_json::to_string(&cmd)?;
    let parsed: SweepCommand = serde_json::from_str(&json)?;
    match parsed {
        SweepCommand::FetchMessages { message_ids } => {
            assert_eq!(message_ids, vec![1, 2, 3]);
        }
        _ => return Err("Wrong variant".into()),
    }
    Ok(())
}

#[test]
fn test_sweep_response_serde_roundtrip() -> Result<(), Box<dyn std::error::Error>> {
    let resp = SweepResponse::TokenCount { count: 42 };
    let json = serde_json::to_string(&resp)?;
    let parsed: SweepResponse = serde_json::from_str(&json)?;
    match parsed {
        SweepResponse::TokenCount { count } => assert_eq!(count, 42),
        _ => return Err("Wrong variant".into()),
    }
    Ok(())
}
