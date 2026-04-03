//! Logs/chat schema validators — mirrors `src/gateway/protocol/schema/logs-chat.ts`.

use crate::protocol::constants::CHAT_SEND_SESSION_KEY_MAX_LENGTH;
use crate::protocol::validation::*;

define_schema! {
    pub fn validate_logs_tail_params {
        [opt "cursor" => integer(Some(0), None)],
        [opt "limit" => integer(Some(1), Some(5000))],
        [opt "maxBytes" => integer(Some(1), Some(1_000_000))],
    }
}

define_schema! {
    pub fn validate_chat_history_params {
        [req "sessionKey" => non_empty_string],
        [opt "limit" => integer(Some(1), Some(1000))],
    }
}

define_schema! {
    pub fn validate_chat_send_params {
        [req "sessionKey" => non_empty_string, max_length(CHAT_SEND_SESSION_KEY_MAX_LENGTH)],
        [req "message" => string],
        [opt "thinking" => string],
        [opt "deliver" => boolean],
        [opt "attachments" => array],
        [opt "timeoutMs" => integer(Some(0), None)],
        [opt "systemInputProvenance" => custom(check_input_provenance)],
        [opt "systemProvenanceReceipt" => string],
        [req "idempotencyKey" => non_empty_string],
    }
}

/// `InputProvenanceSchema`: `{ kind: enum, ...optional string fields }`
fn check_input_provenance(v: &serde_json::Value, p: &str, e: &mut Vec<ValidationError>) {
    if require_object(v, p, e) {
        let Some(prov) = v.as_object() else {
            return;
        };
        let allowed = &[
            "kind",
            "originSessionId",
            "sourceSessionKey",
            "sourceChannel",
            "sourceTool",
        ];
        check_no_additional_properties(prov, allowed, p, e);
        if check_required(prov, "kind", p, e) {
            check_string_enum(
                &prov["kind"],
                &format!("{p}/kind"),
                &["external_user", "inter_session", "internal_system"],
                e,
            );
        }
    }
}

define_schema! {
    pub fn validate_chat_abort_params {
        [req "sessionKey" => non_empty_string],
        [opt "runId" => non_empty_string],
    }
}

define_schema! {
    pub fn validate_chat_inject_params {
        [req "sessionKey" => non_empty_string],
        [req "message" => non_empty_string],
        [opt "label" => string, max_length(100)],
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_logs_tail_valid() {
        let mut e = Vec::new();
        validate_logs_tail_params(&json!({"cursor": 0, "limit": 100}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_logs_tail_limit_too_high() {
        let mut e = Vec::new();
        validate_logs_tail_params(&json!({"limit": 10000}), "", &mut e);
        assert!(e.iter().any(|err| err.keyword == "maximum"));
    }

    #[test]
    fn test_chat_send_valid() {
        let mut e = Vec::new();
        validate_chat_send_params(
            &json!({"sessionKey": "sk", "message": "hi", "idempotencyKey": "idk1"}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }

    #[test]
    fn test_chat_inject_valid() {
        let mut e = Vec::new();
        validate_chat_inject_params(
            &json!({"sessionKey": "sk", "message": "injected"}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }
}
