//! Mail → Markdown conversion for Vega.
//!
//! Port of Python vega/mail/converter.py and vega/mail_to_md.py.
//! Processes incoming email data and appends to project .md files.

use std::fs;
use std::path::{Path, PathBuf};

use chrono::Local;
use regex::Regex;
use rusqlite::{params, Connection};
use serde_json::{json, Value};

use crate::config::VegaConfig;

/// Process a single mail entry and append to the matching project .md file.
pub fn process_mail(mail_data: &Value, config: &VegaConfig, dry_run: bool) -> Value {
    let subject = mail_data
        .get("subject")
        .and_then(|v| v.as_str())
        .unwrap_or("");
    let sender = mail_data
        .get("sender")
        .and_then(|v| v.as_str())
        .unwrap_or("");
    let date = mail_data.get("date").and_then(|v| v.as_str()).unwrap_or("");
    let body = mail_data.get("body").and_then(|v| v.as_str()).unwrap_or("");
    let summary = mail_data
        .get("summary")
        .and_then(|v| v.as_str())
        .unwrap_or("");
    let project_hint = mail_data
        .get("project")
        .and_then(|v| v.as_str())
        .unwrap_or("");

    if subject.is_empty() && sender.is_empty() {
        return json!({"error": "subject 또는 sender가 필요합니다"});
    }

    let date_str = if date.is_empty() {
        Local::now().format("%Y-%m-%d").to_string()
    } else {
        date.to_string()
    };

    // Find matching project
    let project_id = find_project_for_mail(config, project_hint, subject, sender);

    let (pid, pname, md_path) = match project_id {
        Some((id, name, path)) => (id, name, path),
        None => {
            return json!({
                "error": "매칭되는 프로젝트를 찾을 수 없습니다",
                "subject": subject,
                "sender": sender,
            });
        }
    };

    // Build comm_log entry
    let summary_text = if summary.is_empty() {
        truncate_body(body, 200)
    } else {
        summary.to_string()
    };

    // Insert into comm_log
    if !dry_run {
        if let Ok(conn) = Connection::open(&config.db_path) {
            let _ = conn.execute(
                "INSERT INTO comm_log (project_id, log_date, sender, subject, summary) VALUES (?1, ?2, ?3, ?4, ?5)",
                params![pid, date_str, sender, subject, summary_text],
            );
        }

        // Append to .md file
        if let Some(ref path) = md_path {
            let _ = append_mail_to_md(path, &date_str, sender, subject, &summary_text);
        }
    }

    json!({
        "project_id": pid,
        "project_name": pname,
        "appended": !dry_run,
        "date": date_str,
        "sender": sender,
        "subject": subject,
    })
}

/// Process a batch of mail entries.
pub fn process_mail_batch(mails: &[Value], config: &VegaConfig, dry_run: bool) -> Value {
    let mut results = Vec::new();
    let mut success_count = 0;
    let mut error_count = 0;

    for mail in mails {
        let result = process_mail(mail, config, dry_run);
        if result.get("error").is_some() {
            error_count += 1;
        } else {
            success_count += 1;
        }
        results.push(result);
    }

    json!({
        "total": mails.len(),
        "success": success_count,
        "errors": error_count,
        "results": results,
    })
}

/// Find a project matching the mail content.
fn find_project_for_mail(
    config: &VegaConfig,
    project_hint: &str,
    subject: &str,
    sender: &str,
) -> Option<(i64, String, Option<PathBuf>)> {
    let conn = Connection::open(&config.db_path).ok()?;

    // Try explicit project hint first
    if !project_hint.is_empty() {
        // Try as ID
        if let Ok(id) = project_hint.parse::<i64>() {
            if let Ok(row) = conn.query_row(
                "SELECT id, name, source_file FROM projects WHERE id=?1",
                params![id],
                |r| {
                    Ok((
                        r.get::<_, i64>(0)?,
                        r.get::<_, Option<String>>(1)?,
                        r.get::<_, Option<String>>(2)?,
                    ))
                },
            ) {
                let md_path = row.2.map(|sf| resolve_md_path(config, &sf));
                return Some((row.0, row.1.unwrap_or_default(), md_path));
            }
        }

        // Try as name
        if let Ok(row) = conn.query_row(
            "SELECT id, name, source_file FROM projects WHERE name LIKE ?1 LIMIT 1",
            params![format!("%{}%", project_hint)],
            |r| {
                Ok((
                    r.get::<_, i64>(0)?,
                    r.get::<_, Option<String>>(1)?,
                    r.get::<_, Option<String>>(2)?,
                ))
            },
        ) {
            let md_path = row.2.map(|sf| resolve_md_path(config, &sf));
            return Some((row.0, row.1.unwrap_or_default(), md_path));
        }
    }

    // Try matching by subject keywords against project names
    let search_text = format!("{} {}", subject, sender);
    let rows: Vec<(i64, String, Option<String>)> = conn
        .prepare("SELECT id, name, source_file FROM projects")
        .ok()?
        .query_map([], |r| {
            Ok((
                r.get::<_, i64>(0)?,
                r.get::<_, Option<String>>(1)?,
                r.get::<_, Option<String>>(2)?,
            ))
        })
        .ok()?
        .filter_map(|r| r.ok())
        .map(|(id, name, sf)| (id, name.unwrap_or_default(), sf))
        .collect();

    let search_lower = search_text.to_lowercase();
    let mut best: Option<(i64, String, Option<String>, usize)> = None;

    for (id, name, sf) in &rows {
        if name.is_empty() || name.len() < 2 {
            continue;
        }
        let name_lower = name.to_lowercase();
        let mut score = 0usize;

        if search_lower.contains(&name_lower) {
            score += 20;
        }

        // Token matching
        let tokens: Vec<&str> = name.split_whitespace().filter(|t| t.len() >= 2).collect();
        for token in &tokens {
            if search_lower.contains(&token.to_lowercase()) {
                score += 5;
            }
        }

        if score > 0 {
            if best.is_none() || score > best.as_ref().unwrap().3 {
                best = Some((*id, name.clone(), sf.clone(), score));
            }
        }
    }

    best.map(|(id, name, sf, _)| {
        let md_path = sf.map(|s| resolve_md_path(config, &s));
        (id, name, md_path)
    })
}

/// Resolve .md file path from source_file field.
fn resolve_md_path(config: &VegaConfig, source_file: &str) -> PathBuf {
    let clean = source_file
        .trim_start_matches("memory:")
        .trim_start_matches("file:");
    if Path::new(clean).is_absolute() {
        PathBuf::from(clean)
    } else {
        config.md_dir.join(clean)
    }
}

/// Append mail entry to a .md file under "## 커뮤니케이션" section.
fn append_mail_to_md(
    md_path: &Path,
    date: &str,
    sender: &str,
    subject: &str,
    summary: &str,
) -> Result<(), String> {
    let content = fs::read_to_string(md_path).unwrap_or_default();

    let entry = format!("- [{}] {}: {} — {}\n", date, sender, subject, summary);

    // Find "## 커뮤니케이션" or "## 이력" section
    let comm_re = Regex::new(r"(?m)^##\s+커뮤니케이션").unwrap();
    let history_re = Regex::new(r"(?m)^##\s+이력").unwrap();

    let new_content = if let Some(m) = comm_re.find(&content) {
        // Insert after the heading line
        let pos = content[m.end()..]
            .find('\n')
            .map(|p| m.end() + p + 1)
            .unwrap_or(m.end());
        format!("{}{}{}", &content[..pos], entry, &content[pos..])
    } else if let Some(m) = history_re.find(&content) {
        // Insert before 이력 section
        format!(
            "{}## 커뮤니케이션\n{}\n{}",
            &content[..m.start()],
            entry,
            &content[m.start()..]
        )
    } else {
        // Append at end
        format!("{}\n## 커뮤니케이션\n{}", content.trim_end(), entry)
    };

    fs::write(md_path, new_content).map_err(|e| format!("파일 쓰기 실패: {}", e))
}

/// Truncate body text for summary.
fn truncate_body(body: &str, max_chars: usize) -> String {
    let trimmed = body.trim();
    if trimmed.chars().count() <= max_chars {
        trimmed.to_string()
    } else {
        let truncated: String = trimmed.chars().take(max_chars).collect();
        format!("{}...", truncated)
    }
}
