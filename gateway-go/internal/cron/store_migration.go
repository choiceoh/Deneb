// store_migration.go — Legacy cron store format normalization.
// Mirrors src/cron/store-migration.ts (514 LOC).
// Normalizes legacy stored cron jobs to the current schema on load.
package cron

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// StoreMigrationIssues tracks counts of legacy patterns found during migration.
type StoreMigrationIssues struct {
	JobID                       int `json:"jobId,omitempty"`
	LegacyScheduleString        int `json:"legacyScheduleString,omitempty"`
	LegacyScheduleCron          int `json:"legacyScheduleCron,omitempty"`
	LegacyPayloadKind           int `json:"legacyPayloadKind,omitempty"`
	LegacyPayloadProvider       int `json:"legacyPayloadProvider,omitempty"`
	LegacyTopLevelPayloadFields int `json:"legacyTopLevelPayloadFields,omitempty"`
	LegacyDeliveryMode          int `json:"legacyDeliveryMode,omitempty"`
}

// StoreMigrationResult holds the outcome of normalizing stored jobs.
type StoreMigrationResult struct {
	Issues  StoreMigrationIssues `json:"issues"`
	Jobs    []map[string]any     `json:"jobs"`
	Mutated bool                 `json:"mutated"`
}

// NormalizeStoredCronJobs migrates legacy job formats to the current schema.
// Operates on raw map representations to handle arbitrary legacy fields.
func NormalizeStoredCronJobs(jobs []map[string]any) StoreMigrationResult {
	var issues StoreMigrationIssues
	mutated := false

	for _, raw := range jobs {
		jobMutated := normalizeOneStoredJob(raw, &issues)
		if jobMutated {
			mutated = true
		}
	}

	return StoreMigrationResult{
		Issues:  issues,
		Jobs:    jobs,
		Mutated: mutated,
	}
}

func normalizeOneStoredJob(raw map[string]any, issues *StoreMigrationIssues) bool {
	mutated := false

	// Ensure state object.
	if _, ok := raw["state"]; !ok {
		raw["state"] = map[string]any{}
		mutated = true
	} else if _, isMap := raw["state"].(map[string]any); !isMap {
		raw["state"] = map[string]any{}
		mutated = true
	}

	// Migrate jobId → id.
	rawID := stringField(raw, "id")
	legacyJobID := stringField(raw, "jobId")
	if rawID == "" && legacyJobID != "" {
		raw["id"] = legacyJobID
		mutated = true
		issues.JobID++
	}
	if _, has := raw["jobId"]; has {
		delete(raw, "jobId")
		mutated = true
		issues.JobID++
	}

	// Migrate string schedule → { kind: "cron", expr }.
	if schedStr, ok := raw["schedule"].(string); ok {
		expr := strings.TrimSpace(schedStr)
		raw["schedule"] = map[string]any{"kind": "cron", "expr": expr}
		mutated = true
		issues.LegacyScheduleString++
	}

	// Normalize name.
	name := stringField(raw, "name")
	if name == "" {
		raw["name"] = inferLegacyNameFromRaw(raw)
		mutated = true
	}

	// Normalize enabled.
	if _, ok := raw["enabled"].(bool); !ok {
		raw["enabled"] = true
		mutated = true
	}

	// Normalize wakeMode.
	wakeMode := strings.ToLower(strings.TrimSpace(stringField(raw, "wakeMode")))
	switch wakeMode {
	case "next-heartbeat":
		if raw["wakeMode"] != "next-heartbeat" {
			raw["wakeMode"] = "next-heartbeat"
			mutated = true
		}
	case "now":
		if raw["wakeMode"] != "now" {
			raw["wakeMode"] = "now"
			mutated = true
		}
	default:
		raw["wakeMode"] = "now"
		mutated = true
	}

	// Infer payload if missing.
	payload, payloadIsMap := raw["payload"].(map[string]any)
	if !payloadIsMap || payload == nil {
		if inferPayloadFromTopLevel(raw) {
			mutated = true
			issues.LegacyTopLevelPayloadFields++
		}
		payload, payloadIsMap = raw["payload"].(map[string]any)
	}

	// Normalize payload kind.
	if payloadIsMap && payload != nil {
		if normalizePayloadKindInMap(payload) {
			mutated = true
			issues.LegacyPayloadKind++
		}
		// Infer kind if missing.
		kind := stringField(payload, "kind")
		if kind == "" {
			if msg := stringField(payload, "message"); msg != "" {
				payload["kind"] = "agentTurn"
				mutated = true
				issues.LegacyPayloadKind++
			} else if txt := stringField(payload, "text"); txt != "" {
				payload["kind"] = "systemEvent"
				mutated = true
				issues.LegacyPayloadKind++
			}
		}
		// Copy top-level agent turn fields into payload.
		if stringField(payload, "kind") == "agentTurn" {
			if copyTopLevelAgentTurnFields(raw, payload) {
				mutated = true
			}
		}
	}

	// Strip legacy top-level fields.
	if stripLegacyTopLevelFields(raw) {
		mutated = true
		issues.LegacyTopLevelPayloadFields++
	}

	// Migrate legacy payload provider → channel (inlined from deleted payload_migration.go).
	if payloadIsMap && payload != nil {
		hadProvider := stringField(payload, "provider") != ""
		if migrateLegacyPayloadProvider(payload) {
			mutated = true
			if hadProvider {
				issues.LegacyPayloadProvider++
			}
		}
	}

	// Normalize schedule object.
	if sched, ok := raw["schedule"].(map[string]any); ok {
		if normalizeScheduleMap(sched, raw) {
			mutated = true
		}
		// Migrate legacy "cron" field to "expr".
		if cronVal := stringField(sched, "cron"); cronVal != "" {
			if stringField(sched, "expr") == "" {
				sched["expr"] = cronVal
			}
			delete(sched, "cron")
			mutated = true
			issues.LegacyScheduleCron++
		}
		// Resolve stagger for cron expressions.
		kind := stringField(sched, "kind")
		expr := stringField(sched, "expr")
		if (kind == "cron" || kind == "") && expr != "" {
			targetStagger := resolveStaggerForMigration(sched, expr)
			if targetStagger >= 0 {
				current, _ := sched["staggerMs"].(float64)
				if int64(current) != targetStagger {
					if targetStagger == 0 {
						delete(sched, "staggerMs")
					} else {
						sched["staggerMs"] = float64(targetStagger)
					}
					mutated = true
				}
			}
		}
	}

	// Normalize delivery mode.
	if delivery, ok := raw["delivery"].(map[string]any); ok {
		modeRaw := stringField(delivery, "mode")
		if modeRaw != "" {
			lowered := strings.ToLower(strings.TrimSpace(modeRaw))
			if lowered == "deliver" {
				delivery["mode"] = "announce"
				mutated = true
				issues.LegacyDeliveryMode++
			}
		} else {
			delivery["mode"] = "announce"
			mutated = true
		}
	}

	// Remove legacy isolation field.
	if _, has := raw["isolation"]; has {
		delete(raw, "isolation")
		mutated = true
	}

	// Normalize sessionTarget.
	payloadKind := ""
	if payloadIsMap && payload != nil {
		payloadKind = stringField(payload, "kind")
	}
	if normalizeSessionTarget(raw, payloadKind) {
		mutated = true
	}

	// Auto-set delivery for isolated agentTurn jobs.
	sessionTarget := strings.ToLower(stringField(raw, "sessionTarget"))
	isIsolatedAgentTurn := sessionTarget == "isolated" ||
		sessionTarget == "current" ||
		strings.HasPrefix(sessionTarget, "session:") ||
		(sessionTarget == "" && payloadKind == "agentTurn")

	// Auto-set delivery for isolated agentTurn jobs that don't have one.
	if isIsolatedAgentTurn && payloadKind == "agentTurn" {
		if _, hasDelivery := raw["delivery"].(map[string]any); !hasDelivery {
			raw["delivery"] = map[string]any{"mode": "announce"}
			mutated = true
		}
	}

	return mutated
}

// --- Helpers ---

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func mapOrNil(m map[string]any, key string) map[string]any {
	v, ok := m[key]
	if !ok {
		return nil
	}
	result, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return result
}

func inferPayloadFromTopLevel(raw map[string]any) bool {
	if msg := stringField(raw, "message"); msg != "" {
		raw["payload"] = map[string]any{"kind": "agentTurn", "message": msg}
		return true
	}
	if txt := stringField(raw, "text"); txt != "" {
		raw["payload"] = map[string]any{"kind": "systemEvent", "text": txt}
		return true
	}
	if cmd := stringField(raw, "command"); cmd != "" {
		raw["payload"] = map[string]any{"kind": "systemEvent", "text": cmd}
		return true
	}
	return false
}

func normalizePayloadKindInMap(payload map[string]any) bool {
	raw := stringField(payload, "kind")
	lower := strings.ToLower(raw)
	switch lower {
	case "agentturn":
		if payload["kind"] != "agentTurn" {
			payload["kind"] = "agentTurn"
			return true
		}
	case "systemevent":
		if payload["kind"] != "systemEvent" {
			payload["kind"] = "systemEvent"
			return true
		}
	}
	return false
}

func copyTopLevelAgentTurnFields(raw, payload map[string]any) bool {
	mutated := false

	copyString := func(field string) {
		if _, has := payload[field]; has {
			if s, ok := payload[field].(string); ok && strings.TrimSpace(s) != "" {
				return
			}
		}
		if v, ok := raw[field].(string); ok && strings.TrimSpace(v) != "" {
			payload[field] = strings.TrimSpace(v)
			mutated = true
		}
	}
	copyString("model")
	copyString("thinking")

	if _, has := payload["timeoutSeconds"]; !has {
		if v, ok := raw["timeoutSeconds"].(float64); ok && !math.IsInf(v, 0) && !math.IsNaN(v) {
			payload["timeoutSeconds"] = math.Max(0, math.Floor(v))
			mutated = true
		}
	}

	if _, has := payload["deliver"]; !has {
		if v, ok := raw["deliver"].(bool); ok {
			payload["deliver"] = v
			mutated = true
		}
	}
	if _, has := payload["channel"]; !has {
		if v := stringField(raw, "channel"); v != "" {
			payload["channel"] = v
			mutated = true
		}
	}
	if _, has := payload["to"]; !has {
		if v := stringField(raw, "to"); v != "" {
			payload["to"] = v
			mutated = true
		}
	}
	if _, has := payload["bestEffortDeliver"]; !has {
		if v, ok := raw["bestEffortDeliver"].(bool); ok {
			payload["bestEffortDeliver"] = v
			mutated = true
		}
	}
	if _, has := payload["provider"]; !has {
		if v := stringField(raw, "provider"); v != "" {
			payload["provider"] = v
			mutated = true
		}
	}

	return mutated
}

func stripLegacyTopLevelFields(raw map[string]any) bool {
	fields := []string{
		"model", "thinking", "timeoutSeconds", "allowUnsafeExternalContent",
		"message", "text", "command", "timeout",
	}
	found := false
	for _, f := range fields {
		if _, has := raw[f]; has {
			delete(raw, f)
			found = true
		}
	}
	return found
}

func normalizeScheduleMap(sched map[string]any, raw map[string]any) bool {
	mutated := false

	// Infer kind.
	kind := stringField(sched, "kind")
	if kind == "" {
		if _, has := sched["at"]; has {
			sched["kind"] = "at"
			mutated = true
		} else if _, has := sched["atMs"]; has {
			sched["kind"] = "at"
			mutated = true
		} else if _, has := sched["everyMs"]; has {
			sched["kind"] = "every"
			mutated = true
		} else if _, has := sched["expr"]; has {
			sched["kind"] = "cron"
			mutated = true
		}
	}

	// Normalize atMs → at (ISO string).
	atMsRaw := sched["atMs"]
	atRaw := stringField(sched, "at")
	var parsedAtMs int64
	hasParsed := false
	switch v := atMsRaw.(type) {
	case float64:
		parsedAtMs = int64(v)
		hasParsed = true
	case string:
		if ms := parseAbsoluteTimeMs(v); ms > 0 {
			parsedAtMs = ms
			hasParsed = true
		}
	default:
		if atRaw != "" {
			if ms := parseAbsoluteTimeMs(atRaw); ms > 0 {
				parsedAtMs = ms
				hasParsed = true
			}
		}
	}
	if hasParsed && parsedAtMs > 0 {
		sched["at"] = time.UnixMilli(parsedAtMs).UTC().Format(time.RFC3339)
		delete(sched, "atMs")
		mutated = true
	}

	// Normalize everyMs to integer.
	if everyRaw, has := sched["everyMs"]; has {
		if v, ok := everyRaw.(float64); ok {
			floored := int64(math.Floor(v))
			if int64(v) != floored || v != float64(floored) {
				sched["everyMs"] = float64(floored)
				mutated = true
			}
		}
	}

	// Normalize anchorMs for "every" schedules.
	resolvedKind := stringField(sched, "kind")
	if resolvedKind == "every" {
		if _, has := sched["anchorMs"]; !has {
			// Use createdAtMs or updatedAtMs as anchor.
			if created, ok := raw["createdAtMs"].(float64); ok && created > 0 {
				sched["anchorMs"] = math.Floor(created)
				mutated = true
			} else if updated, ok := raw["updatedAtMs"].(float64); ok && updated > 0 {
				sched["anchorMs"] = math.Floor(updated)
				mutated = true
			}
		}
	}

	return mutated
}

func resolveStaggerForMigration(sched map[string]any, expr string) int64 {
	// Check explicit stagger.
	if raw, has := sched["staggerMs"]; has {
		switch v := raw.(type) {
		case float64:
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				return int64(math.Max(0, math.Floor(v)))
			}
		}
	}
	// Default stagger for top-of-hour.
	if IsRecurringTopOfHourCronExpr(expr) {
		return DefaultTopOfHourStaggerMs
	}
	return -1 // no change needed
}

func normalizeSessionTarget(raw map[string]any, payloadKind string) bool {
	rawTarget := strings.TrimSpace(stringField(raw, "sessionTarget"))
	lowered := strings.ToLower(rawTarget)
	mutated := false

	switch {
	case lowered == "main" || lowered == "isolated" || lowered == "subagent":
		if raw["sessionTarget"] != lowered {
			raw["sessionTarget"] = lowered
			mutated = true
		}
	case strings.HasPrefix(lowered, "session:"):
		customID := strings.TrimSpace(rawTarget[8:])
		if customID != "" {
			normalized := fmt.Sprintf("session:%s", customID)
			if raw["sessionTarget"] != normalized {
				raw["sessionTarget"] = normalized
				mutated = true
			}
		}
	case lowered == "current":
		if raw["sessionTarget"] != "isolated" {
			raw["sessionTarget"] = "isolated"
			mutated = true
		}
	default:
		inferred := "main"
		if payloadKind == "agentTurn" {
			inferred = "isolated"
		}
		if raw["sessionTarget"] != inferred {
			raw["sessionTarget"] = inferred
			mutated = true
		}
	}
	return mutated
}

func inferLegacyNameFromRaw(raw map[string]any) string {
	// Try payload text/message first.
	if payload, ok := raw["payload"].(map[string]any); ok {
		kind := stringField(payload, "kind")
		var text string
		if kind == "systemEvent" {
			text = stringField(payload, "text")
		} else if kind == "agentTurn" {
			text = stringField(payload, "message")
		}
		if text != "" {
			firstLine := strings.SplitN(text, "\n", 2)[0]
			firstLine = strings.TrimSpace(firstLine)
			if firstLine != "" {
				if len(firstLine) > 60 {
					return firstLine[:59] + "…"
				}
				return firstLine
			}
		}
	}
	// Try schedule info.
	if sched, ok := raw["schedule"].(map[string]any); ok {
		kind := stringField(sched, "kind")
		if kind == "cron" {
			if expr := stringField(sched, "expr"); expr != "" {
				label := "Cron: " + expr
				if len(label) > 58 {
					return label[:57] + "…"
				}
				return label
			}
		}
		if kind == "every" {
			if everyMs, ok := sched["everyMs"].(float64); ok {
				return fmt.Sprintf("Every: %dms", int64(everyMs))
			}
		}
		if kind == "at" {
			return "One-shot"
		}
	}
	return "Cron job"
}

// migrateLegacyPayloadProvider normalizes the channel/provider field in a payload map.
// If a "provider" field exists, its value is moved to "channel" (lowercased) and "provider" is removed.
func migrateLegacyPayloadProvider(payload map[string]any) bool {
	mutated := false
	channelValue, _ := payload["channel"].(string)
	providerValue, _ := payload["provider"].(string)

	nextChannel := ""
	if ch := strings.TrimSpace(channelValue); ch != "" {
		nextChannel = strings.ToLower(ch)
	} else if pv := strings.TrimSpace(providerValue); pv != "" {
		nextChannel = strings.ToLower(pv)
	}

	if nextChannel != "" && channelValue != nextChannel {
		payload["channel"] = nextChannel
		mutated = true
	}
	if _, has := payload["provider"]; has {
		delete(payload, "provider")
		mutated = true
	}
	return mutated
}
