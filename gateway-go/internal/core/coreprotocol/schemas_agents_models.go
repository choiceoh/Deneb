package coreprotocol

// --- agents.list ---

func validateAgentsListParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, nil, path, errors)
}

// --- agents.create ---

func validateAgentsCreateParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"name", "workspace", "emoji", "avatar"}, path, errors)
	if CheckRequired(obj, "name", path, errors) {
		CheckNonEmptyString(obj["name"], path+"/name", errors)
	}
	if CheckRequired(obj, "workspace", path, errors) {
		CheckNonEmptyString(obj["workspace"], path+"/workspace", errors)
	}
	CheckOptional(obj, "emoji", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "avatar", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
}

// --- agents.update ---

func validateAgentsUpdateParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"agentId", "name", "workspace", "model", "avatar"}, path, errors)
	if CheckRequired(obj, "agentId", path, errors) {
		CheckNonEmptyString(obj["agentId"], path+"/agentId", errors)
	}
	CheckOptional(obj, "name", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "workspace", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "model", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "avatar", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
}

// --- agents.delete ---

func validateAgentsDeleteParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"agentId", "deleteFiles"}, path, errors)
	if CheckRequired(obj, "agentId", path, errors) {
		CheckNonEmptyString(obj["agentId"], path+"/agentId", errors)
	}
	CheckOptional(obj, "deleteFiles", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
}

// --- agents.files.list ---

func validateAgentsFilesListParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"agentId"}, path, errors)
	if CheckRequired(obj, "agentId", path, errors) {
		CheckNonEmptyString(obj["agentId"], path+"/agentId", errors)
	}
}

// --- agents.files.get ---

func validateAgentsFilesGetParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"agentId", "name"}, path, errors)
	if CheckRequired(obj, "agentId", path, errors) {
		CheckNonEmptyString(obj["agentId"], path+"/agentId", errors)
	}
	if CheckRequired(obj, "name", path, errors) {
		CheckNonEmptyString(obj["name"], path+"/name", errors)
	}
}

// --- agents.files.set ---

func validateAgentsFilesSetParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"agentId", "name", "content"}, path, errors)
	if CheckRequired(obj, "agentId", path, errors) {
		CheckNonEmptyString(obj["agentId"], path+"/agentId", errors)
	}
	if CheckRequired(obj, "name", path, errors) {
		CheckNonEmptyString(obj["name"], path+"/name", errors)
	}
	if CheckRequired(obj, "content", path, errors) {
		CheckString(obj["content"], path+"/content", errors)
	}
}

// --- models.list ---

func validateModelsListParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, nil, path, errors)
}

// --- skills.status ---

func validateSkillsStatusParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"agentId"}, path, errors)
	CheckOptional(obj, "agentId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
}

// --- skills.bins ---

func validateSkillsBinsParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, nil, path, errors)
}

// --- skills.install ---

func validateSkillsInstallParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"name", "installId", "timeoutMs"}, path, errors)
	if CheckRequired(obj, "name", path, errors) {
		CheckNonEmptyString(obj["name"], path+"/name", errors)
	}
	if CheckRequired(obj, "installId", path, errors) {
		CheckNonEmptyString(obj["installId"], path+"/installId", errors)
	}
	CheckOptional(obj, "timeoutMs", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1000), nil, e)
	})
}

// --- skills.update ---

func checkStringRecord(v any, p string, e *[]ValidationError) {
	if RequireObject(v, p, e) {
		m := v.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
		for key, val := range m {
			if key == "" {
				*e = append(*e, ValidationError{
					Path: p, Message: "key must be non-empty, got \"\"", Keyword: "minLength",
				})
			}
			CheckString(val, p+"/"+key, e)
		}
	}
}

func validateSkillsUpdateParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"skillKey", "enabled", "apiKey", "env"}, path, errors)
	if CheckRequired(obj, "skillKey", path, errors) {
		CheckNonEmptyString(obj["skillKey"], path+"/skillKey", errors)
	}
	CheckOptional(obj, "enabled", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "apiKey", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "env", path, errors, checkStringRecord)
}

// --- tools.catalog ---

func validateToolsCatalogParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"agentId", "includePlugins"}, path, errors)
	CheckOptional(obj, "agentId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "includePlugins", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
}
