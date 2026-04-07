package coreprotocol

import "regexp"

// --- config.get ---

func validateConfigGetParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, nil, path, errors)
}

// --- config.set ---

func validateConfigSetParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"raw", "baseHash"}, path, errors)
	if CheckRequired(obj, "raw", path, errors) {
		CheckNonEmptyString(obj["raw"], path+"/raw", errors)
	}
	CheckOptional(obj, "baseHash", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
}

// --- config.apply / config.patch (shared shape) ---

func validateConfigApplyLike(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"raw", "baseHash", "sessionKey", "note", "restartDelayMs"}, path, errors)
	if CheckRequired(obj, "raw", path, errors) {
		CheckNonEmptyString(obj["raw"], path+"/raw", errors)
	}
	CheckOptional(obj, "baseHash", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "sessionKey", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "note", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "restartDelayMs", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
}

func validateConfigApplyParams(value any, path string, errors *[]ValidationError) {
	validateConfigApplyLike(value, path, errors)
}

func validateConfigPatchParams(value any, path string, errors *[]ValidationError) {
	validateConfigApplyLike(value, path, errors)
}

// --- config.schema ---

func validateConfigSchemaParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, nil, path, errors)
}

// --- config.schema.lookup ---

var configPathRE = regexp.MustCompile(`^[A-Za-z0-9_./\[\]\-*]+$`)

func checkConfigPath(v any, p string, e *[]ValidationError) {
	CheckNonEmptyString(v, p, e)
	CheckMaxLength(v, p, 1024, e)
	CheckPattern(v, p, configPathRE, e)
}

func validateConfigSchemaLookupParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"path"}, path, errors)
	if CheckRequired(obj, "path", path, errors) {
		checkConfigPath(obj["path"], path+"/path", errors)
	}
}

// --- update.run ---

func validateUpdateRunParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"sessionKey", "note", "restartDelayMs", "timeoutMs"}, path, errors)
	CheckOptional(obj, "sessionKey", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "note", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "restartDelayMs", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
	CheckOptional(obj, "timeoutMs", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), nil, e)
	})
}
