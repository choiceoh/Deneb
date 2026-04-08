package coreprotocol

// --- sessions.list ---

func validateSessionsListParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{
		"limit", "activeMinutes", "includeGlobal", "includeUnknown",
		"includeDerivedTitles", "includeLastMessage", "label", "spawnedBy", "agentId", "search",
	}, path, errors)
	CheckOptional(obj, "limit", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), nil, e)
	})
	CheckOptional(obj, "activeMinutes", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), nil, e)
	})
	CheckOptional(obj, "includeGlobal", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "includeUnknown", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "includeDerivedTitles", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "includeLastMessage", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "label", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
		CheckMaxLength(v, p, SessionLabelMaxLength, e)
	})
	CheckOptional(obj, "spawnedBy", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "agentId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "search", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
}

// --- sessions.preview ---

func validateSessionsPreviewParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"keys", "limit", "maxChars"}, path, errors)
	if CheckRequired(obj, "keys", path, errors) {
		CheckNonEmptyStringArrayMin1(obj["keys"], path+"/keys", errors)
	}
	CheckOptional(obj, "limit", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), nil, e)
	})
	CheckOptional(obj, "maxChars", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(20), nil, e)
	})
}

// --- sessions.resolve ---

func validateSessionsResolveParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{
		"key", "sessionId", "label", "agentId", "spawnedBy", "includeGlobal", "includeUnknown",
	}, path, errors)
	CheckOptional(obj, "key", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "sessionId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "label", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
		CheckMaxLength(v, p, SessionLabelMaxLength, e)
	})
	CheckOptional(obj, "agentId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "spawnedBy", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "includeGlobal", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "includeUnknown", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
}

// --- sessions.create ---

func validateSessionsCreateParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{
		"key", "agentId", "label", "model", "parentSessionKey", "task", "message",
	}, path, errors)
	CheckOptional(obj, "key", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "agentId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "label", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
		CheckMaxLength(v, p, SessionLabelMaxLength, e)
	})
	CheckOptional(obj, "model", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "parentSessionKey", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "task", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "message", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
}

// --- sessions.send ---

func validateSessionsSendParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{
		"key", "message", "thinking", "attachments", "timeoutMs", "idempotencyKey",
	}, path, errors)
	if CheckRequired(obj, "key", path, errors) {
		CheckNonEmptyString(obj["key"], path+"/key", errors)
	}
	if CheckRequired(obj, "message", path, errors) {
		CheckString(obj["message"], path+"/message", errors)
	}
	CheckOptional(obj, "thinking", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "attachments", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckArray(v, p, e)
	})
	CheckOptional(obj, "timeoutMs", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
	CheckOptional(obj, "idempotencyKey", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
}

// --- sessions.messages.subscribe / unsubscribe ---

func validateSessionKeyOnly(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"key"}, path, errors)
	if CheckRequired(obj, "key", path, errors) {
		CheckNonEmptyString(obj["key"], path+"/key", errors)
	}
}

func validateSessionsMessagesSubscribeParams(value any, path string, errors *[]ValidationError) {
	validateSessionKeyOnly(value, path, errors)
}

func validateSessionsMessagesUnsubscribeParams(value any, path string, errors *[]ValidationError) {
	validateSessionKeyOnly(value, path, errors)
}

// --- sessions.abort ---

func validateSessionsAbortParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"key", "runId"}, path, errors)
	if CheckRequired(obj, "key", path, errors) {
		CheckNonEmptyString(obj["key"], path+"/key", errors)
	}
	CheckOptional(obj, "runId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
}

// --- sessions.patch ---

func validateSessionsPatchParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{
		"key", "label", "thinkingLevel", "fastMode", "verboseLevel", "reasoningLevel",
		"responseUsage", "elevatedLevel", "execHost", "execSecurity", "execAsk", "execNode",
		"model", "spawnedBy", "spawnedWorkspaceDir", "spawnDepth", "subagentRole",
		"subagentControlScope", "sendPolicy", "groupActivation",
	}, path, errors)
	if CheckRequired(obj, "key", path, errors) {
		CheckNonEmptyString(obj["key"], path+"/key", errors)
	}
	CheckOptionalNullable(obj, "label", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
		CheckMaxLength(v, p, SessionLabelMaxLength, e)
	})
	CheckOptionalNullable(obj, "thinkingLevel", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "fastMode", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptionalNullable(obj, "verboseLevel", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "reasoningLevel", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "responseUsage", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"off", "tokens", "full", "on"}, e)
	})
	CheckOptionalNullable(obj, "elevatedLevel", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "execHost", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "execSecurity", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "execAsk", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "execNode", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "model", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "spawnedBy", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "spawnedWorkspaceDir", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "spawnDepth", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
	CheckOptionalNullable(obj, "subagentRole", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"orchestrator", "leaf"}, e)
	})
	CheckOptionalNullable(obj, "subagentControlScope", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"children", "none"}, e)
	})
	CheckOptionalNullable(obj, "sendPolicy", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"allow", "deny"}, e)
	})
	CheckOptionalNullable(obj, "groupActivation", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"mention", "always"}, e)
	})
}

// --- sessions.reset ---

func validateSessionsResetParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"key", "reason"}, path, errors)
	if CheckRequired(obj, "key", path, errors) {
		CheckNonEmptyString(obj["key"], path+"/key", errors)
	}
	CheckOptional(obj, "reason", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"new", "reset"}, e)
	})
}

// --- sessions.delete ---

func validateSessionsDeleteParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"key", "deleteTranscript", "emitLifecycleHooks"}, path, errors)
	if CheckRequired(obj, "key", path, errors) {
		CheckNonEmptyString(obj["key"], path+"/key", errors)
	}
	CheckOptional(obj, "deleteTranscript", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "emitLifecycleHooks", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
}
