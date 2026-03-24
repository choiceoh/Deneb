//! Channel schema validators — mirrors `src/gateway/protocol/schema/channels.ts`.

use crate::protocol::validation::*;

pub fn validate_talk_mode_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["enabled", "phase"], path, errors);
    if check_required(obj, "enabled", path, errors) {
        check_boolean(&obj["enabled"], &format!("{path}/enabled"), errors);
    }
    check_optional(obj, "phase", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
}

pub fn validate_talk_config_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["includeSecrets"], path, errors);
    check_optional(obj, "includeSecrets", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
}

pub fn validate_channels_status_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["probe", "timeoutMs"], path, errors);
    check_optional(obj, "probe", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "timeoutMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
}

pub fn validate_channels_logout_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["channel", "accountId"], path, errors);
    if check_required(obj, "channel", path, errors) {
        check_non_empty_string(&obj["channel"], &format!("{path}/channel"), errors);
    }
    check_optional(obj, "accountId", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
}

pub fn validate_web_login_start_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    let allowed = &["force", "timeoutMs", "verbose", "accountId"];
    check_no_additional_properties(obj, allowed, path, errors);
    check_optional(obj, "force", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "timeoutMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
    check_optional(obj, "verbose", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "accountId", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
}

pub fn validate_web_login_wait_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["timeoutMs", "accountId"], path, errors);
    check_optional(obj, "timeoutMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
    check_optional(obj, "accountId", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_talk_mode_valid() {
        let mut e = Vec::new();
        validate_talk_mode_params(&json!({"enabled": true}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_channels_status_valid() {
        let mut e = Vec::new();
        validate_channels_status_params(&json!({"probe": true, "timeoutMs": 5000}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_channels_logout_valid() {
        let mut e = Vec::new();
        validate_channels_logout_params(&json!({"channel": "telegram"}), "", &mut e);
        assert!(e.is_empty());
    }
}
