package coreprotocol

// --- talk.mode ---

func validateTalkModeParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"enabled", "phase"}, path, errors)
	if CheckRequired(obj, "enabled", path, errors) {
		CheckBoolean(obj["enabled"], path+"/enabled", errors)
	}
	CheckOptional(obj, "phase", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
}

// --- talk.config ---

func validateTalkConfigParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"includeSecrets"}, path, errors)
	CheckOptional(obj, "includeSecrets", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
}

// --- telegram.status ---

func validateChannelsStatusParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
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
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"channel", "accountId"}, path, errors)
	if CheckRequired(obj, "channel", path, errors) {
		CheckNonEmptyString(obj["channel"], path+"/channel", errors)
	}
	CheckOptional(obj, "accountId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
}

// --- weblogin.start ---

func validateWebLoginStartParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"force", "timeoutMs", "verbose", "accountId"}, path, errors)
	CheckOptional(obj, "force", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "timeoutMs", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
	CheckOptional(obj, "verbose", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "accountId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
}

// --- weblogin.wait ---

func validateWebLoginWaitParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"timeoutMs", "accountId"}, path, errors)
	CheckOptional(obj, "timeoutMs", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
	CheckOptional(obj, "accountId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
}
