//! Wizard schema validators — mirrors `src/gateway/protocol/schema/wizard.ts`.

use crate::protocol::validation::*;

pub fn validate_wizard_start_params(
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
    check_no_additional_properties(obj, &["mode", "workspace"], path, errors);
    check_optional(obj, "mode", path, errors, |v, p, e| {
        check_string_enum(v, p, &["local", "remote"], e);
    });
    check_optional(obj, "workspace", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
}

pub fn validate_wizard_next_params(
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
    check_no_additional_properties(obj, &["sessionId", "answer"], path, errors);
    if check_required(obj, "sessionId", path, errors) {
        check_non_empty_string(&obj["sessionId"], &format!("{path}/sessionId"), errors);
    }
    check_optional(obj, "answer", path, errors, |v, p, e| {
        // WizardAnswerSchema: { stepId: NonEmptyString, value?: Unknown }
        if !require_object(v, p, e) {
            return;
        }
        let Some(answer_obj) = v.as_object() else {
            return;
        };
        check_no_additional_properties(answer_obj, &["stepId", "value"], p, e);
        if check_required(answer_obj, "stepId", p, e) {
            check_non_empty_string(&answer_obj["stepId"], &format!("{p}/stepId"), e);
        }
        // value is Type.Unknown() — no validation.
    });
}

fn validate_session_id_only(
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
    check_no_additional_properties(obj, &["sessionId"], path, errors);
    if check_required(obj, "sessionId", path, errors) {
        check_non_empty_string(&obj["sessionId"], &format!("{path}/sessionId"), errors);
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
