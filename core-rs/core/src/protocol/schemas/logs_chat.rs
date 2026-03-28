//! Logs/chat schema validators — mirrors `src/gateway/protocol/schema/logs-chat.ts`.

use crate::protocol::constants::CHAT_SEND_SESSION_KEY_MAX_LENGTH;
use crate::protocol::validation::*;

pub fn validate_logs_tail_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["cursor", "limit", "maxBytes"], path, errors);
    check_optional(obj, "cursor", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
    check_optional(obj, "limit", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), Some(5000), e);
    });
    check_optional(obj, "maxBytes", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), Some(1_000_000), e);
    });
}

pub fn validate_chat_history_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["sessionKey", "limit"], path, errors);
    if check_required(obj, "sessionKey", path, errors) {
        check_non_empty_string(&obj["sessionKey"], &format!("{path}/sessionKey"), errors);
    }
    check_optional(obj, "limit", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), Some(1000), e);
    });
}

pub fn validate_chat_send_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    let allowed = &[
        "sessionKey",
        "message",
        "thinking",
        "deliver",
        "attachments",
        "timeoutMs",
        "systemInputProvenance",
        "systemProvenanceReceipt",
        "idempotencyKey",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    if check_required(obj, "sessionKey", path, errors) {
        let sk = &obj["sessionKey"];
        let sk_path = format!("{path}/sessionKey");
        check_non_empty_string(sk, &sk_path, errors);
        check_max_length(sk, &sk_path, CHAT_SEND_SESSION_KEY_MAX_LENGTH, errors);
    }
    if check_required(obj, "message", path, errors) {
        check_string(&obj["message"], &format!("{path}/message"), errors);
    }
    check_optional(obj, "thinking", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "deliver", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "attachments", path, errors, |v, p, e| {
        check_array(v, p, e);
    });
    check_optional(obj, "timeoutMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
    // systemInputProvenance: InputProvenanceSchema — validated as object with known fields
    check_optional(obj, "systemInputProvenance", path, errors, |v, p, e| {
        if require_object(v, p, e) {
            let Some(prov) = v.as_object() else { return; };
            let prov_allowed = &[
                "kind",
                "originSessionId",
                "sourceSessionKey",
                "sourceChannel",
                "sourceTool",
            ];
            check_no_additional_properties(prov, prov_allowed, p, e);
            if check_required(prov, "kind", p, e) {
                check_string_enum(
                    &prov["kind"],
                    &format!("{p}/kind"),
                    &["external_user", "inter_session", "internal_system"],
                    e,
                );
            }
        }
    });
    check_optional(obj, "systemProvenanceReceipt", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    if check_required(obj, "idempotencyKey", path, errors) {
        check_non_empty_string(
            &obj["idempotencyKey"],
            &format!("{path}/idempotencyKey"),
            errors,
        );
    }
}

pub fn validate_chat_abort_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["sessionKey", "runId"], path, errors);
    if check_required(obj, "sessionKey", path, errors) {
        check_non_empty_string(&obj["sessionKey"], &format!("{path}/sessionKey"), errors);
    }
    check_optional(obj, "runId", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
}

pub fn validate_chat_inject_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["sessionKey", "message", "label"], path, errors);
    if check_required(obj, "sessionKey", path, errors) {
        check_non_empty_string(&obj["sessionKey"], &format!("{path}/sessionKey"), errors);
    }
    if check_required(obj, "message", path, errors) {
        check_non_empty_string(&obj["message"], &format!("{path}/message"), errors);
    }
    check_optional(obj, "label", path, errors, |v, p, e| {
        check_string(v, p, e);
        check_max_length(v, p, 100, e);
    });
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
