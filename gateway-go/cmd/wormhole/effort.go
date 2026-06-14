// effort.go — thinking/non-thinking routing at the proxy. wormhole reuses Deneb's
// Ares effort classifier (internal/ai/router) to decide, per request, whether the
// turn is simple enough to skip the model's thinking phase — and if so injects the
// vLLM chat_template_kwargs toggle that turns thinking off before forwarding. It's
// the SAME Decide() the chat pipeline uses, so any client hitting wormhole (Claude
// Code, scripts) gets the same effort routing without re-implementing it.
//
// Only models that declare a `toggleKwarg` (the boolean that disables their
// thinking phase — "thinking" for DeepSeek V4, "enable_thinking" for Qwen3) are
// routed; everything else passes through untouched. Routing is one-directional:
// it only ever turns thinking OFF for an obviously-simple turn — it never forces
// thinking on — matching the Ares bias that a false-easy costs quality.
package main

import (
	"bytes"
	"encoding/json"
	"strings"

	ares "github.com/choiceoh/deneb/gateway-go/internal/ai/router"
)

// applyThinking runs the effort router for one resolved model and logs when it
// turns thinking off (the actionable event; the no-op pass-through stays quiet).
func (rt *router) applyThinking(entry modelEntry, body []byte) []byte {
	out, reason, off := thinkingRoute(body, entry)
	if off {
		rt.log.Info("thinking routed off", "model", entry.Name, "reason", reason)
	}
	return out
}

// thinkingRoute classifies the request's effort and, for a model with a thinking
// toggle, injects chat_template_kwargs to skip the thinking phase on a simple
// turn. Returns the (possibly modified) body and a short reason tag for the log
// (empty reason = the model has no toggle, so nothing was classified).
func thinkingRoute(body []byte, entry modelEntry) (out []byte, reason string, thinkingOff bool) {
	if entry.ToggleKwarg == "" {
		return body, "", false // model has no per-request thinking switch
	}
	d := ares.Decide(ares.DefaultProfile(), effortRequest(body))
	if !d.ThinkingOff {
		return body, d.Reason, false // hard/long/structured → keep thinking on
	}
	return injectKwarg(body, entry.ToggleKwarg, false), d.Reason, true
}

// injectKwarg sets chat_template_kwargs.<key> = val on the request body, merging
// into any kwargs the client already sent and preserving every other field's raw
// bytes. Returns the original body unchanged if it isn't a JSON object.
func injectKwarg(body []byte, key string, val bool) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return body
	}
	kwargs := map[string]json.RawMessage{}
	if raw, ok := fields["chat_template_kwargs"]; ok {
		_ = json.Unmarshal(raw, &kwargs)
	}
	enc, err := json.Marshal(val)
	if err != nil {
		return body
	}
	kwargs[key] = enc
	kwargsEnc, err := json.Marshal(kwargs)
	if err != nil {
		return body
	}
	fields["chat_template_kwargs"] = kwargsEnc
	out, err := json.Marshal(fields)
	if err != nil {
		return body
	}
	return out
}

// effortRequest builds the classifier input from an OpenAI/Anthropic request
// body: the last user message's text and whether it carries attachments. History
// (multi-turn context-heavy detection) is intentionally left out of this first
// cut — length, structure, hard signals, and attachments carry most of the call.
func effortRequest(body []byte) ares.Request {
	var p struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &p)
	var msg string
	var hasAttach bool
	for _, m := range p.Messages {
		if m.Role != "user" {
			continue
		}
		text, attach := contentText(m.Content)
		msg, hasAttach = text, attach // last user message wins
	}
	return ares.Request{Message: msg, HasAttachments: hasAttach}
}

// contentText extracts the text — and whether any non-text (attachment) part is
// present — from a message's content, which is either a plain string or an array
// of typed parts ({"type":"text",…} / image / document blocks). Covers both the
// OpenAI and Anthropic content shapes.
func contentText(raw json.RawMessage) (text string, hasAttachment bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s, false
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", false
	}
	var sb strings.Builder
	for _, pt := range parts {
		if pt.Type == "text" || pt.Type == "" {
			sb.WriteString(pt.Text)
			sb.WriteByte(' ')
		} else {
			hasAttachment = true
		}
	}
	return strings.TrimSpace(sb.String()), hasAttachment
}
