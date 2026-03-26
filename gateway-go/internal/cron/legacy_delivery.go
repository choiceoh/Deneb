// legacy_delivery.go — Extract and merge legacy delivery hints from payload fields.
// Mirrors src/cron/legacy-delivery.ts (157 LOC).
package cron

import "strings"

// HasLegacyDeliveryHints checks if a payload map contains legacy delivery fields
// (deliver, bestEffortDeliver, channel, provider, to).
func HasLegacyDeliveryHints(payload map[string]any) bool {
	if _, ok := payload["deliver"].(bool); ok {
		return true
	}
	if _, ok := payload["bestEffortDeliver"].(bool); ok {
		return true
	}
	if v, ok := payload["channel"].(string); ok && strings.TrimSpace(v) != "" {
		return true
	}
	if v, ok := payload["provider"].(string); ok && strings.TrimSpace(v) != "" {
		return true
	}
	if v, ok := payload["to"].(string); ok && strings.TrimSpace(v) != "" {
		return true
	}
	return false
}

// BuildDeliveryFromLegacyPayload creates a new delivery map from legacy payload fields.
func BuildDeliveryFromLegacyPayload(payload map[string]any) map[string]any {
	deliver, _ := payload["deliver"].(bool)
	deliverSet := false
	if _, ok := payload["deliver"]; ok {
		deliverSet = true
	}

	mode := "announce"
	if deliverSet && !deliver {
		mode = "none"
	}

	channelRaw := ""
	if v, ok := payload["channel"].(string); ok && strings.TrimSpace(v) != "" {
		channelRaw = strings.ToLower(strings.TrimSpace(v))
	} else if v, ok := payload["provider"].(string); ok && strings.TrimSpace(v) != "" {
		channelRaw = strings.ToLower(strings.TrimSpace(v))
	}

	toRaw := ""
	if v, ok := payload["to"].(string); ok {
		toRaw = strings.TrimSpace(v)
	}

	next := map[string]any{"mode": mode}
	if channelRaw != "" {
		next["channel"] = channelRaw
	}
	if toRaw != "" {
		next["to"] = toRaw
	}
	if v, ok := payload["bestEffortDeliver"].(bool); ok {
		next["bestEffort"] = v
	}
	return next
}

// BuildDeliveryPatchFromLegacyPayload creates a delivery patch from legacy payload fields.
// Returns nil if no legacy hints are present.
func BuildDeliveryPatchFromLegacyPayload(payload map[string]any) map[string]any {
	deliver, _ := payload["deliver"].(bool)
	deliverSet := false
	if _, ok := payload["deliver"]; ok {
		deliverSet = true
	}

	channelRaw := ""
	if v, ok := payload["channel"].(string); ok && strings.TrimSpace(v) != "" {
		channelRaw = strings.ToLower(strings.TrimSpace(v))
	} else if v, ok := payload["provider"].(string); ok && strings.TrimSpace(v) != "" {
		channelRaw = strings.ToLower(strings.TrimSpace(v))
	}
	toRaw := ""
	if v, ok := payload["to"].(string); ok {
		toRaw = strings.TrimSpace(v)
	}
	_, hasBestEffort := payload["bestEffortDeliver"].(bool)

	next := map[string]any{}
	hasPatch := false

	if deliverSet && !deliver {
		next["mode"] = "none"
		hasPatch = true
	} else if deliver || channelRaw != "" || toRaw != "" || hasBestEffort {
		next["mode"] = "announce"
		hasPatch = true
	}
	if channelRaw != "" {
		next["channel"] = channelRaw
		hasPatch = true
	}
	if toRaw != "" {
		next["to"] = toRaw
		hasPatch = true
	}
	if v, ok := payload["bestEffortDeliver"].(bool); ok {
		next["bestEffort"] = v
		hasPatch = true
	}

	if !hasPatch {
		return nil
	}
	return next
}

// MergeLegacyDeliveryInto merges legacy payload delivery hints into an existing delivery map.
func MergeLegacyDeliveryInto(delivery, payload map[string]any) (map[string]any, bool) {
	patch := BuildDeliveryPatchFromLegacyPayload(payload)
	if patch == nil {
		return delivery, false
	}

	next := make(map[string]any, len(delivery))
	for k, v := range delivery {
		next[k] = v
	}
	mutated := false

	for _, key := range []string{"mode", "channel", "to", "bestEffort"} {
		if pv, has := patch[key]; has && pv != next[key] {
			next[key] = pv
			mutated = true
		}
	}

	return next, mutated
}

// LegacyDeliveryInputResult holds the result of normalizing legacy delivery input.
type LegacyDeliveryInputResult struct {
	Delivery map[string]any
	Mutated  bool
}

// NormalizeLegacyDeliveryInputMap normalizes legacy delivery hints from payload into delivery.
func NormalizeLegacyDeliveryInputMap(delivery, payload map[string]any) LegacyDeliveryInputResult {
	if payload == nil || !HasLegacyDeliveryHints(payload) {
		return LegacyDeliveryInputResult{Delivery: delivery, Mutated: false}
	}

	var result LegacyDeliveryInputResult
	if delivery != nil {
		merged, mutated := MergeLegacyDeliveryInto(delivery, payload)
		result = LegacyDeliveryInputResult{Delivery: merged, Mutated: mutated}
	} else {
		result = LegacyDeliveryInputResult{
			Delivery: BuildDeliveryFromLegacyPayload(payload),
			Mutated:  true,
		}
	}

	StripLegacyDeliveryFieldsFromPayload(payload)
	result.Mutated = true
	return result
}

// StripLegacyDeliveryFieldsFromPayload removes legacy delivery fields from payload.
func StripLegacyDeliveryFieldsFromPayload(payload map[string]any) {
	delete(payload, "deliver")
	delete(payload, "channel")
	delete(payload, "provider")
	delete(payload, "to")
	delete(payload, "bestEffortDeliver")
}
