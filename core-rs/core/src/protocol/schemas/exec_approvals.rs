//! Exec approvals validators — mirrors `src/gateway/protocol/schema/exec-approvals.ts`.

use crate::protocol::validation::*;

define_schema! { pub fn validate_exec_approvals_get_params {} }

define_schema! {
    pub fn validate_exec_approvals_set_params {
        [req "file" => object],
        [opt "baseHash" => non_empty_string],
    }
}

define_schema! {
    pub fn validate_exec_approval_request_params {
        [opt "id" => non_empty_string],
        [opt "command" => non_empty_string],
        [opt "commandArgv" => custom(check_string_array)],
        [opt "systemRunPlan" => any],
        [opt "env" => any],
        [opt_null "cwd" => string],
        [opt_null "nodeId" => string],
        [opt_null "host" => string],
        [opt_null "security" => string],
        [opt_null "ask" => string],
        [opt_null "agentId" => string],
        [opt_null "resolvedPath" => string],
        [opt_null "sessionKey" => string],
        [opt_null "turnSourceChannel" => string],
        [opt_null "turnSourceTo" => string],
        [opt_null "turnSourceAccountId" => string],
        [opt_null "turnSourceThreadId" => custom(check_string_or_number)],
        [opt "timeoutMs" => integer(Some(1), None)],
        [opt "twoPhase" => boolean],
    }
}

/// Accept string or number (e.g., thread IDs).
fn check_string_or_number(v: &serde_json::Value, p: &str, e: &mut Vec<ValidationError>) {
    if !v.is_string() && !v.is_number() {
        e.push(ValidationError {
            path: p.to_string(),
            message: "must be string or number".to_string(),
            keyword: "type",
        });
    }
}

define_schema! {
    pub fn validate_exec_approval_resolve_params {
        [req "id" => non_empty_string],
        [req "decision" => non_empty_string],
    }
}

define_schema! {
    pub fn validate_exec_approvals_node_get_params {
        [req "nodeId" => non_empty_string],
    }
}

define_schema! {
    pub fn validate_exec_approvals_node_set_params {
        [req "nodeId" => non_empty_string],
        [req "file" => object],
        [opt "baseHash" => non_empty_string],
    }
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
