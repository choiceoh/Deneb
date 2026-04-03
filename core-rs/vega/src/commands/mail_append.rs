use rusqlite::params;
use serde_json::{json, Value};
use std::fs;
use std::path::PathBuf;

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// Mail -> .md auto-insertion.
/// Accepts mail data as JSON with subject, sender, date, body, project fields.
/// Appends the mail content to the project's .md file under "## 메일" section.
pub fn cmd_mail_append(args: &Value, config: &VegaConfig) -> CommandResult {
    let subject = args
        .get("subject")
        .and_then(|v| v.as_str())
        .unwrap_or("(제목 없음)");
    let sender = args
        .get("sender")
        .and_then(|v| v.as_str())
        .unwrap_or("(발신자 없음)");
    let date = args
        .get("date")
        .and_then(|v| v.as_str())
        .unwrap_or("(날짜 없음)");
    let Some(body) = args.get("body").and_then(|v| v.as_str()) else {
        return CommandResult::err("mail-append", "body 파라미터가 필요합니다");
    };
    let Some(project_query) = args.get("project").and_then(|v| v.as_str()) else {
        return CommandResult::err("mail-append", "project 파라미터가 필요합니다");
    };

    // Resolve project name to .md file
    let md_path = resolve_project_md(config, project_query);

    if !md_path.exists() {
        return CommandResult::err(
            "mail-append",
            &format!("프로젝트 파일을 찾을 수 없습니다: {}", md_path.display()),
        );
    }

    // Read existing content
    let content = match fs::read_to_string(&md_path) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("mail-append", &format!("파일 읽기 실패: {e}")),
    };

    // Format mail entry
    let mail_entry = format!("\n### {date} - {subject}\n- **발신:** {sender}\n\n{body}\n",);

    // Insert into "## 메일" section or append at the end
    let new_content = insert_into_section(&content, "메일", &mail_entry);

    // Write back
    if let Err(e) = fs::write(&md_path, &new_content) {
        return CommandResult::err("mail-append", &format!("파일 쓰기 실패: {e}"));
    }

    // Also log to comm_log in DB
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("mail-append", &e),
    };

    let project_id: Option<i64> = conn
        .query_row(
            "SELECT id FROM projects WHERE name = ?1 OR name LIKE ?2",
            params![project_query, format!("%{project_query}%")],
            |row| row.get(0),
        )
        .ok();

    if let Some(pid) = project_id {
        let _ = conn.execute(
            "INSERT INTO comm_log (project_id, date, channel, sender, summary)
             VALUES (?1, ?2, 'email', ?3, ?4)",
            params![pid, date, sender, subject],
        );
    }

    CommandResult::ok(
        "mail-append",
        json!({
            "project": project_query,
            "file": md_path.display().to_string(),
            "subject": subject,
            "sender": sender,
            "date": date,
        }),
    )
}

/// Resolve a project query to its .md file path.
fn resolve_project_md(config: &VegaConfig, query: &str) -> PathBuf {
    let md_dir = &config.md_dir;

    // Try exact filename first
    let exact = md_dir.join(format!("{query}.md"));
    if exact.exists() {
        return exact;
    }

    // Try case-insensitive match
    if let Ok(entries) = fs::read_dir(md_dir) {
        for entry in entries.filter_map(std::result::Result::ok) {
            let name = entry.file_name();
            let name_str = name.to_string_lossy();
            if name_str.ends_with(".md") {
                let stem = name_str.trim_end_matches(".md");
                if stem.eq_ignore_ascii_case(query) {
                    return entry.path();
                }
            }
        }
    }

    // Fallback to exact path (will fail existence check upstream)
    exact
}

/// Insert text into a named H2 section. If the section doesn't exist, create it at the end.
fn insert_into_section(content: &str, section_name: &str, text: &str) -> String {
    let section_header = format!("## {section_name}");
    let lines: Vec<&str> = content.lines().collect();

    // Find the section
    let mut section_start = None;
    let mut section_end = None;

    for (i, line) in lines.iter().enumerate() {
        if line.trim() == section_header || line.starts_with(&format!("{section_header} ")) {
            section_start = Some(i);
        } else if section_start.is_some() && section_end.is_none() && line.starts_with("## ") {
            section_end = Some(i);
        }
    }

    if let Some(_start) = section_start {
        // Insert before the next section or at end
        let insert_pos = section_end.unwrap_or(lines.len());
        let mut result = String::new();
        for (i, line) in lines.iter().enumerate() {
            if i == insert_pos {
                result.push_str(text);
                result.push('\n');
            }
            result.push_str(line);
            result.push('\n');
        }
        if section_end.is_none() {
            // Append at the very end
            result.push_str(text);
            result.push('\n');
        }
        result
    } else {
        // Section doesn't exist; create it at the end
        let mut result = content.to_string();
        if !result.ends_with('\n') {
            result.push('\n');
        }
        result.push('\n');
        result.push_str(&section_header);
        result.push('\n');
        result.push_str(text);
        result.push('\n');
        result
    }
}

pub struct MailAppendHandler;

impl super::CommandHandler for MailAppendHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_mail_append(args, config)
    }
}
