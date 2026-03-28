//! Exec approvals validators — mirrors `src/gateway/protocol/schema/exec-approvals.ts`.

use crate::protocol::validation::*;

pub fn validate_exec_approvals_get_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &[], path, errors);
}

pub fn validate_exec_approvals_set_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["file", "baseHash"], path, errors);
    if check_required(obj, "file", path, errors) {
        // ExecApprovalsFileSchema is complex; validate as object.
        require_object(&obj["file"], &format!("{path}/file"), errors);
    }
    check_optional(obj, "baseHash", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
}

pub fn validate_exec_approval_request_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    let allowed = &[
        "id",
        "command",
        "commandArgv",
        "systemRunPlan",
        "env",
        "cwd",
        "nodeId",
        "host",
        "security",
        "ask",
        "agentId",
        "resolvedPath",
        "sessionKey",
        "turnSourceChannel",
        "turnSourceTo",
        "turnSourceAccountId",
        "turnSourceThreadId",
        "timeoutMs",
        "twoPhase",
    ];
    check_no_additional_properties(obj, allowed, path, errors);

    check_optional(obj, "id", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "command", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "commandArgv", path, errors, |v, p, e| {
        if check_array(v, p, e) {
            if let Some(arr) = v.as_array() {
                for (i, item) in arr.iter().enumerate() {
                    check_string(item, &format!("{p}/{i}"), e);
                }
            }
        }
    });
    // systemRunPlan, env are complex — accept as objects.
    check_optional(obj, "timeoutMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), None, e);
    });
    check_optional(obj, "twoPhase", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    // Nullable string fields
    for f in &[
        "cwd",
        "nodeId",
        "host",
        "security",
        "ask",
        "agentId",
        "resolvedPath",
        "sessionKey",
        "turnSourceChannel",
        "turnSourceTo",
        "turnSourceAccountId",
    ] {
        check_optional_nullable(obj, f, path, errors, |v, p, e| {
            check_string(v, p, e);
        });
    }
    // turnSourceThreadId: string | number | null
    check_optional_nullable(obj, "turnSourceThreadId", path, errors, |v, p, e| {
        if !v.is_string() && !v.is_number() {
            e.push(ValidationError {
                path: p.to_string(),
                message: "must be string or number".to_string(),
                keyword: "type",
            });
        }
    });
}

pub fn validate_exec_approval_resolve_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["id", "decision"], path, errors);
    if check_required(obj, "id", path, errors) {
        check_non_empty_string(&obj["id"], &format!("{path}/id"), errors);
    }
    if check_required(obj, "decision", path, errors) {
        check_non_empty_string(&obj["decision"], &format!("{path}/decision"), errors);
    }
}

pub fn validate_exec_approvals_node_get_params(
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

pub fn validate_exec_approvals_node_set_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let Some(obj) = value.as_object() else { return; };
    check_no_additional_properties(obj, &["nodeId", "file", "baseHash"], path, errors);
    if check_required(obj, "nodeId", path, errors) {
        check_non_empty_string(&obj["nodeId"], &format!("{path}/nodeId"), errors);
    }
    if check_required(obj, "file", path, errors) {
        require_object(&obj["file"], &format!("{path}/file"), errors);
    }
    check_optional(obj, "baseHash", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_resolve_valid() {
        let mut e = Vec::new();
        validate_exec_approval_resolve_params(
            &json!({"id": "req-1", "decision": "allow"}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }

    #[test]
    fn test_node_get_valid() {
        let mut e = Vec::new();
        validate_exec_approvals_node_get_params(&json!({"nodeId": "n1"}), "", &mut e);
        assert!(e.is_empty());
    }
}
