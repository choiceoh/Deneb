use std::collections::HashMap;
use std::path::PathBuf;

use clap::Args;

use crate::config;
use crate::errors::CliError;
use crate::terminal::{is_json_mode, Palette};

#[derive(Args, Debug)]
pub struct SessionsArgs {
    /// Output JSON.
    #[arg(long)]
    pub json: bool,

    /// Agent ID (default: "default").
    #[arg(long, default_value = "default")]
    pub agent: String,

    /// Only show sessions updated within the past N minutes.
    #[arg(long)]
    pub active: Option<u64>,
}

/// A session entry from sessions.json.
#[derive(Debug, Clone, serde::Deserialize, serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SessionEntry {
    pub session_id: String,
    pub updated_at: u64,
    #[serde(default)]
    pub session_file: Option<String>,
    #[serde(default)]
    pub channel: Option<String>,
    #[serde(default)]
    pub model: Option<String>,
    #[serde(default)]
    pub total_tokens: Option<u64>,
    #[serde(default)]
    pub context_tokens: Option<u64>,
    #[serde(default)]
    pub kind: Option<String>,
    #[serde(default)]
    pub started_at: Option<u64>,
    #[serde(default)]
    pub ended_at: Option<u64>,
}

pub async fn run(args: &SessionsArgs) -> Result<(), CliError> {
    let json_mode = is_json_mode(args.json);
    let state_dir = config::resolve_state_dir();
    let store_path = resolve_session_store_path(&state_dir, &args.agent);

    if !store_path.exists() {
        if json_mode {
            println!(
                "{}",
                serde_json::to_string_pretty(&serde_json::json!({
                    "path": store_path.to_string_lossy(),
                    "count": 0,
                    "sessions": []
                }))?
            );
        } else {
            let muted = Palette::muted();
            println!(
                "{}",
                muted.apply_to(format!("No sessions found at {}", store_path.display()))
            );
        }
        return Ok(());
    }

    let raw = std::fs::read_to_string(&store_path)?;
    let store: HashMap<String, SessionEntry> = serde_json::from_str(&raw).map_err(|e| {
        CliError::Config(format!(
            "failed to parse session store at {}: {e}",
            store_path.display()
        ))
    })?;

    let now_ms = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64;

    // Filter by active window if specified
    let mut sessions: Vec<(&String, &SessionEntry)> = store.iter().collect();
    if let Some(active_mins) = args.active {
        let cutoff = now_ms.saturating_sub(active_mins * 60 * 1000);
        sessions.retain(|(_, entry)| entry.updated_at >= cutoff);
    }

    // Sort by updatedAt descending (most recent first)
    sessions.sort_by(|a, b| b.1.updated_at.cmp(&a.1.updated_at));

    if json_mode {
        let json_sessions: Vec<serde_json::Value> = sessions
            .iter()
            .map(|(key, entry)| {
                let mut v = serde_json::to_value(entry).unwrap_or_default();
                if let Some(obj) = v.as_object_mut() {
                    obj.insert("key".to_string(), serde_json::json!(key));
                    obj.insert(
                        "ageMs".to_string(),
                        serde_json::json!(now_ms - entry.updated_at),
                    );
                }
                v
            })
            .collect();

        let output = serde_json::json!({
            "path": store_path.to_string_lossy(),
            "agent": args.agent,
            "count": sessions.len(),
            "sessions": json_sessions,
        });
        println!("{}", serde_json::to_string_pretty(&output)?);
        return Ok(());
    }

    // Text output
    let bold = Palette::bold();
    let muted = Palette::muted();
    println!(
        "{} ({} sessions)",
        bold.apply_to(format!("Sessions [{}]", args.agent)),
        sessions.len()
    );

    if sessions.is_empty() {
        println!("  {}", muted.apply_to("No sessions found."));
        return Ok(());
    }

    let mut table = crate::terminal::table::styled_table();
    table.set_header(vec!["Key", "Kind", "Age", "Model", "Tokens"]);

    for (key, entry) in &sessions {
        let age = format_age(now_ms - entry.updated_at);
        let kind = entry.kind.as_deref().unwrap_or("-");
        let model = entry
            .model
            .as_deref()
            .map(truncate_model)
            .unwrap_or("-".to_string());
        let tokens = entry
            .total_tokens
            .map(|t| format_tokens(t))
            .unwrap_or_else(|| "-".to_string());

        let display_key = if key.len() > 30 {
            format!("{}...", &key[..27])
        } else {
            key.to_string()
        };

        table.add_row(vec![&display_key, kind, &age, &model, &tokens]);
    }

    println!("{table}");
    println!(
        "  {}",
        muted.apply_to(format!("Store: {}", store_path.display()))
    );

    Ok(())
}

fn resolve_session_store_path(state_dir: &std::path::Path, agent_id: &str) -> PathBuf {
    state_dir
        .join("agents")
        .join(agent_id)
        .join("sessions")
        .join("sessions.json")
}

fn format_age(ms: u64) -> String {
    let secs = ms / 1000;
    if secs < 60 {
        format!("{secs}s")
    } else if secs < 3600 {
        format!("{}m", secs / 60)
    } else if secs < 86400 {
        format!("{}h", secs / 3600)
    } else {
        format!("{}d", secs / 86400)
    }
}

fn format_tokens(tokens: u64) -> String {
    if tokens >= 1_000_000 {
        format!("{:.1}M", tokens as f64 / 1_000_000.0)
    } else if tokens >= 1_000 {
        format!("{:.1}K", tokens as f64 / 1_000.0)
    } else {
        format!("{tokens}")
    }
}

fn truncate_model(model: &str) -> String {
    if model.len() > 20 {
        format!("{}...", &model[..17])
    } else {
        model.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn format_age_values() {
        assert_eq!(format_age(30_000), "30s");
        assert_eq!(format_age(120_000), "2m");
        assert_eq!(format_age(7_200_000), "2h");
        assert_eq!(format_age(172_800_000), "2d");
    }

    #[test]
    fn format_tokens_values() {
        assert_eq!(format_tokens(500), "500");
        assert_eq!(format_tokens(1_500), "1.5K");
        assert_eq!(format_tokens(2_500_000), "2.5M");
    }
}
