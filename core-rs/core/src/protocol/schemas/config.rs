//! Config schema validators — mirrors `src/gateway/protocol/schema/config.ts`.

use crate::protocol::validation::*;

pub fn validate_config_get_params(
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
    check_no_additional_properties(obj, &[], path, errors);
}

pub fn validate_config_set_params(
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
    check_no_additional_properties(obj, &["raw", "baseHash"], path, errors);
    if check_required(obj, "raw", path, errors) {
        check_non_empty_string(&obj["raw"], &format!("{path}/raw"), errors);
    }
    check_optional(obj, "baseHash", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
}

fn validate_config_apply_like(
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
    let allowed = &["raw", "baseHash", "sessionKey", "note", "restartDelayMs"];
    check_no_additional_properties(obj, allowed, path, errors);
    if check_required(obj, "raw", path, errors) {
        check_non_empty_string(&obj["raw"], &format!("{path}/raw"), errors);
    }
    check_optional(obj, "baseHash", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "sessionKey", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "note", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "restartDelayMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
}

pub fn validate_config_apply_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_config_apply_like(value, path, errors);
}

pub fn validate_config_patch_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_config_apply_like(value, path, errors);
}

pub fn validate_config_schema_params(
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
    check_no_additional_properties(obj, &[], path, errors);
}

pub fn validate_config_schema_lookup_params(
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
    check_no_additional_properties(obj, &["path"], path, errors);
    if check_required(obj, "path", path, errors) {
        let p_path = format!("{path}/path");
        check_non_empty_string(&obj["path"], &p_path, errors);
        check_max_length(&obj["path"], &p_path, 1024, errors);
        #[allow(clippy::expect_used)]
        static PATH_RE: once_cell::sync::Lazy<regex::Regex> = once_cell::sync::Lazy::new(|| {
            regex::Regex::new(r"^[A-Za-z0-9_./\[\]\-*]+$").expect("valid regex")
        });
        check_pattern(&obj["path"], &p_path, &PATH_RE, errors);
    }
}

pub fn validate_update_run_params(
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
    let allowed = &["sessionKey", "note", "restartDelayMs", "timeoutMs"];
    check_no_additional_properties(obj, allowed, path, errors);
    check_optional(obj, "sessionKey", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "note", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "restartDelayMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(0), None, e);
    });
    check_optional(obj, "timeoutMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(1), None, e);
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_config_get_valid() {
        let mut e = Vec::new();
        validate_config_get_params(&json!({}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_config_set_valid() {
        let mut e = Vec::new();
        validate_config_set_params(&json!({"raw": "yaml content"}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_config_schema_lookup_valid() {
        let mut e = Vec::new();
        validate_config_schema_lookup_params(&json!({"path": "gateway.port"}), "", &mut e);
        assert!(e.is_empty());
    }
}
