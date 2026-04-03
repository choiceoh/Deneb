use rusqlite::params;
use serde_json::{json, Value};
use std::fs;

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// Quick project template creation.
/// Creates a new .md file from a template with name, client, person fields,
/// and registers the project in the DB.
///
/// Args:
///   - name: project name (required)
///   - client: client/company name (optional)
///   - person: contact person (optional)
///   - status: initial status (default "신규")
///   - priority: priority level (optional, default "보통")
///   - template: template type (default "default")
pub fn cmd_template(args: &Value, config: &VegaConfig) -> CommandResult {
    let Some(name) = args.get("name").and_then(|v| v.as_str()) else {
        return CommandResult::err("template", "name 파라미터가 필요합니다");
    };
    let client = args.get("client").and_then(|v| v.as_str()).unwrap_or("");
    let person = args.get("person").and_then(|v| v.as_str()).unwrap_or("");
    let status = args
        .get("status")
        .and_then(|v| v.as_str())
        .unwrap_or("신규");
    let priority = args
        .get("priority")
        .and_then(|v| v.as_str())
        .unwrap_or("보통");
    let template_type = args
        .get("template")
        .and_then(|v| v.as_str())
        .unwrap_or("default");

    let md_dir = &config.md_dir;

    // Ensure md_dir exists
    if !md_dir.exists() {
        if let Err(e) = fs::create_dir_all(md_dir) {
            return CommandResult::err("template", &format!("디렉토리 생성 실패: {e}"));
        }
    }

    let md_path = md_dir.join(format!("{name}.md"));

    // Check if file already exists
    if md_path.exists() {
        return CommandResult::err(
            "template",
            &format!("프로젝트 파일이 이미 존재합니다: {}", md_path.display()),
        );
    }

    // Generate template content
    let content = match template_type {
        "minimal" => generate_minimal_template(name, client, person, status),
        "detailed" => generate_detailed_template(name, client, person, status, priority),
        _ => generate_default_template(name, client, person, status, priority),
    };

    // Write .md file
    if let Err(e) = fs::write(&md_path, &content) {
        return CommandResult::err("template", &format!("파일 쓰기 실패: {e}"));
    }

    // Register in DB
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("template", &e),
    };

    let db_result = conn.execute(
        "INSERT INTO projects (name, client, person, status, priority)
         VALUES (?1, ?2, ?3, ?4, ?5)",
        params![name, client, person, status, priority],
    );

    let project_id = match db_result {
        Ok(_) => conn.last_insert_rowid(),
        Err(e) => {
            return CommandResult::err("template", &format!("DB 등록 실패 (파일은 생성됨): {e}"))
        }
    };

    CommandResult::ok(
        "template",
        json!({
            "project_id": project_id,
            "name": name,
            "client": client,
            "person": person,
            "status": status,
            "priority": priority,
            "template": template_type,
            "file": md_path.display().to_string(),
        }),
    )
}

fn generate_default_template(
    name: &str,
    client: &str,
    person: &str,
    status: &str,
    priority: &str,
) -> String {
    let date = chrono::Local::now().format("%Y-%m-%d").to_string();
    format!(
        r#"# {name}

- **고객사:** {client}
- **담당자:** {person}
- **상태:** {status}
- **우선순위:** {priority}
- **생성일:** {date}

## 개요

(프로젝트 개요를 작성하세요)

## 다음 예상 액션

- [{date}] 프로젝트 생성

## 이력

- [{date}] 프로젝트 시작

## 메모

## 메일
"#
    )
}

fn generate_minimal_template(name: &str, client: &str, person: &str, status: &str) -> String {
    let date = chrono::Local::now().format("%Y-%m-%d").to_string();
    format!(
        r#"# {name}

- **고객사:** {client}
- **담당자:** {person}
- **상태:** {status}

## 다음 예상 액션

## 이력

- [{date}] 프로젝트 시작
"#
    )
}

fn generate_detailed_template(
    name: &str,
    client: &str,
    person: &str,
    status: &str,
    priority: &str,
) -> String {
    let date = chrono::Local::now().format("%Y-%m-%d").to_string();
    format!(
        r#"# {name}

- **고객사:** {client}
- **담당자:** {person}
- **상태:** {status}
- **우선순위:** {priority}
- **생성일:** {date}
- **마감일:** (미정)
- **예산:** (미정)

## 개요

(프로젝트 개요를 작성하세요)

## 목표

- (목표 1)
- (목표 2)

## 범위

### 포함

- (포함 항목)

### 제외

- (제외 항목)

## 다음 예상 액션

- [{date}] 프로젝트 생성

## 이력

- [{date}] 프로젝트 시작

## 회의록

## 메모

## 메일

## 관련 자료
"#
    )
}

pub struct TemplateHandler;

impl super::CommandHandler for TemplateHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_template(args, config)
    }
}
