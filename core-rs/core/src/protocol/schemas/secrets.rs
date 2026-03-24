//! Secrets schema validators — mirrors `src/gateway/protocol/schema/secrets.ts`.

use crate::protocol::validation::*;

pub fn validate_secrets_reload_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &[], path, errors);
}

pub fn validate_secrets_resolve_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["commandName", "targetIds"], path, errors);
    if check_required(obj, "commandName", path, errors) {
        check_non_empty_string(&obj["commandName"], &format!("{path}/commandName"), errors);
    }
    if check_required(obj, "targetIds", path, errors) {
        let tp = format!("{path}/targetIds");
        if check_array(&obj["targetIds"], &tp, errors) {
            if let Some(arr) = obj["targetIds"].as_array() {
                for (i, item) in arr.iter().enumerate() {
                    check_non_empty_string(item, &format!("{tp}/{i}"), errors);
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_reload_valid() {
        let mut e = Vec::new();
        validate_secrets_reload_params(&json!({}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_resolve_valid() {
        let mut e = Vec::new();
        validate_secrets_resolve_params(
            &json!({"commandName": "cmd", "targetIds": ["a", "b"]}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }

    #[test]
    fn test_resolve_missing() {
        let mut e = Vec::new();
        validate_secrets_resolve_params(&json!({}), "", &mut e);
        assert!(e.len() >= 2);
    }
}
