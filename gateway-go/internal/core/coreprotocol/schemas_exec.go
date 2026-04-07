package coreprotocol

// --- exec.approvals.get ---

func validateExecApprovalsGetParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, nil, path, errors)
}

// --- exec.approvals.set ---

func validateExecApprovalsSetParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"file", "baseHash"}, path, errors)
	if CheckRequired(obj, "file", path, errors) {
		RequireObject(obj["file"], path+"/file", errors)
	}
	CheckOptional(obj, "baseHash", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
}

// --- exec.approval.request ---

func checkStringOrNumber(v any, p string, e *[]ValidationError) {
	if _, ok := v.(string); ok {
		return
	}
	if _, ok := v.(float64); ok {
		return
	}
	*e = append(*e, ValidationError{
		Path: p, Message: "must be string or number", Keyword: "type",
	})
}

func validateExecApprovalRequestParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{
		"id", "command", "commandArgv", "systemRunPlan", "env", "cwd", "nodeId", "host",
		"security", "ask", "agentId", "resolvedPath", "sessionKey",
		"turnSourceChannel", "turnSourceTo", "turnSourceAccountId", "turnSourceThreadId",
		"timeoutMs", "twoPhase",
	}, path, errors)
	CheckOptional(obj, "id", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "command", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "commandArgv", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringArray(v, p, e)
	})
	// systemRunPlan: any
	// env: any
	CheckOptionalNullable(obj, "cwd", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptionalNullable(obj, "nodeId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptionalNullable(obj, "host", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptionalNullable(obj, "security", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptionalNullable(obj, "ask", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptionalNullable(obj, "agentId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptionalNullable(obj, "resolvedPath", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptionalNullable(obj, "sessionKey", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptionalNullable(obj, "turnSourceChannel", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptionalNullable(obj, "turnSourceTo", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptionalNullable(obj, "turnSourceAccountId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptionalNullable(obj, "turnSourceThreadId", path, errors, checkStringOrNumber)
	CheckOptional(obj, "timeoutMs", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), nil, e)
	})
	CheckOptional(obj, "twoPhase", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
}

// --- exec.approval.resolve ---

func validateExecApprovalResolveParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"id", "decision"}, path, errors)
	if CheckRequired(obj, "id", path, errors) {
		CheckNonEmptyString(obj["id"], path+"/id", errors)
	}
	if CheckRequired(obj, "decision", path, errors) {
		CheckNonEmptyString(obj["decision"], path+"/decision", errors)
	}
}

// --- exec.approvals.node.get ---

func validateExecApprovalsNodeGetParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"nodeId"}, path, errors)
	if CheckRequired(obj, "nodeId", path, errors) {
		CheckNonEmptyString(obj["nodeId"], path+"/nodeId", errors)
	}
}

// --- exec.approvals.node.set ---

func validateExecApprovalsNodeSetParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any)
	CheckNoAdditionalProperties(obj, []string{"nodeId", "file", "baseHash"}, path, errors)
	if CheckRequired(obj, "nodeId", path, errors) {
		CheckNonEmptyString(obj["nodeId"], path+"/nodeId", errors)
	}
	if CheckRequired(obj, "file", path, errors) {
		RequireObject(obj["file"], path+"/file", errors)
	}
	CheckOptional(obj, "baseHash", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
}
