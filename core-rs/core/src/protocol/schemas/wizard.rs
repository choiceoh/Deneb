//! Wizard schema validators — mirrors `src/gateway/protocol/schema/wizard.ts`.

use crate::protocol::validation::*;

define_schema! {
    pub fn validate_wizard_start_params {
        [opt "mode" => string_enum["local", "remote"]],
        [opt "workspace" => string],
    }
}

define_schema! {
    pub fn validate_wizard_next_params {
        [req "sessionId" => non_empty_string],
        [opt "answer" => custom(check_wizard_answer)],
    }
}

/// `WizardAnswerSchema`: `{ stepId: NonEmptyString, value?: Unknown }`
fn check_wizard_answer(v: &serde_json::Value, p: &str, e: &mut Vec<ValidationError>) {
    if !require_object(v, p, e) {
        return;
    }
    let Some(obj) = v.as_object() else {
        return;
    };
    check_no_additional_properties(obj, &["stepId", "value"], p, e);
    if check_required(obj, "stepId", p, e) {
        check_non_empty_string(&obj["stepId"], &format!("{p}/stepId"), e);
    }
    // value is Type.Unknown() — no validation.
}

define_schema! {
    fn validate_session_id_only {
        [req "sessionId" => non_empty_string],
    }
}

pub fn validate_wizard_cancel_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_session_id_only(value, path, errors);
}

pub fn validate_wizard_status_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_session_id_only(value, path, errors);
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_start_valid() {
        let mut e = Vec::new();
        validate_wizard_start_params(&json!({}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_start_with_mode() {
        let mut e = Vec::new();
        validate_wizard_start_params(&json!({"mode": "local"}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_next_valid() {
        let mut e = Vec::new();
        validate_wizard_next_params(
            &json!({"sessionId": "s1", "answer": {"stepId": "step1"}}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }

    #[test]
    fn test_cancel_valid() {
        let mut e = Vec::new();
        validate_wizard_cancel_params(&json!({"sessionId": "s1"}), "", &mut e);
        assert!(e.is_empty());
    }
}
