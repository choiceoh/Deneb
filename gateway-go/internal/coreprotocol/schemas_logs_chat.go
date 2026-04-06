package coreprotocol

// --- logs.tail ---

func validateLogsTailParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"cursor", "limit", "maxBytes"}, path, errors)
	CheckOptional(obj, "cursor", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
	CheckOptional(obj, "limit", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), intPtr(5000), e)
	})
	CheckOptional(obj, "maxBytes", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), intPtr(1_000_000), e)
	})
}

// --- chat.history ---

func validateChatHistoryParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"sessionKey", "limit"}, path, errors)
	if CheckRequired(obj, "sessionKey", path, errors) {
		CheckNonEmptyString(obj["sessionKey"], path+"/sessionKey", errors)
	}
	CheckOptional(obj, "limit", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), intPtr(1000), e)
	})
}

// --- chat.send ---

func checkInputProvenance(v any, p string, e *[]ValidationError) {
	if RequireObject(v, p, e) {
		prov := v.(map[string]any)
		CheckNoAdditionalProperties(prov, []string{
			"kind", "originSessionId", "sourceSessionKey", "sourceChannel", "sourceTool",
		}, p, e)
		if CheckRequired(prov, "kind", p, e) {
			CheckStringEnum(prov["kind"], p+"/kind",
				[]string{"external_user", "inter_session", "internal_system"}, e)
		}
	}
}

func validateChatSendParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{
		"sessionKey", "message", "thinking", "deliver", "attachments",
		"timeoutMs", "systemInputProvenance", "systemProvenanceReceipt", "idempotencyKey",
	}, path, errors)
	if CheckRequired(obj, "sessionKey", path, errors) {
		CheckNonEmptyString(obj["sessionKey"], path+"/sessionKey", errors)
		CheckMaxLength(obj["sessionKey"], path+"/sessionKey", ChatSendSessionKeyMaxLength, errors)
	}
	if CheckRequired(obj, "message", path, errors) {
		CheckString(obj["message"], path+"/message", errors)
	}
	CheckOptional(obj, "thinking", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "deliver", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "attachments", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckArray(v, p, e)
	})
	CheckOptional(obj, "timeoutMs", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
	CheckOptional(obj, "systemInputProvenance", path, errors, checkInputProvenance)
	CheckOptional(obj, "systemProvenanceReceipt", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	if CheckRequired(obj, "idempotencyKey", path, errors) {
		CheckNonEmptyString(obj["idempotencyKey"], path+"/idempotencyKey", errors)
	}
}

// --- chat.abort ---

func validateChatAbortParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"sessionKey", "runId"}, path, errors)
	if CheckRequired(obj, "sessionKey", path, errors) {
		CheckNonEmptyString(obj["sessionKey"], path+"/sessionKey", errors)
	}
	CheckOptional(obj, "runId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
}

// --- chat.inject ---

func validateChatInjectParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"sessionKey", "message", "label"}, path, errors)
	if CheckRequired(obj, "sessionKey", path, errors) {
		CheckNonEmptyString(obj["sessionKey"], path+"/sessionKey", errors)
	}
	if CheckRequired(obj, "message", path, errors) {
		CheckNonEmptyString(obj["message"], path+"/message", errors)
	}
	CheckOptional(obj, "label", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
		CheckMaxLength(v, p, 100, e)
	})
}
