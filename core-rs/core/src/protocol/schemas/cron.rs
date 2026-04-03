//! Cron schema validators — mirrors `src/gateway/protocol/schema/cron.ts`.

use crate::protocol::validation::*;

// ---------------------------------------------------------------------------
// cron.list / cron.status
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_cron_list_params {
        [opt "includeDisabled" => boolean],
        [opt "limit" => integer(Some(1), Some(200))],
        [opt "offset" => integer(Some(0), None)],
        [opt "query" => string],
        [opt "enabled" => string_enum["all", "enabled", "disabled"]],
        [opt "sortBy" => string_enum["nextRunAtMs", "updatedAtMs", "name"]],
        [opt "sortDir" => string_enum["asc", "desc"]],
    }
}

define_schema! { pub fn validate_cron_status_params {} }

// ---------------------------------------------------------------------------
// cron.add
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_cron_add_params {
        [req "name" => non_empty_string],
        [opt_null "agentId" => non_empty_string],
        [opt_null "sessionKey" => non_empty_string],
        [opt "description" => string],
        [opt "enabled" => boolean],
        [opt "deleteAfterRun" => boolean],
        [req "schedule" => custom(validate_cron_schedule)],
        [req "sessionTarget" => custom(validate_cron_session_target)],
        [req "wakeMode" => string_enum["next-heartbeat", "now"]],
        [req "payload" => custom(validate_cron_payload)],
        [opt "delivery" => any],
        [opt "failureAlert" => any],
    }
}

// ---------------------------------------------------------------------------
// Discriminated union validators (manual — not expressible via define_schema!)
// ---------------------------------------------------------------------------

fn validate_cron_schedule(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else {
        return;
    };
    match obj.get("kind").and_then(|v| v.as_str()) {
        Some("at") => {
            check_no_additional_properties(obj, &["kind", "at"], path, errors);
            if check_required(obj, "at", path, errors) {
                check_non_empty_string(&obj["at"], &format!("{path}/at"), errors);
            }
        }
        Some("every") => {
            check_no_additional_properties(obj, &["kind", "everyMs", "anchorMs"], path, errors);
            if check_required(obj, "everyMs", path, errors) {
                check_integer(
                    &obj["everyMs"],
                    &format!("{path}/everyMs"),
                    Some(1),
                    None,
                    errors,
                );
            }
            check_optional(obj, "anchorMs", path, errors, |v, p, e| {
                check_integer(v, p, Some(0), None, e);
            });
        }
        Some("cron") => {
            check_no_additional_properties(obj, &["kind", "expr", "tz", "staggerMs"], path, errors);
            if check_required(obj, "expr", path, errors) {
                check_non_empty_string(&obj["expr"], &format!("{path}/expr"), errors);
            }
            check_optional(obj, "tz", path, errors, |v, p, e| {
                check_string(v, p, e);
            });
            check_optional(obj, "staggerMs", path, errors, |v, p, e| {
                check_integer(v, p, Some(0), None, e);
            });
        }
        _ => {
            if !obj.contains_key("kind") {
                check_required(obj, "kind", path, errors);
            } else {
                check_string_enum(
                    &obj["kind"],
                    &format!("{path}/kind"),
                    &["at", "every", "cron"],
                    errors,
                );
            }
        }
    }
}

fn validate_cron_session_target(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    // Union: "main" | "isolated" | "current" | string matching "^session:.+"
    match value.as_str() {
        Some("main") | Some("isolated") | Some("current") => {}
        Some(s) if s.starts_with("session:") && s.len() > 8 => {}
        Some(_) => {
            errors.push(ValidationError {
                path: path.to_string(),
                message: "must be \"main\", \"isolated\", \"current\", or \"session:<key>\""
                    .to_string(),
                keyword: "enum",
            });
        }
        None => {
            errors.push(ValidationError {
                path: path.to_string(),
                message: "must be string".to_string(),
                keyword: "type",
            });
        }
    }
}

fn validate_cron_payload(value: &serde_json::Value, path: &str, errors: &mut Vec<ValidationError>) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else {
        return;
    };
    match obj.get("kind").and_then(|v| v.as_str()) {
        Some("systemEvent") => {
            check_no_additional_properties(obj, &["kind", "text"], path, errors);
            if check_required(obj, "text", path, errors) {
                check_non_empty_string(&obj["text"], &format!("{path}/text"), errors);
            }
        }
        Some("agentTurn") => {
            let allowed = &[
                "kind",
                "message",
                "model",
                "fallbacks",
                "thinking",
                "timeoutSeconds",
                "allowUnsafeExternalContent",
                "lightContext",
                "deliver",
                "channel",
                "to",
                "bestEffortDeliver",
            ];
            check_no_additional_properties(obj, allowed, path, errors);
            if check_required(obj, "message", path, errors) {
                check_non_empty_string(&obj["message"], &format!("{path}/message"), errors);
            }
        }
        _ => {
            if !obj.contains_key("kind") {
                check_required(obj, "kind", path, errors);
            } else {
                check_string_enum(
                    &obj["kind"],
                    &format!("{path}/kind"),
                    &["systemEvent", "agentTurn"],
                    errors,
                );
            }
        }
    }
}

// ---------------------------------------------------------------------------
// cron.update / cron.remove / cron.run (id-or-jobId union)
// ---------------------------------------------------------------------------

/// cron.update and cron.remove use cronIdOrJobIdParams — union of { id } | { jobId }
fn validate_cron_id_or_job_id(
    value: &serde_json::Value,
    path: &str,
    extra_allowed: &[&str],
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else {
        return;
    };
    let has_id = obj.contains_key("id");
    let has_job_id = obj.contains_key("jobId");

    if !has_id && !has_job_id {
        errors.push(ValidationError {
            path: path.to_string(),
            message: "must have property 'id' or 'jobId'".to_string(),
            keyword: "required",
        });
    }

    let mut allowed: Vec<&str> = vec!["id", "jobId"];
    allowed.extend_from_slice(extra_allowed);
    check_no_additional_properties(obj, &allowed, path, errors);

    if has_id {
        check_non_empty_string(&obj["id"], &format!("{path}/id"), errors);
    }
    if has_job_id {
        check_non_empty_string(&obj["jobId"], &format!("{path}/jobId"), errors);
    }
}

pub fn validate_cron_update_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_cron_id_or_job_id(value, path, &["patch"], errors);
    // patch is a complex CronJobPatchSchema — validate as object.
    if let Some(obj) = value.as_object() {
        check_optional(obj, "patch", path, errors, |v, p, e| {
            require_object(v, p, e);
        });
    }
}

pub fn validate_cron_remove_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_cron_id_or_job_id(value, path, &[], errors);
}

pub fn validate_cron_run_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_cron_id_or_job_id(value, path, &["mode"], errors);
    if let Some(obj) = value.as_object() {
        check_optional(obj, "mode", path, errors, |v, p, e| {
            check_string_enum(v, p, &["due", "force"], e);
        });
    }
}

// ---------------------------------------------------------------------------
// cron.runs
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_cron_runs_params {
        [opt "scope" => string_enum["job", "all"]],
        [opt "id" => custom(check_cron_run_job_id)],
        [opt "jobId" => custom(check_cron_run_job_id)],
        [opt "limit" => integer(Some(1), Some(200))],
        [opt "offset" => integer(Some(0), None)],
        [opt "statuses" => custom(check_statuses_array)],
        [opt "status" => string_enum["all", "ok", "error", "skipped"]],
        [opt "deliveryStatuses" => custom(check_delivery_statuses_array)],
        [opt "deliveryStatus" => string_enum["delivered", "not-delivered", "unknown", "not-requested"]],
        [opt "query" => string],
        [opt "sortDir" => string_enum["asc", "desc"]],
    }
}

fn check_cron_run_job_id(v: &serde_json::Value, p: &str, e: &mut Vec<ValidationError>) {
    #[allow(clippy::expect_used)]
    static JOB_ID_RE: std::sync::LazyLock<regex::Regex> =
        std::sync::LazyLock::new(|| regex::Regex::new(r"^[^/\\]+$").expect("valid regex"));
    check_non_empty_string(v, p, e);
    check_pattern(v, p, &JOB_ID_RE, e);
}

fn check_statuses_array(v: &serde_json::Value, p: &str, e: &mut Vec<ValidationError>) {
    if check_array(v, p, e) {
        check_min_items(v, p, 1, e);
        if let Some(arr) = v.as_array() {
            if arr.len() > 3 {
                e.push(ValidationError {
                    path: p.to_string(),
                    message: "must NOT have more than 3 items".to_string(),
                    keyword: "maxItems",
                });
            }
            for (i, item) in arr.iter().enumerate() {
                check_string_enum(item, &format!("{p}/{i}"), &["ok", "error", "skipped"], e);
            }
        }
    }
}

fn check_delivery_statuses_array(v: &serde_json::Value, p: &str, e: &mut Vec<ValidationError>) {
    if check_array(v, p, e) {
        check_min_items(v, p, 1, e);
        if let Some(arr) = v.as_array() {
            if arr.len() > 4 {
                e.push(ValidationError {
                    path: p.to_string(),
                    message: "must NOT have more than 4 items".to_string(),
                    keyword: "maxItems",
                });
            }
            for (i, item) in arr.iter().enumerate() {
                check_string_enum(
                    item,
                    &format!("{p}/{i}"),
                    &["delivered", "not-delivered", "unknown", "not-requested"],
                    e,
                );
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_cron_list_valid() {
        let mut e = Vec::new();
        validate_cron_list_params(&json!({"limit": 50, "sortBy": "name"}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_cron_add_valid() {
        let mut e = Vec::new();
        validate_cron_add_params(
            &json!({
                "name": "daily-check",
                "schedule": {"kind": "cron", "expr": "0 9 * * *"},
                "sessionTarget": "main",
                "wakeMode": "now",
                "payload": {"kind": "systemEvent", "text": "check"}
            }),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }

    #[test]
    fn test_cron_remove_valid() {
        let mut e = Vec::new();
        validate_cron_remove_params(&json!({"id": "job-1"}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_cron_remove_with_job_id() {
        let mut e = Vec::new();
        validate_cron_remove_params(&json!({"jobId": "job-1"}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_cron_remove_missing_id() {
        let mut e = Vec::new();
        validate_cron_remove_params(&json!({}), "", &mut e);
        assert!(!e.is_empty());
    }

    #[test]
    fn test_cron_session_target_custom() {
        let mut e = Vec::new();
        validate_cron_add_params(
            &json!({
                "name": "t",
                "schedule": {"kind": "at", "at": "2024-01-01"},
                "sessionTarget": "session:my-key",
                "wakeMode": "now",
                "payload": {"kind": "systemEvent", "text": "t"}
            }),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }
}
