//! Cron schema validators — mirrors `src/gateway/protocol/schema/cron.ts`.

use crate::protocol::validation::*;

pub fn validate_cron_list_params(
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
    let allowed = &[
        "includeDisabled",
        "limit",
        "offset",
        "query",
        "enabled",
        "sortBy",
        "sortDir",
    ];
    check_no_additional_properties(obj, allowed, path, errors);
    check_optional(obj, "includeDisabled", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "limit", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), Some(200), e);
    });
    check_optional(obj, "offset", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
    check_optional(obj, "query", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "enabled", path, errors, |v, p, e| {
        check_string_enum(v, p, &["all", "enabled", "disabled"], e);
    });
    check_optional(obj, "sortBy", path, errors, |v, p, e| {
        check_string_enum(v, p, &["nextRunAtMs", "updatedAtMs", "name"], e);
    });
    check_optional(obj, "sortDir", path, errors, |v, p, e| {
        check_string_enum(v, p, &["asc", "desc"], e);
    });
}

pub fn validate_cron_status_params(
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
    check_no_additional_properties(obj, &[], path, errors);
}

pub fn validate_cron_add_params(
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
    let allowed = &[
        "name",
        "agentId",
        "sessionKey",
        "description",
        "enabled",
        "deleteAfterRun",
        "schedule",
        "sessionTarget",
        "wakeMode",
        "payload",
        "delivery",
        "failureAlert",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    if check_required(obj, "name", path, errors) {
        check_non_empty_string(&obj["name"], &format!("{path}/name"), errors);
    }
    // Common optional fields
    check_optional_nullable(obj, "agentId", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional_nullable(obj, "sessionKey", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "description", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "enabled", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "deleteAfterRun", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });

    if check_required(obj, "schedule", path, errors) {
        validate_cron_schedule(&obj["schedule"], &format!("{path}/schedule"), errors);
    }
    if check_required(obj, "sessionTarget", path, errors) {
        validate_cron_session_target(
            &obj["sessionTarget"],
            &format!("{path}/sessionTarget"),
            errors,
        );
    }
    if check_required(obj, "wakeMode", path, errors) {
        check_string_enum(
            &obj["wakeMode"],
            &format!("{path}/wakeMode"),
            &["next-heartbeat", "now"],
            errors,
        );
    }
    if check_required(obj, "payload", path, errors) {
        validate_cron_payload(&obj["payload"], &format!("{path}/payload"), errors);
    }
    // delivery and failureAlert are complex — accept as objects for now.
}

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

pub fn validate_cron_runs_params(
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
    let allowed = &[
        "scope",
        "id",
        "jobId",
        "limit",
        "offset",
        "statuses",
        "status",
        "deliveryStatuses",
        "deliveryStatus",
        "query",
        "sortDir",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    check_optional(obj, "scope", path, errors, |v, p, e| {
        check_string_enum(v, p, &["job", "all"], e);
    });

    // id and jobId use CronRunLogJobIdSchema: minLength 1, pattern ^[^/\\]+$
    #[allow(clippy::expect_used)]
    static JOB_ID_RE: std::sync::LazyLock<regex::Regex> =
        std::sync::LazyLock::new(|| regex::Regex::new(r"^[^/\\]+$").expect("valid regex"));
    for f in &["id", "jobId"] {
        check_optional(obj, f, path, errors, |v, p, e| {
            check_non_empty_string(v, p, e);
            check_pattern(v, p, &JOB_ID_RE, e);
        });
    }

    check_optional(obj, "limit", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), Some(200), e);
    });
    check_optional(obj, "offset", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
    check_optional(obj, "statuses", path, errors, |v, p, e| {
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
    });
    check_optional(obj, "status", path, errors, |v, p, e| {
        check_string_enum(v, p, &["all", "ok", "error", "skipped"], e);
    });
    check_optional(obj, "deliveryStatuses", path, errors, |v, p, e| {
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
    });
    check_optional(obj, "deliveryStatus", path, errors, |v, p, e| {
        check_string_enum(
            v,
            p,
            &["delivered", "not-delivered", "unknown", "not-requested"],
            e,
        );
    });
    check_optional(obj, "query", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "sortDir", path, errors, |v, p, e| {
        check_string_enum(v, p, &["asc", "desc"], e);
    });
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
