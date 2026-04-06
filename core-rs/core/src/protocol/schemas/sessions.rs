//! Session schema validators — mirrors `src/gateway/protocol/schema/sessions.ts`.

use crate::protocol::constants::SESSION_LABEL_MAX_LENGTH;
use crate::protocol::validation::*;

// ---------------------------------------------------------------------------
// sessions.list
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_sessions_list_params {
        [opt "limit" => integer(Some(1), None)],
        [opt "activeMinutes" => integer(Some(1), None)],
        [opt "includeGlobal" => boolean],
        [opt "includeUnknown" => boolean],
        [opt "includeDerivedTitles" => boolean],
        [opt "includeLastMessage" => boolean],
        [opt "label" => non_empty_string, max_length(SESSION_LABEL_MAX_LENGTH)],
        [opt "spawnedBy" => non_empty_string],
        [opt "agentId" => non_empty_string],
        [opt "search" => string],
    }
}

// ---------------------------------------------------------------------------
// sessions.preview
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_sessions_preview_params {
        [req "keys" => custom(check_non_empty_string_array_min1)],
        [opt "limit" => integer(Some(1), None)],
        [opt "maxChars" => integer(Some(20), None)],
    }
}

// ---------------------------------------------------------------------------
// sessions.resolve
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_sessions_resolve_params {
        [opt "key" => non_empty_string],
        [opt "sessionId" => non_empty_string],
        [opt "label" => non_empty_string, max_length(SESSION_LABEL_MAX_LENGTH)],
        [opt "agentId" => non_empty_string],
        [opt "spawnedBy" => non_empty_string],
        [opt "includeGlobal" => boolean],
        [opt "includeUnknown" => boolean],
    }
}

// ---------------------------------------------------------------------------
// sessions.create
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_sessions_create_params {
        [opt "key" => non_empty_string],
        [opt "agentId" => non_empty_string],
        [opt "label" => non_empty_string, max_length(SESSION_LABEL_MAX_LENGTH)],
        [opt "model" => non_empty_string],
        [opt "parentSessionKey" => non_empty_string],
        [opt "task" => string],
        [opt "message" => string],
    }
}

// ---------------------------------------------------------------------------
// sessions.send
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_sessions_send_params {
        [req "key" => non_empty_string],
        [req "message" => string],
        [opt "thinking" => string],
        [opt "attachments" => array],
        [opt "timeoutMs" => integer(Some(0), None)],
        [opt "idempotencyKey" => non_empty_string],
    }
}

// ---------------------------------------------------------------------------
// sessions.messages.subscribe / unsubscribe
// ---------------------------------------------------------------------------

define_schema! {
    fn validate_session_key_only {
        [req "key" => non_empty_string],
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

define_schema! {
    pub fn validate_sessions_abort_params {
        [req "key" => non_empty_string],
        [opt "runId" => non_empty_string],
    }
}

// ---------------------------------------------------------------------------
// sessions.patch
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_sessions_patch_params {
        [req "key" => non_empty_string],
        [opt_null "label" => non_empty_string, max_length(SESSION_LABEL_MAX_LENGTH)],
        [opt_null "thinkingLevel" => non_empty_string],
        [opt_null "fastMode" => boolean],
        [opt_null "verboseLevel" => non_empty_string],
        [opt_null "reasoningLevel" => non_empty_string],
        [opt_null "responseUsage" => string_enum["off", "tokens", "full", "on"]],
        [opt_null "elevatedLevel" => non_empty_string],
        [opt_null "execHost" => non_empty_string],
        [opt_null "execSecurity" => non_empty_string],
        [opt_null "execAsk" => non_empty_string],
        [opt_null "execNode" => non_empty_string],
        [opt_null "model" => non_empty_string],
        [opt_null "spawnedBy" => non_empty_string],
        [opt_null "spawnedWorkspaceDir" => non_empty_string],
        [opt_null "spawnDepth" => integer(Some(0), None)],
        [opt_null "subagentRole" => string_enum["orchestrator", "leaf"]],
        [opt_null "subagentControlScope" => string_enum["children", "none"]],
        [opt_null "sendPolicy" => string_enum["allow", "deny"]],
        [opt_null "groupActivation" => string_enum["mention", "always"]],
    }
}

// ---------------------------------------------------------------------------
// sessions.reset
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_sessions_reset_params {
        [req "key" => non_empty_string],
        [opt "reason" => string_enum["new", "reset"]],
    }
}

// ---------------------------------------------------------------------------
// sessions.delete
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_sessions_delete_params {
        [req "key" => non_empty_string],
        [opt "deleteTranscript" => boolean],
        [opt "emitLifecycleHooks" => boolean],
    }
}

// ---------------------------------------------------------------------------
// sessions.compact
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_sessions_compact_params {
        [req "key" => non_empty_string],
        [opt "maxLines" => integer(Some(1), None)],
    }
}

// ---------------------------------------------------------------------------
// sessions.usage
// ---------------------------------------------------------------------------

define_schema! {
    pub fn validate_sessions_usage_params {
        [opt "key" => non_empty_string],
        [opt "startDate" => custom(check_date_string)],
        [opt "endDate" => custom(check_date_string)],
        [opt "mode" => string_enum["utc", "gateway", "specific"]],
        [opt "utcOffset" => custom(check_utc_offset)],
        [opt "limit" => integer(Some(1), None)],
        [opt "includeContextWeight" => boolean],
    }
}

fn check_date_string(v: &serde_json::Value, p: &str, e: &mut Vec<ValidationError>) {
    #[allow(clippy::expect_used)]
    static DATE_RE: std::sync::LazyLock<regex::Regex> = std::sync::LazyLock::new(|| {
        regex::Regex::new(r"^\d{4}-\d{2}-\d{2}$").expect("valid regex")
    });
    if check_string(v, p, e) {
        check_pattern(v, p, &DATE_RE, e);
    }
}

fn check_utc_offset(v: &serde_json::Value, p: &str, e: &mut Vec<ValidationError>) {
    #[allow(clippy::expect_used)]
    static UTC_OFFSET_RE: std::sync::LazyLock<regex::Regex> = std::sync::LazyLock::new(|| {
        regex::Regex::new(r"^UTC[+-]\d{1,2}(?::[0-5]\d)?$").expect("valid regex")
    });
    if check_string(v, p, e) {
        check_pattern(v, p, &UTC_OFFSET_RE, e);
    }
}
