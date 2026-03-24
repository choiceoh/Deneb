//! Protocol parameter validation framework.
//!
//! Provides a method-dispatched validator that validates RPC parameters
//! against schemas equivalent to the TypeScript TypeBox/AJV definitions
//! in `src/gateway/protocol/schema/`.
//!
//! Each schema maps to a validation function operating on `serde_json::Value`
//! (not typed structs) for rich error reporting with field paths.

use once_cell::sync::Lazy;
use regex::Regex;
use serde::Serialize;

use super::schemas;

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

/// A single validation error with AJV-compatible fields.
#[derive(Debug, Clone, Serialize)]
pub struct ValidationError {
    /// JSON pointer path to the offending field (e.g. "/key", "/options/2").
    pub path: String,
    /// Human-readable error message.
    pub message: String,
    /// AJV-compatible keyword (e.g. "required", "minLength", "additionalProperties").
    pub keyword: &'static str,
}

/// Result of validating RPC parameters.
#[derive(Debug, Clone, Serialize)]
pub struct ValidationResult {
    pub valid: bool,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub errors: Vec<ValidationError>,
}

impl ValidationResult {
    pub fn ok() -> Self {
        Self {
            valid: true,
            errors: Vec::new(),
        }
    }

    pub fn from_errors(errors: Vec<ValidationError>) -> Self {
        if errors.is_empty() {
            Self::ok()
        } else {
            Self {
                valid: false,
                errors,
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

/// Check that a value is an object; if not, push an error.
pub fn require_object(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) -> bool {
    if value.is_object() {
        true
    } else {
        errors.push(ValidationError {
            path: path.to_string(),
            message: "must be object".to_string(),
            keyword: "type",
        });
        false
    }
}

/// Check that a required field exists on an object.
pub fn check_required(
    obj: &serde_json::Map<String, serde_json::Value>,
    field: &str,
    parent_path: &str,
    errors: &mut Vec<ValidationError>,
) -> bool {
    if obj.contains_key(field) {
        true
    } else {
        errors.push(ValidationError {
            path: format!("{parent_path}/{field}"),
            message: format!("must have required property '{field}'"),
            keyword: "required",
        });
        false
    }
}

/// Check that a value is a string.
pub fn check_string(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) -> bool {
    if value.is_string() {
        true
    } else {
        errors.push(ValidationError {
            path: path.to_string(),
            message: "must be string".to_string(),
            keyword: "type",
        });
        false
    }
}

/// Check that a string value has `minLength >= 1` (NonEmptyString).
pub fn check_non_empty_string(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) -> bool {
    match value.as_str() {
        Some(s) if !s.is_empty() => true,
        Some(_) => {
            errors.push(ValidationError {
                path: path.to_string(),
                message: "must NOT have fewer than 1 characters".to_string(),
                keyword: "minLength",
            });
            false
        }
        None => {
            errors.push(ValidationError {
                path: path.to_string(),
                message: "must be string".to_string(),
                keyword: "type",
            });
            false
        }
    }
}

/// Check that a string is at most `max_len` characters.
pub fn check_max_length(
    value: &serde_json::Value,
    path: &str,
    max_len: usize,
    errors: &mut Vec<ValidationError>,
) {
    if let Some(s) = value.as_str() {
        if s.chars().count() > max_len {
            errors.push(ValidationError {
                path: path.to_string(),
                message: format!("must NOT have more than {max_len} characters"),
                keyword: "maxLength",
            });
        }
    }
}

/// Check that a string matches a regex pattern.
pub fn check_pattern(
    value: &serde_json::Value,
    path: &str,
    pattern: &Lazy<Regex>,
    errors: &mut Vec<ValidationError>,
) {
    if let Some(s) = value.as_str() {
        if !pattern.is_match(s) {
            errors.push(ValidationError {
                path: path.to_string(),
                message: format!("must match pattern \"{}\"", pattern.as_str()),
                keyword: "pattern",
            });
        }
    }
}

/// Check that a string matches a fancy-regex pattern (supports lookahead/lookbehind).
pub fn check_fancy_pattern(
    value: &serde_json::Value,
    path: &str,
    pattern: &Lazy<fancy_regex::Regex>,
    errors: &mut Vec<ValidationError>,
) {
    if let Some(s) = value.as_str() {
        if !pattern.is_match(s).unwrap_or(false) {
            errors.push(ValidationError {
                path: path.to_string(),
                message: format!("must match pattern \"{}\"", pattern.as_str()),
                keyword: "pattern",
            });
        }
    }
}

/// Check that a value is a boolean.
pub fn check_boolean(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) -> bool {
    if value.is_boolean() {
        true
    } else {
        errors.push(ValidationError {
            path: path.to_string(),
            message: "must be boolean".to_string(),
            keyword: "type",
        });
        false
    }
}

/// Check that a value is an integer within the given range.
pub fn check_integer(
    value: &serde_json::Value,
    path: &str,
    minimum: Option<i64>,
    maximum: Option<i64>,
    errors: &mut Vec<ValidationError>,
) -> bool {
    match value.as_i64() {
        Some(n) => {
            // Also verify it's actually an integer (not a float).
            if value.is_f64() && !value.is_i64() && !value.is_u64() {
                errors.push(ValidationError {
                    path: path.to_string(),
                    message: "must be integer".to_string(),
                    keyword: "type",
                });
                return false;
            }
            if let Some(min) = minimum {
                if n < min {
                    errors.push(ValidationError {
                        path: path.to_string(),
                        message: format!("must be >= {min}"),
                        keyword: "minimum",
                    });
                    return false;
                }
            }
            if let Some(max) = maximum {
                if n > max {
                    errors.push(ValidationError {
                        path: path.to_string(),
                        message: format!("must be <= {max}"),
                        keyword: "maximum",
                    });
                    return false;
                }
            }
            true
        }
        None => {
            // Try u64 for large positive values.
            if value.is_u64() {
                let n = value.as_u64().unwrap();
                if let Some(max) = maximum {
                    if n > max as u64 {
                        errors.push(ValidationError {
                            path: path.to_string(),
                            message: format!("must be <= {max}"),
                            keyword: "maximum",
                        });
                        return false;
                    }
                }
                return true;
            }
            errors.push(ValidationError {
                path: path.to_string(),
                message: "must be integer".to_string(),
                keyword: "type",
            });
            false
        }
    }
}

/// Check that a value is an array.
pub fn check_array(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) -> bool {
    if value.is_array() {
        true
    } else {
        errors.push(ValidationError {
            path: path.to_string(),
            message: "must be array".to_string(),
            keyword: "type",
        });
        false
    }
}

/// Check array minimum items.
pub fn check_min_items(
    value: &serde_json::Value,
    path: &str,
    min_items: usize,
    errors: &mut Vec<ValidationError>,
) {
    if let Some(arr) = value.as_array() {
        if arr.len() < min_items {
            errors.push(ValidationError {
                path: path.to_string(),
                message: format!("must NOT have fewer than {min_items} items"),
                keyword: "minItems",
            });
        }
    }
}

/// Check that a string value is one of the allowed enum values.
pub fn check_string_enum(
    value: &serde_json::Value,
    path: &str,
    allowed: &[&str],
    errors: &mut Vec<ValidationError>,
) -> bool {
    match value.as_str() {
        Some(s) if allowed.contains(&s) => true,
        Some(s) => {
            errors.push(ValidationError {
                path: path.to_string(),
                message: format!("must be equal to one of the allowed values: {allowed:?}, got \"{s}\""),
                keyword: "enum",
            });
            false
        }
        None => {
            errors.push(ValidationError {
                path: path.to_string(),
                message: "must be string".to_string(),
                keyword: "type",
            });
            false
        }
    }
}

/// Check that a value is a literal string.
pub fn check_literal(
    value: &serde_json::Value,
    path: &str,
    expected: &str,
    errors: &mut Vec<ValidationError>,
) -> bool {
    match value.as_str() {
        Some(s) if s == expected => true,
        _ => {
            errors.push(ValidationError {
                path: path.to_string(),
                message: format!("must be equal to constant \"{expected}\""),
                keyword: "const",
            });
            false
        }
    }
}

/// Check that a value is null.
pub fn is_null(value: &serde_json::Value) -> bool {
    value.is_null()
}

/// Check that an object has no additional properties beyond the allowed set.
pub fn check_no_additional_properties(
    obj: &serde_json::Map<String, serde_json::Value>,
    allowed: &[&str],
    parent_path: &str,
    errors: &mut Vec<ValidationError>,
) {
    for key in obj.keys() {
        if !allowed.contains(&key.as_str()) {
            errors.push(ValidationError {
                path: parent_path.to_string(),
                message: format!("must NOT have additional properties: '{key}'"),
                keyword: "additionalProperties",
            });
        }
    }
}

/// Check an optional field: if present and not null, run the provided checker.
/// Returns `true` if the field is absent, null, or passes the checker.
pub fn check_optional<F>(
    obj: &serde_json::Map<String, serde_json::Value>,
    field: &str,
    parent_path: &str,
    errors: &mut Vec<ValidationError>,
    checker: F,
) where
    F: FnOnce(&serde_json::Value, &str, &mut Vec<ValidationError>),
{
    if let Some(value) = obj.get(field) {
        let path = format!("{parent_path}/{field}");
        checker(value, &path, errors);
    }
}

/// Check an optional-nullable field: if present and not null, run the checker.
/// Allows the field to be absent or JSON null.
pub fn check_optional_nullable<F>(
    obj: &serde_json::Map<String, serde_json::Value>,
    field: &str,
    parent_path: &str,
    errors: &mut Vec<ValidationError>,
    checker: F,
) where
    F: FnOnce(&serde_json::Value, &str, &mut Vec<ValidationError>),
{
    if let Some(value) = obj.get(field) {
        if !value.is_null() {
            let path = format!("{parent_path}/{field}");
            checker(value, &path, errors);
        }
    }
}

// ---------------------------------------------------------------------------
// Method dispatcher
// ---------------------------------------------------------------------------

/// Validate RPC parameters for a given method name.
/// Returns a `ValidationResult` with detailed errors.
pub fn validate_params(method: &str, json: &str) -> Result<ValidationResult, ValidateParamsError> {
    let value: serde_json::Value =
        serde_json::from_str(json).map_err(ValidateParamsError::InvalidJson)?;

    let validator = match lookup_validator(method) {
        Some(v) => v,
        None => return Err(ValidateParamsError::UnknownMethod(method.to_string())),
    };

    let mut errors = Vec::new();
    validator(&value, "", &mut errors);
    Ok(ValidationResult::from_errors(errors))
}

#[derive(Debug, thiserror::Error)]
pub enum ValidateParamsError {
    #[error("invalid JSON: {0}")]
    InvalidJson(#[from] serde_json::Error),
    #[error("unknown method: {0}")]
    UnknownMethod(String),
}

/// Validator function signature: takes a value, a parent JSON pointer path,
/// and a mutable errors vector to append to.
pub type ValidatorFn = fn(&serde_json::Value, &str, &mut Vec<ValidationError>);

/// Look up the validator function for a given RPC method name.
fn lookup_validator(method: &str) -> Option<ValidatorFn> {
    // Session methods
    match method {
        "sessions.list" => Some(schemas::sessions::validate_sessions_list_params),
        "sessions.preview" => Some(schemas::sessions::validate_sessions_preview_params),
        "sessions.resolve" => Some(schemas::sessions::validate_sessions_resolve_params),
        "sessions.create" => Some(schemas::sessions::validate_sessions_create_params),
        "sessions.send" => Some(schemas::sessions::validate_sessions_send_params),
        "sessions.messages.subscribe" => {
            Some(schemas::sessions::validate_sessions_messages_subscribe_params)
        }
        "sessions.messages.unsubscribe" => {
            Some(schemas::sessions::validate_sessions_messages_unsubscribe_params)
        }
        "sessions.abort" => Some(schemas::sessions::validate_sessions_abort_params),
        "sessions.patch" => Some(schemas::sessions::validate_sessions_patch_params),
        "sessions.reset" => Some(schemas::sessions::validate_sessions_reset_params),
        "sessions.delete" => Some(schemas::sessions::validate_sessions_delete_params),
        "sessions.compact" => Some(schemas::sessions::validate_sessions_compact_params),
        "sessions.usage" => Some(schemas::sessions::validate_sessions_usage_params),

        // Device methods
        "device.pair.list" => Some(schemas::devices::validate_device_pair_list_params),
        "device.pair.approve" => Some(schemas::devices::validate_device_pair_approve_params),
        "device.pair.reject" => Some(schemas::devices::validate_device_pair_reject_params),
        "device.pair.remove" => Some(schemas::devices::validate_device_pair_remove_params),
        "device.token.rotate" => Some(schemas::devices::validate_device_token_rotate_params),
        "device.token.revoke" => Some(schemas::devices::validate_device_token_revoke_params),

        // Secrets methods
        "secrets.resolve" => Some(schemas::secrets::validate_secrets_resolve_params),
        "secrets.reload" => Some(schemas::secrets::validate_secrets_reload_params),

        // Wizard methods
        "wizard.start" => Some(schemas::wizard::validate_wizard_start_params),
        "wizard.next" => Some(schemas::wizard::validate_wizard_next_params),
        "wizard.cancel" => Some(schemas::wizard::validate_wizard_cancel_params),
        "wizard.status" => Some(schemas::wizard::validate_wizard_status_params),

        // Logs/chat methods
        "logs.tail" => Some(schemas::logs_chat::validate_logs_tail_params),
        "chat.history" => Some(schemas::logs_chat::validate_chat_history_params),
        "chat.send" => Some(schemas::logs_chat::validate_chat_send_params),
        "chat.abort" => Some(schemas::logs_chat::validate_chat_abort_params),
        "chat.inject" => Some(schemas::logs_chat::validate_chat_inject_params),

        // Config methods
        "config.get" => Some(schemas::config::validate_config_get_params),
        "config.set" => Some(schemas::config::validate_config_set_params),
        "config.apply" => Some(schemas::config::validate_config_apply_params),
        "config.patch" => Some(schemas::config::validate_config_patch_params),
        "config.schema" => Some(schemas::config::validate_config_schema_params),
        "config.schema.lookup" => Some(schemas::config::validate_config_schema_lookup_params),
        "update.run" => Some(schemas::config::validate_update_run_params),

        // Channels methods
        "channels.status" => Some(schemas::channels::validate_channels_status_params),
        "channels.logout" => Some(schemas::channels::validate_channels_logout_params),
        "talk.mode" => Some(schemas::channels::validate_talk_mode_params),
        "talk.config" => Some(schemas::channels::validate_talk_config_params),
        "weblogin.start" => Some(schemas::channels::validate_web_login_start_params),
        "weblogin.wait" => Some(schemas::channels::validate_web_login_wait_params),

        // Agent methods
        "agent.send" => Some(schemas::agent::validate_send_params),
        "agent.poll" => Some(schemas::agent::validate_poll_params),
        "agent" => Some(schemas::agent::validate_agent_params),
        "agent.identity" => Some(schemas::agent::validate_agent_identity_params),
        "agent.wait" => Some(schemas::agent::validate_agent_wait_params),
        "agent.wake" => Some(schemas::agent::validate_wake_params),

        // Agents CRUD
        "agents.list" => Some(schemas::agents_models_skills::validate_agents_list_params),
        "agents.create" => Some(schemas::agents_models_skills::validate_agents_create_params),
        "agents.update" => Some(schemas::agents_models_skills::validate_agents_update_params),
        "agents.delete" => Some(schemas::agents_models_skills::validate_agents_delete_params),
        "agents.files.list" => {
            Some(schemas::agents_models_skills::validate_agents_files_list_params)
        }
        "agents.files.get" => {
            Some(schemas::agents_models_skills::validate_agents_files_get_params)
        }
        "agents.files.set" => {
            Some(schemas::agents_models_skills::validate_agents_files_set_params)
        }
        "models.list" => Some(schemas::agents_models_skills::validate_models_list_params),
        "skills.status" => Some(schemas::agents_models_skills::validate_skills_status_params),
        "skills.bins" => Some(schemas::agents_models_skills::validate_skills_bins_params),
        "skills.install" => Some(schemas::agents_models_skills::validate_skills_install_params),
        "skills.update" => Some(schemas::agents_models_skills::validate_skills_update_params),
        "tools.catalog" => Some(schemas::agents_models_skills::validate_tools_catalog_params),

        // Node methods
        "node.pair.request" => Some(schemas::nodes::validate_node_pair_request_params),
        "node.pair.list" => Some(schemas::nodes::validate_node_pair_list_params),
        "node.pair.approve" => Some(schemas::nodes::validate_node_pair_approve_params),
        "node.pair.reject" => Some(schemas::nodes::validate_node_pair_reject_params),
        "node.pair.verify" => Some(schemas::nodes::validate_node_pair_verify_params),
        "node.rename" => Some(schemas::nodes::validate_node_rename_params),
        "node.list" => Some(schemas::nodes::validate_node_list_params),
        "node.pending.ack" => Some(schemas::nodes::validate_node_pending_ack_params),
        "node.describe" => Some(schemas::nodes::validate_node_describe_params),
        "node.invoke" => Some(schemas::nodes::validate_node_invoke_params),
        "node.invoke.result" => Some(schemas::nodes::validate_node_invoke_result_params),
        "node.event" => Some(schemas::nodes::validate_node_event_params),
        "node.pending.drain" => Some(schemas::nodes::validate_node_pending_drain_params),
        "node.pending.enqueue" => Some(schemas::nodes::validate_node_pending_enqueue_params),

        // Cron methods
        "cron.list" => Some(schemas::cron::validate_cron_list_params),
        "cron.status" => Some(schemas::cron::validate_cron_status_params),
        "cron.add" => Some(schemas::cron::validate_cron_add_params),
        "cron.update" => Some(schemas::cron::validate_cron_update_params),
        "cron.remove" => Some(schemas::cron::validate_cron_remove_params),
        "cron.run" => Some(schemas::cron::validate_cron_run_params),
        "cron.runs" => Some(schemas::cron::validate_cron_runs_params),

        // Exec approvals
        "exec.approvals.get" => {
            Some(schemas::exec_approvals::validate_exec_approvals_get_params)
        }
        "exec.approvals.set" => {
            Some(schemas::exec_approvals::validate_exec_approvals_set_params)
        }
        "exec.approval.request" => {
            Some(schemas::exec_approvals::validate_exec_approval_request_params)
        }
        "exec.approval.resolve" => {
            Some(schemas::exec_approvals::validate_exec_approval_resolve_params)
        }
        "exec.approvals.node.get" => {
            Some(schemas::exec_approvals::validate_exec_approvals_node_get_params)
        }
        "exec.approvals.node.set" => {
            Some(schemas::exec_approvals::validate_exec_approvals_node_set_params)
        }

        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_validate_params_unknown_method() {
        let result = validate_params("nonexistent.method", "{}");
        assert!(result.is_err());
        match result.unwrap_err() {
            ValidateParamsError::UnknownMethod(m) => assert_eq!(m, "nonexistent.method"),
            _ => panic!("expected UnknownMethod"),
        }
    }

    #[test]
    fn test_validate_params_invalid_json() {
        let result = validate_params("sessions.list", "{not json}");
        assert!(result.is_err());
        assert!(matches!(
            result.unwrap_err(),
            ValidateParamsError::InvalidJson(_)
        ));
    }

    #[test]
    fn test_validate_params_valid_sessions_list() {
        let result = validate_params("sessions.list", "{}").unwrap();
        assert!(result.valid, "empty object should be valid for sessions.list (all fields optional)");
    }

    #[test]
    fn test_validate_params_additional_properties() {
        let result = validate_params("sessions.list", r#"{"unknownField": true}"#).unwrap();
        assert!(!result.valid, "unknown properties should fail");
        assert_eq!(result.errors[0].keyword, "additionalProperties");
    }

    #[test]
    fn test_validation_result_serialization() {
        let result = ValidationResult::from_errors(vec![ValidationError {
            path: "/key".to_string(),
            message: "must be non-empty".to_string(),
            keyword: "minLength",
        }]);
        let json = serde_json::to_string(&result).unwrap();
        assert!(json.contains("\"valid\":false"));
        assert!(json.contains("\"minLength\""));
    }
}
