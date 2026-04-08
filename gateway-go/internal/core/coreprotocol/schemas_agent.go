package coreprotocol

// --- agent ---

func validateAgentParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj, _ := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{
		"message", "agentId", "provider", "model", "to", "replyTo",
		"sessionId", "sessionKey", "thinking", "deliver", "attachments",
		"channel", "replyChannel", "accountId", "replyAccountId",
		"threadId", "groupId", "groupChannel", "groupSpace", "timeout",
		"bestEffortDeliver", "lane", "extraSystemPrompt", "internalEvents",
		"inputProvenance", "idempotencyKey", "label",
	}, path, errors)
	if CheckRequired(obj, "message", path, errors) {
		CheckNonEmptyString(obj["message"], path+"/message", errors)
	}
	CheckOptional(obj, "agentId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "provider", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "model", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "to", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "replyTo", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "sessionId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "sessionKey", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "thinking", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "deliver", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "attachments", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckArray(v, p, e)
	})
	CheckOptional(obj, "channel", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "replyChannel", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "accountId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "replyAccountId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "threadId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "groupId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "groupChannel", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "groupSpace", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "timeout", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
	CheckOptional(obj, "bestEffortDeliver", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "lane", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "extraSystemPrompt", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "internalEvents", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckArray(v, p, e)
	})
	// inputProvenance: any
	if CheckRequired(obj, "idempotencyKey", path, errors) {
		CheckNonEmptyString(obj["idempotencyKey"], path+"/idempotencyKey", errors)
	}
	CheckOptional(obj, "label", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
		CheckMaxLength(v, p, SessionLabelMaxLength, e)
	})
}

// --- agent.identity ---

func validateAgentIdentityParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj, _ := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"agentId", "sessionKey"}, path, errors)
	CheckOptional(obj, "agentId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "sessionKey", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
}

// --- agent.wait ---

func validateAgentWaitParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj, _ := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"runId", "timeoutMs"}, path, errors)
	if CheckRequired(obj, "runId", path, errors) {
		CheckNonEmptyString(obj["runId"], path+"/runId", errors)
	}
	CheckOptional(obj, "timeoutMs", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
}
