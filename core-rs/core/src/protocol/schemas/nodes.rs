//! Node schema validators — mirrors `src/gateway/protocol/schema/nodes.ts`.

use crate::protocol::validation::*;

pub fn validate_node_pair_request_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    let allowed = &[
        "nodeId",
        "displayName",
        "platform",
        "version",
        "coreVersion",
        "uiVersion",
        "deviceFamily",
        "modelIdentifier",
        "caps",
        "commands",
        "remoteIp",
        "silent",
    ];
    check_no_additional_properties(obj, allowed, path, errors);
    if check_required(obj, "nodeId", path, errors) {
        check_non_empty_string(&obj["nodeId"], &format!("{path}/nodeId"), errors);
    }
    for f in &[
        "displayName",
        "platform",
        "version",
        "coreVersion",
        "uiVersion",
        "deviceFamily",
        "modelIdentifier",
        "remoteIp",
    ] {
        check_optional(obj, f, path, errors, |v, p, e| {
            check_non_empty_string(v, p, e);
        });
    }
    for f in &["caps", "commands"] {
        check_optional(obj, f, path, errors, |v, p, e| {
            if check_array(v, p, e) {
                if let Some(arr) = v.as_array() {
                    for (i, item) in arr.iter().enumerate() {
                        check_non_empty_string(item, &format!("{p}/{i}"), e);
                    }
                }
            }
        });
    }
    check_optional(obj, "silent", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
}

fn validate_empty(value: &serde_json::Value, path: &str, errors: &mut Vec<ValidationError>) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &[], path, errors);
}

fn validate_request_id(value: &serde_json::Value, path: &str, errors: &mut Vec<ValidationError>) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["requestId"], path, errors);
    if check_required(obj, "requestId", path, errors) {
        check_non_empty_string(&obj["requestId"], &format!("{path}/requestId"), errors);
    }
}

pub fn validate_node_pair_list_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_empty(value, path, errors);
}

pub fn validate_node_pair_approve_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_request_id(value, path, errors);
}

pub fn validate_node_pair_reject_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_request_id(value, path, errors);
}

pub fn validate_node_pair_verify_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["nodeId", "token"], path, errors);
    if check_required(obj, "nodeId", path, errors) {
        check_non_empty_string(&obj["nodeId"], &format!("{path}/nodeId"), errors);
    }
    if check_required(obj, "token", path, errors) {
        check_non_empty_string(&obj["token"], &format!("{path}/token"), errors);
    }
}

pub fn validate_node_rename_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["nodeId", "displayName"], path, errors);
    if check_required(obj, "nodeId", path, errors) {
        check_non_empty_string(&obj["nodeId"], &format!("{path}/nodeId"), errors);
    }
    if check_required(obj, "displayName", path, errors) {
        check_non_empty_string(&obj["displayName"], &format!("{path}/displayName"), errors);
    }
}

pub fn validate_node_list_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_empty(value, path, errors);
}

pub fn validate_node_pending_ack_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["ids"], path, errors);
    if check_required(obj, "ids", path, errors) {
        let ids_p = format!("{path}/ids");
        if check_array(&obj["ids"], &ids_p, errors) {
            check_min_items(&obj["ids"], &ids_p, 1, errors);
            if let Some(arr) = obj["ids"].as_array() {
                for (i, item) in arr.iter().enumerate() {
                    check_non_empty_string(item, &format!("{ids_p}/{i}"), errors);
                }
            }
        }
    }
}

pub fn validate_node_describe_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["nodeId"], path, errors);
    if check_required(obj, "nodeId", path, errors) {
        check_non_empty_string(&obj["nodeId"], &format!("{path}/nodeId"), errors);
    }
}

pub fn validate_node_invoke_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    let allowed = &["nodeId", "command", "params", "timeoutMs", "idempotencyKey"];
    check_no_additional_properties(obj, allowed, path, errors);
    if check_required(obj, "nodeId", path, errors) {
        check_non_empty_string(&obj["nodeId"], &format!("{path}/nodeId"), errors);
    }
    if check_required(obj, "command", path, errors) {
        check_non_empty_string(&obj["command"], &format!("{path}/command"), errors);
    }
    // params is Type.Unknown()
    check_optional(obj, "timeoutMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
    if check_required(obj, "idempotencyKey", path, errors) {
        check_non_empty_string(
            &obj["idempotencyKey"],
            &format!("{path}/idempotencyKey"),
            errors,
        );
    }
}

pub fn validate_node_invoke_result_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    let allowed = &["id", "nodeId", "ok", "payload", "payloadJSON", "error"];
    check_no_additional_properties(obj, allowed, path, errors);
    if check_required(obj, "id", path, errors) {
        check_non_empty_string(&obj["id"], &format!("{path}/id"), errors);
    }
    if check_required(obj, "nodeId", path, errors) {
        check_non_empty_string(&obj["nodeId"], &format!("{path}/nodeId"), errors);
    }
    if check_required(obj, "ok", path, errors) {
        check_boolean(&obj["ok"], &format!("{path}/ok"), errors);
    }
    // payload is Type.Unknown()
    check_optional(obj, "payloadJSON", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "error", path, errors, |v, p, e| {
        if require_object(v, p, e) {
            let Some(err_obj) = v.as_object() else { return; };
            check_no_additional_properties(err_obj, &["code", "message"], p, e);
            check_optional(err_obj, "code", p, e, |v2, p2, e2| {
                check_non_empty_string(v2, p2, e2);
            });
            check_optional(err_obj, "message", p, e, |v2, p2, e2| {
                check_non_empty_string(v2, p2, e2);
            });
        }
    });
}

pub fn validate_node_event_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["event", "payload", "payloadJSON"], path, errors);
    if check_required(obj, "event", path, errors) {
        check_non_empty_string(&obj["event"], &format!("{path}/event"), errors);
    }
    // payload is Type.Unknown()
    check_optional(obj, "payloadJSON", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
}

pub fn validate_node_pending_drain_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["maxItems"], path, errors);
    check_optional(obj, "maxItems", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), Some(10), e);
    });
}

pub fn validate_node_pending_enqueue_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    let allowed = &["nodeId", "type", "priority", "expiresInMs", "wake"];
    check_no_additional_properties(obj, allowed, path, errors);
    if check_required(obj, "nodeId", path, errors) {
        check_non_empty_string(&obj["nodeId"], &format!("{path}/nodeId"), errors);
    }
    if check_required(obj, "type", path, errors) {
        check_string_enum(
            &obj["type"],
            &format!("{path}/type"),
            &["status.request", "location.request"],
            errors,
        );
    }
    check_optional(obj, "priority", path, errors, |v, p, e| {
        check_string_enum(v, p, &["normal", "high"], e);
    });
    check_optional(obj, "expiresInMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(1_000), Some(86_400_000), e);
    });
    check_optional(obj, "wake", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_node_invoke_valid() {
        let mut e = Vec::new();
        validate_node_invoke_params(
            &json!({"nodeId": "n1", "command": "status", "idempotencyKey": "k1"}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }

    #[test]
    fn test_node_enqueue_valid() {
        let mut e = Vec::new();
        validate_node_pending_enqueue_params(
            &json!({"nodeId": "n1", "type": "status.request"}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }
}
