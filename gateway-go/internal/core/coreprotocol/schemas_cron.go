package coreprotocol

import (
	"fmt"
	"regexp"
	"strings"
)

// --- cron.list ---

func validateCronListParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{
		"includeDisabled", "limit", "offset", "query", "enabled", "sortBy", "sortDir",
	}, path, errors)
	CheckOptional(obj, "includeDisabled", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "limit", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), intPtr(200), e)
	})
	CheckOptional(obj, "offset", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
	CheckOptional(obj, "query", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "enabled", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"all", "enabled", "disabled"}, e)
	})
	CheckOptional(obj, "sortBy", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"nextRunAtMs", "updatedAtMs", "name"}, e)
	})
	CheckOptional(obj, "sortDir", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"asc", "desc"}, e)
	})
}

// --- cron.status ---

func validateCronStatusParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, nil, path, errors)
}

// --- cron.add ---

func validateCronAddParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{
		"name", "agentId", "sessionKey", "description", "enabled", "deleteAfterRun",
		"schedule", "sessionTarget", "wakeMode", "payload", "delivery", "failureAlert",
	}, path, errors)
	if CheckRequired(obj, "name", path, errors) {
		CheckNonEmptyString(obj["name"], path+"/name", errors)
	}
	CheckOptionalNullable(obj, "agentId", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptionalNullable(obj, "sessionKey", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckNonEmptyString(v, p, e)
	})
	CheckOptional(obj, "description", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "enabled", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	CheckOptional(obj, "deleteAfterRun", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckBoolean(v, p, e)
	})
	if CheckRequired(obj, "schedule", path, errors) {
		validateCronSchedule(obj["schedule"], path+"/schedule", errors)
	}
	if CheckRequired(obj, "sessionTarget", path, errors) {
		validateCronSessionTarget(obj["sessionTarget"], path+"/sessionTarget", errors)
	}
	if CheckRequired(obj, "wakeMode", path, errors) {
		CheckStringEnum(obj["wakeMode"], path+"/wakeMode", []string{"next-heartbeat", "now"}, errors)
	}
	if CheckRequired(obj, "payload", path, errors) {
		validateCronPayload(obj["payload"], path+"/payload", errors)
	}
	// delivery and failureAlert: any
}

// --- Discriminated union: schedule ---

func validateCronSchedule(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	kindVal := obj["kind"]
	kind, _ := kindVal.(string)
	switch kind {
	case "at":
		CheckNoAdditionalProperties(obj, []string{"kind", "at"}, path, errors)
		if CheckRequired(obj, "at", path, errors) {
			CheckNonEmptyString(obj["at"], path+"/at", errors)
		}
	case "every":
		CheckNoAdditionalProperties(obj, []string{"kind", "everyMs", "anchorMs", "anchorTime"}, path, errors)
		if CheckRequired(obj, "everyMs", path, errors) {
			CheckInteger(obj["everyMs"], path+"/everyMs", intPtr(1), nil, errors)
		}
		CheckOptional(obj, "anchorMs", path, errors, func(v any, p string, e *[]ValidationError) {
			CheckInteger(v, p, intPtr(0), nil, e)
		})
		CheckOptional(obj, "anchorTime", path, errors, func(v any, p string, e *[]ValidationError) {
			CheckNonEmptyString(v, p, e)
		})
	case "cron":
		CheckNoAdditionalProperties(obj, []string{"kind", "expr", "tz", "staggerMs"}, path, errors)
		if CheckRequired(obj, "expr", path, errors) {
			CheckNonEmptyString(obj["expr"], path+"/expr", errors)
		}
		CheckOptional(obj, "tz", path, errors, func(v any, p string, e *[]ValidationError) {
			CheckString(v, p, e)
		})
		CheckOptional(obj, "staggerMs", path, errors, func(v any, p string, e *[]ValidationError) {
			CheckInteger(v, p, intPtr(0), nil, e)
		})
	default:
		if _, ok := obj["kind"]; !ok {
			CheckRequired(obj, "kind", path, errors)
		} else {
			CheckStringEnum(obj["kind"], path+"/kind", []string{"at", "every", "cron"}, errors)
		}
	}
}

// --- Discriminated union: sessionTarget ---

func validateCronSessionTarget(value any, path string, errors *[]ValidationError) {
	s, ok := value.(string)
	if !ok {
		*errors = append(*errors, ValidationError{
			Path: path, Message: "must be string", Keyword: "type",
		})
		return
	}
	switch s {
	case "main", "isolated", "current":
		return
	default:
		if strings.HasPrefix(s, "session:") && len(s) > 8 {
			return
		}
		*errors = append(*errors, ValidationError{
			Path:    path,
			Message: `must be "main", "isolated", "current", or "session:<key>"`,
			Keyword: "enum",
		})
	}
}

// --- Discriminated union: payload ---

func validateCronPayload(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	kindVal := obj["kind"]
	kind, _ := kindVal.(string)
	switch kind {
	case "systemEvent":
		CheckNoAdditionalProperties(obj, []string{"kind", "text"}, path, errors)
		if CheckRequired(obj, "text", path, errors) {
			CheckNonEmptyString(obj["text"], path+"/text", errors)
		}
	case "agentTurn":
		CheckNoAdditionalProperties(obj, []string{
			"kind", "message", "model", "fallbacks", "thinking", "timeoutSeconds",
			"allowUnsafeExternalContent", "lightContext", "deliver", "channel",
			"to", "bestEffortDeliver", "retryCount", "retryBackoffMs",
		}, path, errors)
		if CheckRequired(obj, "message", path, errors) {
			CheckNonEmptyString(obj["message"], path+"/message", errors)
		}
	default:
		if _, ok := obj["kind"]; !ok {
			CheckRequired(obj, "kind", path, errors)
		} else {
			CheckStringEnum(obj["kind"], path+"/kind", []string{"systemEvent", "agentTurn"}, errors)
		}
	}
}

// --- cron.update / cron.remove / cron.run (id-or-jobId union) ---

func validateCronIDOrJobID(value any, path string, extraAllowed []string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	_, hasID := obj["id"]
	_, hasJobID := obj["jobId"]

	if !hasID && !hasJobID {
		*errors = append(*errors, ValidationError{
			Path: path, Message: "must have property 'id' or 'jobId'", Keyword: "required",
		})
	}

	allowed := append([]string{"id", "jobId"}, extraAllowed...)
	CheckNoAdditionalProperties(obj, allowed, path, errors)

	if hasID {
		CheckNonEmptyString(obj["id"], path+"/id", errors)
	}
	if hasJobID {
		CheckNonEmptyString(obj["jobId"], path+"/jobId", errors)
	}
}

func validateCronUpdateParams(value any, path string, errors *[]ValidationError) {
	validateCronIDOrJobID(value, path, []string{"patch"}, errors)
	if obj, ok := value.(map[string]any); ok {
		CheckOptional(obj, "patch", path, errors, func(v any, p string, e *[]ValidationError) {
			RequireObject(v, p, e)
		})
	}
}

func validateCronRemoveParams(value any, path string, errors *[]ValidationError) {
	validateCronIDOrJobID(value, path, nil, errors)
}

func validateCronRunParams(value any, path string, errors *[]ValidationError) {
	validateCronIDOrJobID(value, path, []string{"mode"}, errors)
	if obj, ok := value.(map[string]any); ok {
		CheckOptional(obj, "mode", path, errors, func(v any, p string, e *[]ValidationError) {
			CheckStringEnum(v, p, []string{"due", "force"}, e)
		})
	}
}

// --- cron.runs ---

var cronRunJobIDRE = regexp.MustCompile(`^[^/\\]+$`)

func checkCronRunJobID(v any, p string, e *[]ValidationError) {
	CheckNonEmptyString(v, p, e)
	CheckPattern(v, p, cronRunJobIDRE, e)
}

func checkStatusesArray(v any, p string, e *[]ValidationError) {
	if CheckArray(v, p, e) {
		CheckMinItems(v, p, 1, e)
		arr := v.([]any) //nolint:errcheck // type guaranteed by CheckArray above
		if len(arr) > 3 {
			*e = append(*e, ValidationError{
				Path: p, Message: "must NOT have more than 3 items", Keyword: "maxItems",
			})
		}
		for i, item := range arr {
			CheckStringEnum(item, fmt.Sprintf("%s/%d", p, i), []string{"ok", "error", "skipped"}, e)
		}
	}
}

func checkDeliveryStatusesArray(v any, p string, e *[]ValidationError) {
	if CheckArray(v, p, e) {
		CheckMinItems(v, p, 1, e)
		arr := v.([]any) //nolint:errcheck // type guaranteed by CheckArray above
		if len(arr) > 4 {
			*e = append(*e, ValidationError{
				Path: p, Message: "must NOT have more than 4 items", Keyword: "maxItems",
			})
		}
		for i, item := range arr {
			CheckStringEnum(item, fmt.Sprintf("%s/%d", p, i),
				[]string{"delivered", "not-delivered", "unknown", "not-requested"}, e)
		}
	}
}

func validateCronRunsParams(value any, path string, errors *[]ValidationError) {
	if !RequireObject(value, path, errors) {
		return
	}
	obj := value.(map[string]any) //nolint:errcheck // type guaranteed by RequireObject check above
	CheckNoAdditionalProperties(obj, []string{
		"scope", "id", "jobId", "limit", "offset", "statuses", "status",
		"deliveryStatuses", "deliveryStatus", "query", "sortDir",
	}, path, errors)
	CheckOptional(obj, "scope", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"job", "all"}, e)
	})
	CheckOptional(obj, "id", path, errors, checkCronRunJobID)
	CheckOptional(obj, "jobId", path, errors, checkCronRunJobID)
	CheckOptional(obj, "limit", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(1), intPtr(200), e)
	})
	CheckOptional(obj, "offset", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckInteger(v, p, intPtr(0), nil, e)
	})
	CheckOptional(obj, "statuses", path, errors, checkStatusesArray)
	CheckOptional(obj, "status", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"all", "ok", "error", "skipped"}, e)
	})
	CheckOptional(obj, "deliveryStatuses", path, errors, checkDeliveryStatusesArray)
	CheckOptional(obj, "deliveryStatus", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"delivered", "not-delivered", "unknown", "not-requested"}, e)
	})
	CheckOptional(obj, "query", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckString(v, p, e)
	})
	CheckOptional(obj, "sortDir", path, errors, func(v any, p string, e *[]ValidationError) {
		CheckStringEnum(v, p, []string{"asc", "desc"}, e)
	})
}
