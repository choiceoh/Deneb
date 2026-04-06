package coreprotocol

// --- secrets.reload ---

func validateSecretsReloadParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, nil, path, errors)
}

// --- secrets.resolve ---

func validateSecretsResolveParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"commandName", "targetIds"}, path, errors)
	if CheckRequired(obj, "commandName", path, errors) {
		CheckNonEmptyString(obj["commandName"], path+"/commandName", errors)
	}
	if CheckRequired(obj, "targetIds", path, errors) {
		CheckNonEmptyStringArray(obj["targetIds"], path+"/targetIds", errors)
	}
}
