//! Agent schema validators — mirrors `src/gateway/protocol/schema/agent.ts`.

use crate::protocol::constants::SESSION_LABEL_MAX_LENGTH;
use crate::protocol::validation::*;

pub fn validate_send_params(
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
        "to",
        "message",
        "mediaUrl",
        "mediaUrls",
        "channel",
        "accountId",
        "agentId",
        "threadId",
        "sessionKey",
        "idempotencyKey",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    if check_required(obj, "to", path, errors) {
        check_non_empty_string(&obj["to"], &format!("{path}/to"), errors);
    }
    check_optional(obj, "message", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "mediaUrl", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "mediaUrls", path, errors, |v, p, e| {
        if check_array(v, p, e) {
            if let Some(arr) = v.as_array() {
                for (i, item) in arr.iter().enumerate() {
                    check_string(item, &format!("{p}/{i}"), e);
                }
            }
        }
    });
    check_optional(obj, "channel", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "accountId", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "agentId", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "threadId", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "sessionKey", path, errors, |v, p, e| {
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

pub fn validate_poll_params(
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
        "to",
        "question",
        "options",
        "maxSelections",
        "durationSeconds",
        "durationHours",
        "silent",
        "isAnonymous",
        "threadId",
        "channel",
        "accountId",
        "idempotencyKey",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    if check_required(obj, "to", path, errors) {
        check_non_empty_string(&obj["to"], &format!("{path}/to"), errors);
    }
    if check_required(obj, "question", path, errors) {
        check_non_empty_string(&obj["question"], &format!("{path}/question"), errors);
    }
    if check_required(obj, "options", path, errors) {
        let op = format!("{path}/options");
        let opts = &obj["options"];
        if check_array(opts, &op, errors) {
            check_min_items(opts, &op, 2, errors);
            if let Some(arr) = opts.as_array() {
                if arr.len() > 12 {
                    errors.push(ValidationError {
                        path: op.clone(),
                        message: "must NOT have more than 12 items".to_string(),
                        keyword: "maxItems",
                    });
                }
                for (i, item) in arr.iter().enumerate() {
                    check_non_empty_string(item, &format!("{op}/{i}"), errors);
                }
            }
        }
    }
    check_optional(obj, "maxSelections", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), Some(12), e);
    });
    check_optional(obj, "durationSeconds", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), Some(604_800), e);
    });
    check_optional(obj, "durationHours", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), None, e);
    });
    check_optional(obj, "silent", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "isAnonymous", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "threadId", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "channel", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "accountId", path, errors, |v, p, e| {
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

pub fn validate_agent_params(
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
        "message",
        "agentId",
        "provider",
        "model",
        "to",
        "replyTo",
        "sessionId",
        "sessionKey",
        "thinking",
        "deliver",
        "attachments",
        "channel",
        "replyChannel",
        "accountId",
        "replyAccountId",
        "threadId",
        "groupId",
        "groupChannel",
        "groupSpace",
        "timeout",
        "bestEffortDeliver",
        "lane",
        "extraSystemPrompt",
        "internalEvents",
        "inputProvenance",
        "idempotencyKey",
        "label",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    if check_required(obj, "message", path, errors) {
        check_non_empty_string(&obj["message"], &format!("{path}/message"), errors);
    }
    check_optional(obj, "agentId", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    // String-typed optional fields (no minLength constraint).
    for field in &[
        "provider",
        "model",
        "to",
        "replyTo",
        "sessionId",
        "sessionKey",
        "thinking",
        "channel",
        "replyChannel",
        "accountId",
        "replyAccountId",
        "threadId",
        "groupId",
        "groupChannel",
        "groupSpace",
        "lane",
        "extraSystemPrompt",
    ] {
        check_optional(obj, field, path, errors, |v, p, e| {
            check_string(v, p, e);
        });
    }
    check_optional(obj, "deliver", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "bestEffortDeliver", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "attachments", path, errors, |v, p, e| {
        check_array(v, p, e);
    });
    check_optional(obj, "timeout", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
    // internalEvents and inputProvenance are complex — accept as Unknown for now.
    // A full implementation would validate their nested structures.
    check_optional(obj, "internalEvents", path, errors, |v, p, e| {
        check_array(v, p, e);
    });
    if check_required(obj, "idempotencyKey", path, errors) {
        check_non_empty_string(
            &obj["idempotencyKey"],
            &format!("{path}/idempotencyKey"),
            errors,
        );
    }
    check_optional(obj, "label", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
        check_max_length(v, p, SESSION_LABEL_MAX_LENGTH, e);
    });
}

pub fn validate_agent_identity_params(
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
    check_no_additional_properties(obj, &["agentId", "sessionKey"], path, errors);
    check_optional(obj, "agentId", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "sessionKey", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
}

pub fn validate_agent_wait_params(
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
    check_no_additional_properties(obj, &["runId", "timeoutMs"], path, errors);
    if check_required(obj, "runId", path, errors) {
        check_non_empty_string(&obj["runId"], &format!("{path}/runId"), errors);
    }
    check_optional(obj, "timeoutMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
}

pub fn validate_wake_params(
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
    check_no_additional_properties(obj, &["mode", "text"], path, errors);
    if check_required(obj, "mode", path, errors) {
        check_string_enum(
            &obj["mode"],
            &format!("{path}/mode"),
            &["now", "next-heartbeat"],
            errors,
        );
    }
    if check_required(obj, "text", path, errors) {
        check_non_empty_string(&obj["text"], &format!("{path}/text"), errors);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_send_params_valid() {
        let mut e = Vec::new();
        validate_send_params(
            &json!({"to": "user1", "message": "hi", "idempotencyKey": "k1"}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }

    #[test]
    fn test_wake_params_valid() {
        let mut e = Vec::new();
        validate_wake_params(&json!({"mode": "now", "text": "wake up"}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_wake_params_invalid_mode() {
        let mut e = Vec::new();
        validate_wake_params(&json!({"mode": "invalid", "text": "t"}), "", &mut e);
        assert!(e.iter().any(|err| err.keyword == "enum"));
    }
}
