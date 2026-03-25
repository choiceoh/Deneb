use regex::Regex;
use rusqlite::Connection;
use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// Pipeline analysis: extract monetary amounts from project text,
/// classify project stages, and aggregate pipeline metrics.
pub fn cmd_pipeline(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("pipeline", &e),
    };

    let projects = match get_active_projects(&conn) {
        Ok(p) => p,
        Err(e) => return CommandResult::err("pipeline", &e),
    };
    let mut pipeline_items: Vec<Value> = Vec::new();
    let mut stage_totals: std::collections::HashMap<String, f64> =
        std::collections::HashMap::new();
    let mut stage_counts: std::collections::HashMap<String, i64> =
        std::collections::HashMap::new();

    for (project_id, title, status) in &projects {
        let amount = match extract_amount_for_project(&conn, *project_id) {
            Ok(a) => a,
            Err(e) => return CommandResult::err("pipeline", &e),
        };
        let stage = classify_stage(status);

        *stage_totals.entry(stage.clone()).or_insert(0.0) += amount;
        *stage_counts.entry(stage.clone()).or_insert(0) += 1;

        pipeline_items.push(json!({
            "project_id": project_id,
            "title": title,
            "status": status,
            "stage": stage,
            "amount": amount,
            "amount_display": format_amount(amount)
        }));
    }

    // Sort by amount descending
    pipeline_items.sort_by(|a, b| {
        let a_amt = a["amount"].as_f64().unwrap_or(0.0);
        let b_amt = b["amount"].as_f64().unwrap_or(0.0);
        b_amt
            .partial_cmp(&a_amt)
            .unwrap_or(std::cmp::Ordering::Equal)
    });

    // Build stage summary in pipeline order
    let stage_order = [
        "lead",
        "proposal",
        "negotiation",
        "contract",
        "execution",
        "completed",
        "lost",
        "unknown",
    ];
    let stages: Vec<Value> = stage_order
        .iter()
        .filter_map(|&s| {
            let count = stage_counts.get(s).copied().unwrap_or(0);
            if count > 0 {
                Some(json!({
                    "stage": s,
                    "stage_label": stage_label(s),
                    "count": count,
                    "total_amount": stage_totals.get(s).copied().unwrap_or(0.0),
                    "total_display": format_amount(stage_totals.get(s).copied().unwrap_or(0.0))
                }))
            } else {
                None
            }
        })
        .collect();

    let total_amount: f64 = stage_totals.values().sum();
    let weighted_amount: f64 = stage_totals
        .iter()
        .map(|(stage, amount)| amount * stage_weight(stage))
        .sum();

    CommandResult::ok(
        "pipeline",
        json!({
            "summary": {
                "total_projects": projects.len(),
                "total_amount": total_amount,
                "total_display": format_amount(total_amount),
                "weighted_amount": weighted_amount,
                "weighted_display": format_amount(weighted_amount)
            },
            "stages": stages,
            "items": pipeline_items
        }),
    )
}

/// Get all active projects.
fn get_active_projects(conn: &Connection) -> Result<Vec<(i64, String, String)>, String> {
    let sql = "
        SELECT id, title, COALESCE(status, '') AS status
        FROM projects
        WHERE status NOT LIKE '%완료%'
          AND status NOT LIKE '%종료%'
        ORDER BY title
    ";
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map([], |row| {
            let id: i64 = row.get(0)?;
            let title: String = row.get(1)?;
            let status: String = row.get(2)?;
            Ok((id, title, status))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut projects = Vec::new();
    for row in rows {
        projects.push(row.map_err(|e| format!("행 읽기 실패: {}", e))?);
    }
    Ok(projects)
}

/// Extract monetary amounts from project chunks.
/// Looks for Korean Won amounts (만원, 억원, 원) and plain numbers.
fn extract_amount_for_project(conn: &Connection, project_id: i64) -> Result<f64, String> {
    let sql = "
        SELECT body FROM chunks
        WHERE project_id = ?1
          AND chunk_type IN ('body', 'note', 'amount', 'budget', 'contract_amount')
        ORDER BY seq
    ";
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map(rusqlite::params![project_id], |row| {
            let body: String = row.get(0)?;
            Ok(body)
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut max_amount: f64 = 0.0;

    for row in rows {
        let body = row.map_err(|e| format!("행 읽기 실패: {}", e))?;
        let amount = extract_amount_from_text(&body);
        if amount > max_amount {
            max_amount = amount;
        }
    }

    Ok(max_amount)
}

/// Extract monetary amount from text using regex patterns.
/// Supports Korean Won formats: 억원, 만원, 원, and comma-separated numbers.
fn extract_amount_from_text(text: &str) -> f64 {
    // Pattern: N억 M만원 or N억원
    let billions_re = Regex::new(r"(\d+(?:\.\d+)?)\s*억\s*(?:(\d+(?:\.\d+)?)\s*만)?\s*원?").unwrap();
    // Pattern: N만원
    let ten_thousands_re = Regex::new(r"(\d+(?:,\d+)?(?:\.\d+)?)\s*만\s*원").unwrap();
    // Pattern: N원 (plain number with 원)
    let won_re = Regex::new(r"(\d{1,3}(?:,\d{3})+)\s*원").unwrap();

    let mut max_amount: f64 = 0.0;

    // Check 억원 patterns first (largest unit)
    for caps in billions_re.captures_iter(text) {
        let billions: f64 = caps
            .get(1)
            .and_then(|m| m.as_str().parse().ok())
            .unwrap_or(0.0);
        let ten_thousands: f64 = caps
            .get(2)
            .and_then(|m| m.as_str().parse().ok())
            .unwrap_or(0.0);
        let amount = billions * 100_000_000.0 + ten_thousands * 10_000.0;
        if amount > max_amount {
            max_amount = amount;
        }
    }

    // Check 만원 patterns
    for caps in ten_thousands_re.captures_iter(text) {
        let raw = caps.get(1).map(|m| m.as_str()).unwrap_or("0");
        let cleaned = raw.replace(',', "");
        let ten_thousands: f64 = cleaned.parse().unwrap_or(0.0);
        let amount = ten_thousands * 10_000.0;
        if amount > max_amount {
            max_amount = amount;
        }
    }

    // Check plain 원 patterns (comma-formatted)
    for caps in won_re.captures_iter(text) {
        let raw = caps.get(1).map(|m| m.as_str()).unwrap_or("0");
        let cleaned = raw.replace(',', "");
        let amount: f64 = cleaned.parse().unwrap_or(0.0);
        if amount > max_amount {
            max_amount = amount;
        }
    }

    max_amount
}

/// Classify project stage based on status text.
fn classify_stage(status: &str) -> String {
    let s = status.to_lowercase();
    if s.contains("리드") || s.contains("발굴") || s.contains("탐색") {
        "lead".to_string()
    } else if s.contains("제안") || s.contains("입찰") || s.contains("견적") {
        "proposal".to_string()
    } else if s.contains("협상") || s.contains("검토") || s.contains("심사") {
        "negotiation".to_string()
    } else if s.contains("계약") || s.contains("수주") || s.contains("착수") {
        "contract".to_string()
    } else if s.contains("진행") || s.contains("시공") || s.contains("실행") || s.contains("🟢") {
        "execution".to_string()
    } else if s.contains("완료") || s.contains("준공") || s.contains("종료") {
        "completed".to_string()
    } else if s.contains("실패") || s.contains("탈락") || s.contains("포기") || s.contains("취소")
    {
        "lost".to_string()
    } else {
        "unknown".to_string()
    }
}

/// Get Korean label for pipeline stage.
fn stage_label(stage: &str) -> &str {
    match stage {
        "lead" => "리드/발굴",
        "proposal" => "제안/입찰",
        "negotiation" => "협상/검토",
        "contract" => "계약/수주",
        "execution" => "진행/실행",
        "completed" => "완료",
        "lost" => "실패/탈락",
        _ => "기타",
    }
}

/// Get probability weight for pipeline stage (for weighted pipeline value).
fn stage_weight(stage: &str) -> f64 {
    match stage {
        "lead" => 0.1,
        "proposal" => 0.25,
        "negotiation" => 0.5,
        "contract" => 0.75,
        "execution" => 0.9,
        "completed" => 1.0,
        "lost" => 0.0,
        _ => 0.1,
    }
}

/// Format amount in Korean Won display format.
fn format_amount(amount: f64) -> String {
    if amount >= 100_000_000.0 {
        let billions = amount / 100_000_000.0;
        let remainder = (amount % 100_000_000.0) / 10_000.0;
        if remainder > 0.0 {
            format!("{:.0}억 {:.0}만원", billions, remainder)
        } else {
            format!("{:.0}억원", billions)
        }
    } else if amount >= 10_000.0 {
        format!("{:.0}만원", amount / 10_000.0)
    } else if amount > 0.0 {
        format!("{:.0}원", amount)
    } else {
        "미정".to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_extract_amount_billions() {
        assert_eq!(extract_amount_from_text("총 3억원 규모"), 300_000_000.0);
        assert_eq!(
            extract_amount_from_text("약 2억 5000만원"),
            250_000_000.0
        );
    }

    #[test]
    fn test_extract_amount_ten_thousands() {
        assert_eq!(extract_amount_from_text("5000만원"), 50_000_000.0);
        assert_eq!(extract_amount_from_text("500만원 예산"), 5_000_000.0);
    }

    #[test]
    fn test_extract_amount_plain_won() {
        assert_eq!(
            extract_amount_from_text("50,000,000원"),
            50_000_000.0
        );
    }

    #[test]
    fn test_classify_stage() {
        assert_eq!(classify_stage("🟢 진행중"), "execution");
        assert_eq!(classify_stage("제안 준비"), "proposal");
        assert_eq!(classify_stage("계약 완료"), "contract");
        assert_eq!(classify_stage("리드 발굴"), "lead");
    }

    #[test]
    fn test_format_amount() {
        assert_eq!(format_amount(300_000_000.0), "3억원");
        assert_eq!(format_amount(250_000_000.0), "2억 5000만원");
        assert_eq!(format_amount(5_000_000.0), "500만원");
        assert_eq!(format_amount(0.0), "미정");
    }
}
