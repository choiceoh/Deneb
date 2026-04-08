package coreprotocol

// --- telegram.status ---

func validateChannelsStatusParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"probe", "timeoutMs"}, path, errors)
	CheckOptional(obj, "probe", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "timeoutMs", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
}

// --- telegram.logout ---

func validateChannelsLogoutParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"channel", "accountId"}, path, errors)
	if CheckRequired(obj, "channel", path, errors) {
		CheckNonEmptyString(obj["channel"], path+"/channel", errors)
	}
	CheckOptional(obj, "accountId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
}
