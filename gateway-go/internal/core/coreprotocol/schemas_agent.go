package coreprotocol

import "fmt"

// --- agent.send ---

func validateSendParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj, _ := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{
		"to", "message", "mediaUrl", "mediaUrls", "channel", "accountId",
		"agentId", "threadId", "sessionKey", "idempotencyKey",
	}, path, errors)
	if CheckRequired(obj, "to", path, errors) {
		CheckNonEmptyString(obj["to"], path+"/to", errors)
	}
	CheckOptional(obj, "message", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "mediaUrl", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "mediaUrls", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringArray(v, p, e)
	})
	CheckOptional(obj, "channel", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "accountId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "agentId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "threadId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "sessionKey", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	if CheckRequired(obj, "idempotencyKey", path, errors) {
		CheckNonEmptyString(obj["idempotencyKey"], path+"/idempotencyKey", errors)
	}
}

// --- agent.poll ---

func checkPollOptions(v any, p string, e *[]ValidationError) {
	if CheckArray(v, p, e) {
		CheckMinItems(v, p, 2, e)
		arr, _ := v.([]any) //nolint:errcheck // type guaranteed by CheckArray above
		if len(arr) > 12 {
			*e = append(*e, ValidationError{
				Path: p, Message: "must NOT have more than 12 items", Keyword: "maxItems",
			})
		}
		for i, item := range arr {
			CheckNonEmptyString(item, fmt.Sprintf("%s/%d", p, i), e)
		}
	}
}

func validatePollParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj, _ := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{
		"to", "question", "options", "maxSelections", "durationSeconds",
		"durationHours", "silent", "isAnonymous", "threadId", "channel",
		"accountId", "idempotencyKey",
	}, path, errors)
	if CheckRequired(obj, "to", path, errors) {
		CheckNonEmptyString(obj["to"], path+"/to", errors)
	}
	if CheckRequired(obj, "question", path, errors) {
		CheckNonEmptyString(obj["question"], path+"/question", errors)
	}
	if CheckRequired(obj, "options", path, errors) {
		checkPollOptions(obj["options"], path+"/options", errors)
	}
	CheckOptional(obj, "maxSelections", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), intPtr(12), e)
	})
	CheckOptional(obj, "durationSeconds", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), intPtr(604_800), e)
	})
	CheckOptional(obj, "durationHours", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), nil, e)
	})
	CheckOptional(obj, "silent", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "isAnonymous", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "threadId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "channel", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "accountId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	if CheckRequired(obj, "idempotencyKey", path, errors) {
		CheckNonEmptyString(obj["idempotencyKey"], path+"/idempotencyKey", errors)
	}
}

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

// --- agent.wake ---

func validateWakeParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj, _ := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{"mode", "text"}, path, errors)
	if CheckRequired(obj, "mode", path, errors) {
		CheckStringEnum(obj["mode"], path+"/mode", []string{"now", "next-heartbeat"}, errors)
	}
	if CheckRequired(obj, "text", path, errors) {
		CheckNonEmptyString(obj["text"], path+"/text", errors)
	}
}
