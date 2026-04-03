use rusqlite::params;
use serde_json::{json, Value};
use std::fs;

use crate::config::VegaConfig;

use super::{find_project_id, open_db, CommandResult};

/// Add action items or history entries to project .md files.
/// Finds project by ID or name, adds text to "다음 예상 액션" or "이력" section.
///
/// Args:
///   - project: project name or ID (required)
///   - text: action/history text to add (required)
///   - section: "action" (default) or "history"
///   - date: optional date string (defaults to today)
pub fn cmd_add_action(args: &Value, config: &VegaConfig) -> CommandResult {
    let Some(project_query) = args.get("project").and_then(|v| v.as_str()) else {
        return CommandResult::err("add-action", "project 파라미터가 필요합니다");
    };
    let Some(text) = args.get("text").and_then(|v| v.as_str()) else {
        return CommandResult::err("add-action", "text 파라미터가 필요합니다");
    };
    let section_type = args
        .get("section")
        .and_then(|v| v.as_str())
        .unwrap_or("action");
    let date = args.get("date").and_then(|v| v.as_str()).map_or_else(
        || chrono::Local::now().format("%Y-%m-%d").to_string(),
        std::string::ToString::to_string,
    );

    // Resolve project
    let project_id = find_project_id(config, project_query);

    // Get project name from DB if found by ID
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("add-action", &e),
    };

    let project_name = if let Some(pid) = project_id {
        conn.query_row(
            "SELECT name FROM projects WHERE id = ?1",
            params![pid],
            |row| row.get::<_, String>(0),
        )
        .unwrap_or_else(|_| project_query.to_string())
    } else {
        project_query.to_string()
    };

    // Find the .md file
    let md_path = config.md_dir.join(format!("{project_name}.md"));
    if !md_path.exists() {
        return CommandResult::err(
            "add-action",
            &format!("프로젝트 파일을 찾을 수 없습니다: {}", md_path.display()),
        );
    }

    let content = match fs::read_to_string(&md_path) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("add-action", &format!("파일 읽기 실패: {e}")),
    };

    // Determine target section name
    let section_name = match section_type {
        "history" | "이력" => "이력",
        _ => "다음 예상 액션",
    };

    // Format the entry
    let entry = format!("- [{date}] {text}");

    // Insert into section
    let new_content = insert_into_md_section(&content, section_name, &entry);

    if let Err(e) = fs::write(&md_path, &new_content) {
        return CommandResult::err("add-action", &format!("파일 쓰기 실패: {e}"));
    }

    // Also update DB if project exists
    if let Some(pid) = project_id {
        let db_section = if section_name == "이력" {
            "history"
        } else {
            "action"
        };
        let _ = conn.execute(
            "INSERT INTO comm_log (project_id, date, channel, sender, summary)
             VALUES (?1, ?2, ?3, 'system', ?4)",
            params![pid, date, db_section, text],
        );
    }

    CommandResult::ok(
        "add-action",
        json!({
            "project": project_name,
            "section": section_name,
            "text": text,
            "date": date,
            "file": md_path.display().to_string(),
        }),
    )
}

/// Insert a line into a named H2 section in markdown content.
/// If the section exists, appends the line at the end of that section.
/// If not, creates the section at the end of the file.
fn insert_into_md_section(content: &str, section_name: &str, line: &str) -> String {
    let section_header = format!("## {section_name}");
    let lines: Vec<&str> = content.lines().collect();

    let mut section_start = None;
    let mut next_section = None;

    for (i, l) in lines.iter().enumerate() {
        let trimmed = l.trim();
        if trimmed == section_header || trimmed.starts_with(&format!("{section_header} ")) {
            section_start = Some(i);
        } else if section_start.is_some() && next_section.is_none() && trimmed.starts_with("## ") {
            next_section = Some(i);
        }
    }

    let mut result = String::new();

    if let Some(_start) = section_start {
        let insert_pos = next_section.unwrap_or(lines.len());

        for (i, l) in lines.iter().enumerate() {
            if i == insert_pos {
                result.push_str(line);
                result.push('\n');
            }
            result.push_str(l);
            result.push('\n');
        }

        // If inserting at the very end
        if next_section.is_none() {
            result.push_str(line);
            result.push('\n');
        }
    } else {
        // Section doesn't exist; create at end
        result.push_str(content);
        if !content.ends_with('\n') {
            result.push('\n');
        }
        result.push('\n');
        result.push_str(&section_header);
        result.push('\n');
        result.push_str(line);
        result.push('\n');
    }

    result
}

pub struct AddActionHandler;

impl super::CommandHandler for AddActionHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_add_action(args, config)
    }
}
