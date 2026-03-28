use regex::Regex;
use rusqlite::params;
use serde_json::{json, Value};
use std::fs;

use crate::config::VegaConfig;

use super::{find_project_id, open_db, CommandResult};

/// Update project status/fields in both .md and DB.
///
/// Args:
///   - project: project name or ID (required)
///   - status: new status value (optional)
///   - client: new client name (optional)
///   - person: new person name (optional)
///   - priority: new priority value (optional)
///   - notes: additional notes (optional)
pub fn cmd_update(args: &Value, config: &VegaConfig) -> CommandResult {
    let project_query = match args.get("project").and_then(|v| v.as_str()) {
        Some(p) => p,
        None => return CommandResult::err("update", "project 파라미터가 필요합니다"),
    };

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("update", &e),
    };

    // Resolve project
    let project_id = match find_project_id(config, project_query) {
        Some(id) => id,
        None => {
            return CommandResult::err(
                "update",
                &format!("프로젝트를 찾을 수 없습니다: {project_query}"),
            )
        }
    };

    // Get current project name
    let project_name: String = match conn.query_row(
        "SELECT name FROM projects WHERE id = ?1",
        params![project_id],
        |row| row.get(0),
    ) {
        Ok(n) => n,
        Err(e) => return CommandResult::err("update", &format!("프로젝트 조회 실패: {e}")),
    };

    let mut updated_fields: Vec<String> = Vec::new();

    // Update DB fields
    if let Some(status) = args.get("status").and_then(|v| v.as_str()) {
        conn.execute(
            "UPDATE projects SET status = ?1 WHERE id = ?2",
            params![status, project_id],
        )
        .ok();
        updated_fields.push(format!("status -> {status}"));
    }

    if let Some(client) = args.get("client").and_then(|v| v.as_str()) {
        conn.execute(
            "UPDATE projects SET client = ?1 WHERE id = ?2",
            params![client, project_id],
        )
        .ok();
        updated_fields.push(format!("client -> {client}"));
    }

    if let Some(person) = args.get("person").and_then(|v| v.as_str()) {
        conn.execute(
            "UPDATE projects SET person = ?1 WHERE id = ?2",
            params![person, project_id],
        )
        .ok();
        updated_fields.push(format!("person -> {person}"));
    }

    if let Some(priority) = args.get("priority").and_then(|v| v.as_str()) {
        conn.execute(
            "UPDATE projects SET priority = ?1 WHERE id = ?2",
            params![priority, project_id],
        )
        .ok();
        updated_fields.push(format!("priority -> {priority}"));
    }

    if updated_fields.is_empty() && args.get("notes").is_none() {
        return CommandResult::err("update", "업데이트할 필드가 지정되지 않았습니다");
    }

    // Update .md file
    let md_path = config.md_dir.join(format!("{project_name}.md"));
    if md_path.exists() {
        let content = match fs::read_to_string(&md_path) {
            Ok(c) => c,
            Err(e) => return CommandResult::err("update", &format!("파일 읽기 실패: {e}")),
        };

        let mut new_content = content.clone();

        // Update YAML-like frontmatter fields
        if let Some(status) = args.get("status").and_then(|v| v.as_str()) {
            new_content = update_md_field(&new_content, "상태", status);
            new_content = update_md_field(&new_content, "status", status);
        }
        if let Some(client) = args.get("client").and_then(|v| v.as_str()) {
            new_content = update_md_field(&new_content, "고객사", client);
            new_content = update_md_field(&new_content, "client", client);
        }
        if let Some(person) = args.get("person").and_then(|v| v.as_str()) {
            new_content = update_md_field(&new_content, "담당자", person);
            new_content = update_md_field(&new_content, "person", person);
        }
        if let Some(priority) = args.get("priority").and_then(|v| v.as_str()) {
            new_content = update_md_field(&new_content, "우선순위", priority);
            new_content = update_md_field(&new_content, "priority", priority);
        }

        // Add notes if provided
        if let Some(notes) = args.get("notes").and_then(|v| v.as_str()) {
            let date = chrono::Local::now().format("%Y-%m-%d").to_string();
            let note_entry = format!("- [{date}] {notes}");
            new_content = append_to_section(&new_content, "메모", &note_entry);
            updated_fields.push("notes 추가됨".to_string());
        }

        if new_content != content {
            if let Err(e) = fs::write(&md_path, &new_content) {
                return CommandResult::err("update", &format!("파일 쓰기 실패: {e}"));
            }
        }
    }

    CommandResult::ok(
        "update",
        json!({
            "project": project_name,
            "project_id": project_id,
            "updated": updated_fields,
            "file": md_path.display().to_string(),
        }),
    )
}

/// Update a key-value field in markdown content (e.g., "- **상태:** 진행중").
fn update_md_field(content: &str, field_name: &str, new_value: &str) -> String {
    // Match patterns like "- **필드명:** 값" or "**필드명:** 값"
    let pattern = format!(r"(?m)^(\s*-?\s*\*\*{field_name}:\*\*\s*)(.*)$");
    if let Ok(re) = Regex::new(&pattern) {
        if re.is_match(content) {
            return re
                .replace(content, format!("${{1}}{new_value}"))
                .to_string();
        }
    }

    // Also try plain "필드명: 값" in frontmatter
    let pattern_plain = format!(r"(?m)^({field_name}:\s*)(.*)$");
    if let Ok(re) = Regex::new(&pattern_plain) {
        if re.is_match(content) {
            return re
                .replace(content, format!("${{1}}{new_value}"))
                .to_string();
        }
    }

    content.to_string()
}

/// Append a line to a named H2 section, creating it if it doesn't exist.
fn append_to_section(content: &str, section_name: &str, line: &str) -> String {
    let section_header = format!("## {section_name}");
    let lines: Vec<&str> = content.lines().collect();

    let mut section_start = None;
    let mut next_section = None;

    for (i, l) in lines.iter().enumerate() {
        let trimmed = l.trim();
        if trimmed == section_header {
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
        if next_section.is_none() {
            result.push_str(line);
            result.push('\n');
        }
    } else {
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

pub struct UpdateHandler;

impl super::CommandHandler for UpdateHandler {
    fn execute(&self, config: &crate::config::VegaConfig, args: &serde_json::Value) -> super::CommandResult {
        cmd_update(args, config)
    }
}
