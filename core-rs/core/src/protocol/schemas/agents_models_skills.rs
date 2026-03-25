//! Agents/models/skills validators — mirrors `src/gateway/protocol/schema/agents-models-skills.ts`.

use crate::protocol::validation::*;

pub fn validate_agents_list_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    check_no_additional_properties(value.as_object().unwrap(), &[], path, errors);
}

pub fn validate_agents_create_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["name", "workspace", "emoji", "avatar"], path, errors);
    if check_required(obj, "name", path, errors) {
        check_non_empty_string(&obj["name"], &format!("{path}/name"), errors);
    }
    if check_required(obj, "workspace", path, errors) {
        check_non_empty_string(&obj["workspace"], &format!("{path}/workspace"), errors);
    }
    check_optional(obj, "emoji", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    check_optional(obj, "avatar", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
}

pub fn validate_agents_update_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    let allowed = &["agentId", "name", "workspace", "model", "avatar"];
    check_no_additional_properties(obj, allowed, path, errors);
    if check_required(obj, "agentId", path, errors) {
        check_non_empty_string(&obj["agentId"], &format!("{path}/agentId"), errors);
    }
    check_optional(obj, "name", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "workspace", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "model", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "avatar", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
}

pub fn validate_agents_delete_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["agentId", "deleteFiles"], path, errors);
    if check_required(obj, "agentId", path, errors) {
        check_non_empty_string(&obj["agentId"], &format!("{path}/agentId"), errors);
    }
    check_optional(obj, "deleteFiles", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
}

pub fn validate_agents_files_list_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["agentId"], path, errors);
    if check_required(obj, "agentId", path, errors) {
        check_non_empty_string(&obj["agentId"], &format!("{path}/agentId"), errors);
    }
}

pub fn validate_agents_files_get_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["agentId", "name"], path, errors);
    if check_required(obj, "agentId", path, errors) {
        check_non_empty_string(&obj["agentId"], &format!("{path}/agentId"), errors);
    }
    if check_required(obj, "name", path, errors) {
        check_non_empty_string(&obj["name"], &format!("{path}/name"), errors);
    }
}

pub fn validate_agents_files_set_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["agentId", "name", "content"], path, errors);
    if check_required(obj, "agentId", path, errors) {
        check_non_empty_string(&obj["agentId"], &format!("{path}/agentId"), errors);
    }
    if check_required(obj, "name", path, errors) {
        check_non_empty_string(&obj["name"], &format!("{path}/name"), errors);
    }
    if check_required(obj, "content", path, errors) {
        check_string(&obj["content"], &format!("{path}/content"), errors);
    }
}

pub fn validate_models_list_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    check_no_additional_properties(value.as_object().unwrap(), &[], path, errors);
}

pub fn validate_skills_status_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["agentId"], path, errors);
    check_optional(obj, "agentId", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
}

pub fn validate_skills_bins_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    check_no_additional_properties(value.as_object().unwrap(), &[], path, errors);
}

pub fn validate_skills_install_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["name", "installId", "timeoutMs"], path, errors);
    if check_required(obj, "name", path, errors) {
        check_non_empty_string(&obj["name"], &format!("{path}/name"), errors);
    }
    if check_required(obj, "installId", path, errors) {
        check_non_empty_string(&obj["installId"], &format!("{path}/installId"), errors);
    }
    check_optional(obj, "timeoutMs", path, errors, |v, p, e| {
        check_integer(v, p, Some(1000), None, e);
    });
}

pub fn validate_skills_update_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["skillKey", "enabled", "apiKey", "env"], path, errors);
    if check_required(obj, "skillKey", path, errors) {
        check_non_empty_string(&obj["skillKey"], &format!("{path}/skillKey"), errors);
    }
    check_optional(obj, "enabled", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
    check_optional(obj, "apiKey", path, errors, |v, p, e| {
        check_string(v, p, e);
    });
    // env: Record<NonEmptyString, String> — validate keys are non-empty, values are strings.
    check_optional(obj, "env", path, errors, |v, p, e| {
        if require_object(v, p, e) {
            if let Some(map) = v.as_object() {
                for (key, val) in map {
                    if key.is_empty() {
                        e.push(ValidationError {
                            path: p.to_string(),
                            message: format!("key must be non-empty, got \"\""),
                            keyword: "minLength",
                        });
                    }
                    check_string(val, &format!("{p}/{key}"), e);
                }
            }
        }
    });
}

pub fn validate_tools_catalog_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    if !require_object(value, path, errors) {
        return;
    }
    let obj = value.as_object().unwrap();
    check_no_additional_properties(obj, &["agentId", "includePlugins"], path, errors);
    check_optional(obj, "agentId", path, errors, |v, p, e| {
        check_non_empty_string(v, p, e);
    });
    check_optional(obj, "includePlugins", path, errors, |v, p, e| {
        check_boolean(v, p, e);
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_agents_create_valid() {
        let mut e = Vec::new();
        validate_agents_create_params(
            &json!({"name": "bot", "workspace": "/home/bot"}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }

    #[test]
    fn test_agents_files_set_valid() {
        let mut e = Vec::new();
        validate_agents_files_set_params(
            &json!({"agentId": "a1", "name": "config.yaml", "content": "key: val"}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }

    #[test]
    fn test_skills_install_valid() {
        let mut e = Vec::new();
        validate_skills_install_params(&json!({"name": "weather", "installId": "i1"}), "", &mut e);
        assert!(e.is_empty());
    }
}
