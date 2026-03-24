//! Device schema validators — mirrors `src/gateway/protocol/schema/devices.ts`.

use crate::protocol::validation::*;

fn validate_empty_object(
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

fn validate_request_id_only(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["requestId"], path, errors);
    if check_required(obj, "requestId", path, errors) {
        check_non_empty_string(&obj["requestId"], &format!("{path}/requestId"), errors);
    }
}

fn validate_device_id_only(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["deviceId"], path, errors);
    if check_required(obj, "deviceId", path, errors) {
        check_non_empty_string(&obj["deviceId"], &format!("{path}/deviceId"), errors);
    }
}

pub fn validate_device_pair_list_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_empty_object(value, path, errors);
}

pub fn validate_device_pair_approve_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_request_id_only(value, path, errors);
}

pub fn validate_device_pair_reject_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_request_id_only(value, path, errors);
}

pub fn validate_device_pair_remove_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_device_id_only(value, path, errors);
}

pub fn validate_device_token_rotate_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["deviceId", "role", "scopes"], path, errors);
    if check_required(obj, "deviceId", path, errors) {
        check_non_empty_string(&obj["deviceId"], &format!("{path}/deviceId"), errors);
    }
    if check_required(obj, "role", path, errors) {
        check_non_empty_string(&obj["role"], &format!("{path}/role"), errors);
    }
    check_optional(obj, "scopes", path, errors, |v, p, e| {
        if check_array(v, p, e) {
            if let Some(arr) = v.as_array() {
                for (i, item) in arr.iter().enumerate() {
                    check_non_empty_string(item, &format!("{p}/{i}"), e);
                }
            }
        }
    });
}

pub fn validate_device_token_revoke_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["deviceId", "role"], path, errors);
    if check_required(obj, "deviceId", path, errors) {
        check_non_empty_string(&obj["deviceId"], &format!("{path}/deviceId"), errors);
    }
    if check_required(obj, "role", path, errors) {
        check_non_empty_string(&obj["role"], &format!("{path}/role"), errors);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_pair_list_empty() {
        let mut e = Vec::new();
        validate_device_pair_list_params(&json!({}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_pair_approve_valid() {
        let mut e = Vec::new();
        validate_device_pair_approve_params(&json!({"requestId": "r1"}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_pair_approve_missing() {
        let mut e = Vec::new();
        validate_device_pair_approve_params(&json!({}), "", &mut e);
        assert!(!e.is_empty());
    }

    #[test]
    fn test_token_rotate_valid() {
        let mut e = Vec::new();
        validate_device_token_rotate_params(
            &json!({"deviceId": "d1", "role": "admin", "scopes": ["read"]}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }
}
