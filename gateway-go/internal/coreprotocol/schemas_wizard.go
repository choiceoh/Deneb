package coreprotocol

// --- wizard.start ---

func validateWizardStartParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"mode", "workspace"}, path, errors)
	CheckOptional(obj, "mode", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"local", "remote"}, e)
	})
	CheckOptional(obj, "workspace", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
}

// --- wizard.next ---

func checkWizardAnswer(v any, p string, e *[]ValidationError) {
	if !RequireObject(v, p, e) {
		return
	}
	obj := v.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"stepId", "value"}, p, e)
	if CheckRequired(obj, "stepId", p, e) {
		CheckNonEmptyString(obj["stepId"], p+"/stepId", e)
	}
	// value is Type.Unknown() — no validation.
}

func validateWizardNextParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"sessionId", "answer"}, path, errors)
	if CheckRequired(obj, "sessionId", path, errors) {
		CheckNonEmptyString(obj["sessionId"], path+"/sessionId", errors)
	}
	CheckOptional(obj, "answer", path, errors, checkWizardAnswer)
}

// --- wizard.cancel / wizard.status ---

func validateSessionIDOnly(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"sessionId"}, path, errors)
	if CheckRequired(obj, "sessionId", path, errors) {
		CheckNonEmptyString(obj["sessionId"], path+"/sessionId", errors)
	}
}

func validateWizardCancelParams(value any, path string, errors *[]ValidationError) {
	validateSessionIDOnly(value, path, errors)
}

func validateWizardStatusParams(value any, path string, errors *[]ValidationError) {
	validateSessionIDOnly(value, path, errors)
}
