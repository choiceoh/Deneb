package chat

import (
	"encoding/json"
	"sort"
)

// harnessArgKeys are top-level keys Deneb's own tool layer consumes before the
// tool sees the input, so they are legitimately absent from every tool schema.
var harnessArgKeys = map[string]bool{
	"compress": true, // extractCompressFlag
	"$ref":     true, // resolveRef
}

// unknownToolArgKeys returns the input's top-level keys that the tool's schema
// does not declare, sorted for stable logging. Empty when the schema declares
// nothing (or accepts anything), when the input is not a JSON object, or when
// every key is known.
//
// This is MEASUREMENT ONLY: an unknown key is not an error and nothing is
// rejected. Today a hallucinated argument is silently dropped and the tool
// answers as if no filter had been asked for — measured 2026-08-25 in puppet
// mode, where calendar(action="list", range="today") returned the default
// 48-hour window and calendar(..., persno="김대표") returned every event, with
// neither result saying the argument went nowhere. Telling the model belongs in
// a later step, deliberately gated on this counter first (the discipline
// tool_argrepair.go already applies to schema-aware repairs).
//
// Only key NAMES are ever returned — argument values may hold user content.
func unknownToolArgKeys(schema map[string]any, input json.RawMessage) []string {
	if len(schema) == 0 || len(input) == 0 {
		return nil
	}
	if extra, ok := schema["additionalProperties"].(bool); ok && extra {
		return nil
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(input, &fields) != nil {
		return nil
	}
	var unknown []string
	for key := range fields {
		if _, declared := properties[key]; declared || harnessArgKeys[key] {
			continue
		}
		unknown = append(unknown, key)
	}
	sort.Strings(unknown)
	return unknown
}
