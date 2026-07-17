// quirks_kimi.go — request normalization for the Kimi coding endpoint.
//
// The Kimi subscription endpoint (api.kimi.com/coding/v1, anthropic protocol)
// translates Anthropic-shaped requests into its internal format, and that
// translator has quirks that reject otherwise-legal requests (all proven live,
// 2026-07-17/18, while diagnosing recurring gateway fallbacks):
//
//  1. tool_result blocks must OPEN their user message. A text block merged in
//     front of them ([text, tool_result, …] — e.g. a user steering mid-tool-
//     loop) derails the translator, which then 400s blaming unrelated LATER
//     calls by its internal name:index labels ("tool_call_ids did not have
//     response messages: web:24, web:25").
//  2. Blank text blocks (and blank unsigned thinking blocks) riding a message
//     that also has real blocks → 400 "Invalid request: text content is empty".
//  3. tool_use blocks with null/missing input are rejected; Anthropic requires
//     an object, and picky translators don't backfill.
//
// The Deneb gateway normalizes its own requests (#3896/#3898); this profile
// extends the same protection to every other wormhole consumer (scripts,
// benches, future agents) and keeps the entry self-defending. Applied only to
// entries with "profile": "kimi", anthropic protocol.
package main

import (
	"encoding/json"
	"strings"
)

const profileKimi = "kimi"

// kimiBlockProbe reads only the decision-relevant fields of a content block;
// the block's raw bytes are forwarded untouched unless the block itself needs
// patching (tool_use input backfill).
type kimiBlockProbe struct {
	Type         string          `json:"type"`
	Text         *string         `json:"text"`
	Thinking     *string         `json:"thinking"`
	Signature    string          `json:"signature"`
	Input        json.RawMessage `json:"input"`
	CacheControl json.RawMessage `json:"cache_control"`
}

// blankInert reports whether the block reaches the wire with an empty payload
// and carries no routing/caching information. Signature-bearing thinking blocks
// and cache_control-bearing blocks are never dropped (providers validate
// round-tripped signatures; markers are cache breakpoints).
func (p kimiBlockProbe) blankInert() bool {
	if len(p.CacheControl) != 0 && string(p.CacheControl) != "null" {
		return false
	}
	switch p.Type {
	case "text":
		return p.Text == nil || strings.TrimSpace(*p.Text) == ""
	case "thinking":
		return (p.Thinking == nil || strings.TrimSpace(*p.Thinking) == "") && p.Signature == ""
	}
	return false
}

// applyKimiQuirks normalizes an anthropic-protocol request body for the Kimi
// coding endpoint. Byte-preserving: untouched fields keep their raw bytes, and
// a request that needs no change is returned as the identical slice. Malformed
// or unexpected JSON passes through unchanged — the upstream owns rejection.
func applyKimiQuirks(body []byte) []byte {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return body
	}
	rawMsgs, ok := top["messages"]
	if !ok {
		return body
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(rawMsgs, &msgs); err != nil {
		return body
	}
	changedAny := false
	for i, m := range msgs {
		if out, changed := kimiNormalizeMessage(m); changed {
			msgs[i] = out
			changedAny = true
		}
	}
	if !changedAny {
		return body
	}
	encMsgs, err := json.Marshal(msgs)
	if err != nil {
		return body
	}
	top["messages"] = encMsgs
	out, err := json.Marshal(top)
	if err != nil {
		return body
	}
	return out
}

// kimiNormalizeMessage applies the per-message quirks; reports whether the
// message was rewritten.
func kimiNormalizeMessage(raw json.RawMessage) (json.RawMessage, bool) {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return raw, false
	}
	var role string
	_ = json.Unmarshal(msg["role"], &role)
	rawContent, ok := msg["content"]
	if !ok {
		return raw, false
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(rawContent, &blocks); err != nil {
		return raw, false // plain-string content — nothing block-shaped to fix
	}
	probes := make([]kimiBlockProbe, len(blocks))
	for i, b := range blocks {
		_ = json.Unmarshal(b, &probes[i])
	}

	changed := false

	// Quirk 3: tool_use with null/missing input → {}.
	for i := range blocks {
		if probes[i].Type != "tool_use" {
			continue
		}
		if len(probes[i].Input) != 0 && string(probes[i].Input) != "null" {
			continue
		}
		var bm map[string]json.RawMessage
		if json.Unmarshal(blocks[i], &bm) != nil {
			continue
		}
		bm["input"] = json.RawMessage(`{}`)
		if enc, err := json.Marshal(bm); err == nil {
			blocks[i] = enc
			changed = true
		}
	}

	// Quirk 2: drop blank inert blocks — only when real blocks remain.
	keptBlocks := make([]json.RawMessage, 0, len(blocks))
	keptProbes := make([]kimiBlockProbe, 0, len(probes))
	for i := range blocks {
		if probes[i].blankInert() {
			continue
		}
		keptBlocks = append(keptBlocks, blocks[i])
		keptProbes = append(keptProbes, probes[i])
	}
	if len(keptBlocks) > 0 && len(keptBlocks) < len(blocks) {
		blocks, probes = keptBlocks, keptProbes
		changed = true
	}

	// Quirk 1: user messages — tool_result blocks first, both relative orders
	// preserved.
	if role == "user" && kimiNeedsResultReorder(probes) {
		reordered := make([]json.RawMessage, 0, len(blocks))
		for i := range blocks {
			if probes[i].Type == "tool_result" {
				reordered = append(reordered, blocks[i])
			}
		}
		for i := range blocks {
			if probes[i].Type != "tool_result" {
				reordered = append(reordered, blocks[i])
			}
		}
		blocks = reordered
		changed = true
	}

	if !changed {
		return raw, false
	}
	encBlocks, err := json.Marshal(blocks)
	if err != nil {
		return raw, false
	}
	msg["content"] = encBlocks
	out, err := json.Marshal(msg)
	if err != nil {
		return raw, false
	}
	return out, true
}

// kimiNeedsResultReorder reports whether a tool_result appears after any
// non-tool_result block.
func kimiNeedsResultReorder(probes []kimiBlockProbe) bool {
	seenOther := false
	for _, p := range probes {
		if p.Type == "tool_result" {
			if seenOther {
				return true
			}
			continue
		}
		seenOther = true
	}
	return false
}

// kimiBadRequestHint maps a Kimi 400 error body to a diagnostic hint for the
// operator log. The endpoint's error messages are misleading on their own —
// "web:N" labels are Kimi-internal name:index ordinals, not caller tool ids,
// and usually blame calls far from the actual defect.
func kimiBadRequestHint(errBody []byte) string {
	s := string(errBody)
	switch {
	case strings.Contains(s, "did not have response messages"):
		return "kimi quirk: blamed ids are internal name:index ordinals — real cause is usually a user message whose tool_result blocks are not first, earlier in the history"
	case strings.Contains(s, "text content is empty"):
		return "kimi quirk: a blank text/thinking block reached the wire — normally stripped by the kimi profile; check for cache_control-bearing blank blocks"
	}
	return ""
}
