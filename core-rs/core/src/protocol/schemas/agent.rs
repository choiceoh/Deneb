//! Agent schema validators — mirrors `src/gateway/protocol/schema/agent.ts`.

use crate::protocol::constants::SESSION_LABEL_MAX_LENGTH;
use crate::protocol::validation::*;

define_schema! {
    pub fn validate_send_params {
        [req "to" => non_empty_string],
        [opt "message" => string],
        [opt "mediaUrl" => string],
        [opt "mediaUrls" => custom(check_string_array)],
        [opt "channel" => string],
        [opt "accountId" => string],
        [opt "agentId" => string],
        [opt "threadId" => string],
        [opt "sessionKey" => string],
        [req "idempotencyKey" => non_empty_string],
    }
}

define_schema! {
    pub fn validate_poll_params {
        [req "to" => non_empty_string],
        [req "question" => non_empty_string],
        [req "options" => custom(check_poll_options)],
        [opt "maxSelections" => integer(Some(1), Some(12))],
        [opt "durationSeconds" => integer(Some(1), Some(604_800))],
        [opt "durationHours" => integer(Some(1), None)],
        [opt "silent" => boolean],
        [opt "isAnonymous" => boolean],
        [opt "threadId" => string],
        [opt "channel" => string],
        [opt "accountId" => string],
        [req "idempotencyKey" => non_empty_string],
    }
}

/// Poll options: array of 2–12 non-empty strings.
fn check_poll_options(v: &serde_json::Value, p: &str, e: &mut Vec<ValidationError>) {
    if check_array(v, p, e) {
        check_min_items(v, p, 2, e);
        if let Some(arr) = v.as_array() {
            if arr.len() > 12 {
                e.push(ValidationError {
                    path: p.to_string(),
                    message: "must NOT have more than 12 items".to_string(),
                    keyword: "maxItems",
                });
            }
            for (i, item) in arr.iter().enumerate() {
                check_non_empty_string(item, &format!("{p}/{i}"), e);
            }
        }
    }
}

define_schema! {
    pub fn validate_agent_params {
        [req "message" => non_empty_string],
        [opt "agentId" => non_empty_string],
        [opt "provider" => string],
        [opt "model" => string],
        [opt "to" => string],
        [opt "replyTo" => string],
        [opt "sessionId" => string],
        [opt "sessionKey" => string],
        [opt "thinking" => string],
        [opt "deliver" => boolean],
        [opt "attachments" => array],
        [opt "channel" => string],
        [opt "replyChannel" => string],
        [opt "accountId" => string],
        [opt "replyAccountId" => string],
        [opt "threadId" => string],
        [opt "groupId" => string],
        [opt "groupChannel" => string],
        [opt "groupSpace" => string],
        [opt "timeout" => integer(Some(0), None)],
        [opt "bestEffortDeliver" => boolean],
        [opt "lane" => string],
        [opt "extraSystemPrompt" => string],
        [opt "internalEvents" => array],
        [opt "inputProvenance" => any],
        [req "idempotencyKey" => non_empty_string],
        [opt "label" => non_empty_string, max_length(SESSION_LABEL_MAX_LENGTH)],
    }
}

define_schema! {
    pub fn validate_agent_identity_params {
        [opt "agentId" => non_empty_string],
        [opt "sessionKey" => string],
    }
}

define_schema! {
    pub fn validate_agent_wait_params {
        [req "runId" => non_empty_string],
        [opt "timeoutMs" => integer(Some(0), None)],
    }
}

define_schema! {
    pub fn validate_wake_params {
        [req "mode" => string_enum["now", "next-heartbeat"]],
        [req "text" => non_empty_string],
    }
}
