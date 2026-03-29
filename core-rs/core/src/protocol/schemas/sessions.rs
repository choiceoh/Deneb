//! Session schema validators — mirrors `src/gateway/protocol/schema/sessions.ts`.

use crate::protocol::constants::SESSION_LABEL_MAX_LENGTH;
use crate::protocol::validation::*;

// ---------------------------------------------------------------------------
// sessions.list
// ---------------------------------------------------------------------------

pub fn validate_sessions_list_params(
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
        "limit",
        "activeMinutes",
        "includeGlobal",
        "includeUnknown",
        "includeDerivedTitles",
        "includeLastMessage",
        "label",
        "spawnedBy",
        "agentId",
        "search",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    check_optional(obj, "limit", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), None, e);
    });
    check_optional(obj, "activeMinutes", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), None, e);
    });
    check_optional(obj, "includeGlobal", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "includeUnknown", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "includeDerivedTitles", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "includeLastMessage", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "label", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
        check_max_length(v, p, SESSION_LABEL_MAX_LENGTH, e);
    });
    check_optional(obj, "spawnedBy", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "agentId", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "search", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
}

// ---------------------------------------------------------------------------
// sessions.preview
// ---------------------------------------------------------------------------

pub fn validate_sessions_preview_params(
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
    let allowed = &["keys", "limit", "maxChars"];
    check_no_additional_properties(obj, allowed, path, errors);

    if check_required(obj, "keys", path, errors) {
        let keys_path = format!("{path}/keys");
        let keys = &obj["keys"];
        if check_array(keys, &keys_path, errors) {
            check_min_items(keys, &keys_path, 1, errors);
            if let Some(arr) = keys.as_array() {
                for (i, item) in arr.iter().enumerate() {
                    check_non_empty_string(item, &format!("{keys_path}/{i}"), errors);
                }
            }
        }
    }
    check_optional(obj, "limit", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), None, e);
    });
    check_optional(obj, "maxChars", path, errors, |v, p, e| {
        check_integer(v, p, Some(20), None, e);
    });
}

// ---------------------------------------------------------------------------
// sessions.resolve
// ---------------------------------------------------------------------------

pub fn validate_sessions_resolve_params(
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
        "key",
        "sessionId",
        "label",
        "agentId",
        "spawnedBy",
        "includeGlobal",
        "includeUnknown",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    check_optional(obj, "key", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "sessionId", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "label", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
        check_max_length(v, p, SESSION_LABEL_MAX_LENGTH, e);
    });
    check_optional(obj, "agentId", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "spawnedBy", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "includeGlobal", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "includeUnknown", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
}

// ---------------------------------------------------------------------------
// sessions.create
// ---------------------------------------------------------------------------

pub fn validate_sessions_create_params(
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
        "key",
        "agentId",
        "label",
        "model",
        "parentSessionKey",
        "task",
        "message",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    check_optional(obj, "key", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "agentId", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "label", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
        check_max_length(v, p, SESSION_LABEL_MAX_LENGTH, e);
    });
    check_optional(obj, "model", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "parentSessionKey", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "task", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "message", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
}

// ---------------------------------------------------------------------------
// sessions.send
// ---------------------------------------------------------------------------

pub fn validate_sessions_send_params(
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
        "key",
        "message",
        "thinking",
        "attachments",
        "timeoutMs",
        "idempotencyKey",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    if check_required(obj, "key", path, errors) {
        check_non_empty_string(&obj["key"], &format!("{path}/key"), errors);
    }
    if check_required(obj, "message", path, errors) {
        check_string(&obj["message"], &format!("{path}/message"), errors);
    }
    check_optional(obj, "thinking", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "attachments", path, errors, |v, p, e| {
        check_array(v, p, e);
        // Items are Type.Unknown() — no further validation.
    });
    check_optional(obj, "timeoutMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
    check_optional(obj, "idempotencyKey", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
}

// ---------------------------------------------------------------------------
// sessions.messages.subscribe / unsubscribe
// ---------------------------------------------------------------------------

fn validate_session_key_only(
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
    check_no_additional_properties(obj, &["key"], path, errors);
    if check_required(obj, "key", path, errors) {
        check_non_empty_string(&obj["key"], &format!("{path}/key"), errors);
    }
}

pub fn validate_sessions_messages_subscribe_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_session_key_only(value, path, errors);
}

pub fn validate_sessions_messages_unsubscribe_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_session_key_only(value, path, errors);
}

// ---------------------------------------------------------------------------
// sessions.abort
// ---------------------------------------------------------------------------

pub fn validate_sessions_abort_params(
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
    check_no_additional_properties(obj, &["key", "runId"], path, errors);
    if check_required(obj, "key", path, errors) {
        check_non_empty_string(&obj["key"], &format!("{path}/key"), errors);
    }
    check_optional(obj, "runId", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
}

// ---------------------------------------------------------------------------
// sessions.patch
// ---------------------------------------------------------------------------

pub fn validate_sessions_patch_params(
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
        "key",
        "label",
        "thinkingLevel",
        "fastMode",
        "verboseLevel",
        "reasoningLevel",
        "responseUsage",
        "elevatedLevel",
        "execHost",
        "execSecurity",
        "execAsk",
        "execNode",
        "model",
        "spawnedBy",
        "spawnedWorkspaceDir",
        "spawnDepth",
        "subagentRole",
        "subagentControlScope",
        "sendPolicy",
        "groupActivation",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    if check_required(obj, "key", path, errors) {
        check_non_empty_string(&obj["key"], &format!("{path}/key"), errors);
    }

    // Nullable string fields.
    let nullable_string_fields = &[
        "label",
        "thinkingLevel",
        "verboseLevel",
        "reasoningLevel",
        "elevatedLevel",
        "execHost",
        "execSecurity",
        "execAsk",
        "execNode",
        "model",
        "spawnedBy",
        "spawnedWorkspaceDir",
    ];
    for field in nullable_string_fields {
        check_optional_nullable(obj, field, path, errors, |v, p, e| {
            check_non_empty_string(v, p, e);
            if *field == "label" {
                check_max_length(v, p, SESSION_LABEL_MAX_LENGTH, e);
            }
        });
    }

    // Nullable boolean fields.
    check_optional_nullable(obj, "fastMode", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });

    // responseUsage: "off" | "tokens" | "full" | "on" | null
    check_optional_nullable(obj, "responseUsage", path, errors, |v, p, e| {
        check_string_enum(v, p, &["off", "tokens", "full", "on"], e);
    });

    // spawnDepth: integer | null
    check_optional_nullable(obj, "spawnDepth", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });

    // subagentRole: "orchestrator" | "leaf" | null
    check_optional_nullable(obj, "subagentRole", path, errors, |v, p, e| {
        check_string_enum(v, p, &["orchestrator", "leaf"], e);
    });

    // subagentControlScope: "children" | "none" | null
    check_optional_nullable(obj, "subagentControlScope", path, errors, |v, p, e| {
        check_string_enum(v, p, &["children", "none"], e);
    });

    // sendPolicy: "allow" | "deny" | null
    check_optional_nullable(obj, "sendPolicy", path, errors, |v, p, e| {
        check_string_enum(v, p, &["allow", "deny"], e);
    });

    // groupActivation: "mention" | "always" | null
    check_optional_nullable(obj, "groupActivation", path, errors, |v, p, e| {
        check_string_enum(v, p, &["mention", "always"], e);
    });
}

// ---------------------------------------------------------------------------
// sessions.reset
// ---------------------------------------------------------------------------

pub fn validate_sessions_reset_params(
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
    check_no_additional_properties(obj, &["key", "reason"], path, errors);
    if check_required(obj, "key", path, errors) {
        check_non_empty_string(&obj["key"], &format!("{path}/key"), errors);
    }
    check_optional(obj, "reason", path, errors, |v, p, e| {
        check_string_enum(v, p, &["new", "reset"], e);
    });
}

// ---------------------------------------------------------------------------
// sessions.delete
// ---------------------------------------------------------------------------

pub fn validate_sessions_delete_params(
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
    check_no_additional_properties(
        obj,
        &["key", "deleteTranscript", "emitLifecycleHooks"],
        path,
        errors,
    );
    if check_required(obj, "key", path, errors) {
        check_non_empty_string(&obj["key"], &format!("{path}/key"), errors);
    }
    check_optional(obj, "deleteTranscript", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "emitLifecycleHooks", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
}

// ---------------------------------------------------------------------------
// sessions.compact
// ---------------------------------------------------------------------------

pub fn validate_sessions_compact_params(
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
    check_no_additional_properties(obj, &["key", "maxLines"], path, errors);
    if check_required(obj, "key", path, errors) {
        check_non_empty_string(&obj["key"], &format!("{path}/key"), errors);
    }
    check_optional(obj, "maxLines", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), None, e);
    });
}

// ---------------------------------------------------------------------------
// sessions.usage
// ---------------------------------------------------------------------------

pub fn validate_sessions_usage_params(
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
        "key",
        "startDate",
        "endDate",
        "mode",
        "utcOffset",
        "limit",
        "includeContextWeight",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    #[allow(clippy::expect_used)]
    static DATE_RE: once_cell::sync::Lazy<regex::Regex> = once_cell::sync::Lazy::new(|| {
        regex::Regex::new(r"^\d{4}-\d{2}-\d{2}$").expect("valid regex")
    });
    #[allow(clippy::expect_used)]
    static UTC_OFFSET_RE: once_cell::sync::Lazy<regex::Regex> = once_cell::sync::Lazy::new(|| {
        regex::Regex::new(r"^UTC[+-]\d{1,2}(?::[0-5]\d)?$").expect("valid regex")
    });

    check_optional(obj, "key", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "startDate", path, errors, |v, p, e| {
        if check_string(v, p, e) {
            check_pattern(v, p, &DATE_RE, e);
        }
    });
    check_optional(obj, "endDate", path, errors, |v, p, e| {
        if check_string(v, p, e) {
            check_pattern(v, p, &DATE_RE, e);
        }
    });
    check_optional(obj, "mode", path, errors, |v, p, e| {
        check_string_enum(v, p, &["utc", "gateway", "specific"], e);
    });
    check_optional(obj, "utcOffset", path, errors, |v, p, e| {
        if check_string(v, p, e) {
            check_pattern(v, p, &UTC_OFFSET_RE, e);
        }
    });
    check_optional(obj, "limit", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), None, e);
    });
    check_optional(obj, "includeContextWeight", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_sessions_list_valid_empty() {
        let mut errors = Vec::new();
        validate_sessions_list_params(&json!({}), "", &mut errors);
        assert!(errors.is_empty());
    }

    #[test]
    fn test_sessions_list_valid_with_fields() {
        let mut errors = Vec::new();
        validate_sessions_list_params(
            &json!({"limit": 10, "includeGlobal": true, "label": "test"}),
            "",
            &mut errors,
        );
        assert!(errors.is_empty());
    }

    #[test]
    fn test_sessions_list_additional_property() {
        let mut errors = Vec::new();
        validate_sessions_list_params(&json!({"unknown": true}), "", &mut errors);
        assert_eq!(errors.len(), 1);
        assert_eq!(errors[0].keyword, "additionalProperties");
    }

    #[test]
    fn test_sessions_send_missing_required() {
        let mut errors = Vec::new();
        validate_sessions_send_params(&json!({}), "", &mut errors);
        assert!(errors
            .iter()
            .any(|e| e.keyword == "required" && e.path.contains("key")));
        assert!(errors
            .iter()
            .any(|e| e.keyword == "required" && e.path.contains("message")));
    }

    #[test]
    fn test_sessions_send_valid() {
        let mut errors = Vec::new();
        validate_sessions_send_params(
            &json!({"key": "sess-1", "message": "hello"}),
            "",
            &mut errors,
        );
        assert!(errors.is_empty());
    }

    #[test]
    fn test_sessions_patch_nullable_fields() {
        let mut errors = Vec::new();
        validate_sessions_patch_params(
            &json!({"key": "k", "label": null, "fastMode": null, "responseUsage": null}),
            "",
            &mut errors,
        );
        assert!(
            errors.is_empty(),
            "null values should be accepted for nullable fields"
        );
    }

    #[test]
    fn test_sessions_patch_invalid_enum() {
        let mut errors = Vec::new();
        validate_sessions_patch_params(
            &json!({"key": "k", "responseUsage": "invalid"}),
            "",
            &mut errors,
        );
        assert!(errors.iter().any(|e| e.keyword == "enum"));
    }

    #[test]
    fn test_sessions_usage_date_pattern() {
        let mut errors = Vec::new();
        validate_sessions_usage_params(&json!({"startDate": "2024-01-15"}), "", &mut errors);
        assert!(errors.is_empty());

        let mut errors = Vec::new();
        validate_sessions_usage_params(&json!({"startDate": "not-a-date"}), "", &mut errors);
        assert!(errors.iter().any(|e| e.keyword == "pattern"));
    }
}
