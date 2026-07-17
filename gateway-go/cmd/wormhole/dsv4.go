// dsv4.go — DeepSeek V4 profile sampling defaults for the no-thinking path.
package main

import (
	"encoding/json"
	"strings"
)

const (
	dsv4ProfileName      = "dsv4"
	dsv4DefaultTemp      = 0.6
	dsv4DefaultTopP      = 0.95
	kwargReasoningEffort = "reasoning_effort"
)

func isDsv4Profile(entry modelEntry) bool {
	return strings.EqualFold(strings.TrimSpace(entry.Profile), dsv4ProfileName)
}

func profileName(profile string) string {
	if strings.EqualFold(strings.TrimSpace(profile), dsv4ProfileName) {
		return dsv4ProfileName
	}
	return strings.TrimSpace(profile)
}

// applyDsv4Profile injects vendor-recommended sampling when the dsv4 profile is
// active and thinking is off. Thinking mode ignores temperature/top_p per the
// DeepSeek docs, so this is a no-op while thinking stays on.
func applyDsv4Profile(entry modelEntry, body []byte) []byte {
	entry = normalizeEntry(entry)
	if !isDsv4Profile(entry) {
		return body
	}
	if thinkingStaysOn(entry, body) {
		return body
	}
	out := body
	if !bodyHasField(body, "temperature") {
		out = setBodyField(out, "temperature", dsv4DefaultTemp)
	}
	if !bodyHasField(body, "top_p") {
		out = setBodyField(out, "top_p", dsv4DefaultTopP)
	}
	return out
}

func thinkingStaysOn(entry modelEntry, body []byte) bool {
	if entry.ThinkingMode == thinkingModeOff {
		return false
	}
	if entry.ToggleKwarg != "" && kwargBool(body, entry.ToggleKwarg) == falseVal {
		return false
	}
	return true
}

type triBool int

const (
	absentBool triBool = iota
	falseVal
	trueVal
)

func kwargBool(body []byte, key string) triBool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return absentBool
	}
	raw, ok := fields["chat_template_kwargs"]
	if !ok {
		return absentBool
	}
	kwargs := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &kwargs) != nil {
		return absentBool
	}
	valRaw, ok := kwargs[key]
	if !ok {
		return absentBool
	}
	var v bool
	if json.Unmarshal(valRaw, &v) != nil {
		return absentBool
	}
	if v {
		return trueVal
	}
	return falseVal
}

func bodyHasField(body []byte, key string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return false
	}
	_, ok := fields[key]
	return ok
}
