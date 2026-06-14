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
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	ares "github.com/choiceoh/deneb/gateway-go/internal/ai/router"
)

// noEffortRouting reports whether the caller opted OUT of wormhole's effort
// routing for this request (header X-Wormhole-No-Effort). A "smart" client that
// already does its own thinking control — the Deneb gateway, whose pipeline runs
// Ares per turn — sends this so wormhole doesn't re-run the classifier and
// overwrite its decision (which would also break the gateway's vLLM prefix cache).
// A "dumb" external client (Claude Code, a script) omits it and gets effort
// routing for free.
func noEffortRouting(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("X-Wormhole-No-Effort"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

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
// body in a single parse: the last user message's text, whether it carries
// attachments, and the reconstructed History so the context-heavy check (Ares
// decision #3) can fire. Without History a short follow-up ("계속해줘") steering a
// thread already deep in tool work would wrongly route off — the current message
// alone looks trivial; only h_t reveals the thread is mid-work.
func effortRequest(body []byte) ares.Request {
	var p struct {
		Messages []struct {
			Role      string          `json:"role"`
			Content   json.RawMessage `json:"content"`
			ToolCalls json.RawMessage `json:"tool_calls"` // OpenAI: assistant tool call(s)
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &p)
	var msg string
	var hasAttach bool
	history := make([]llm.Message, 0, len(p.Messages))
	for _, m := range p.Messages {
		if m.Role == "user" {
			text, attach := contentText(m.Content)
			msg, hasAttach = text, attach // last user message wins
		}
		history = append(history, effortMessage(m.Role, m.Content, m.ToolCalls))
	}
	return ares.Request{Message: msg, HasAttachments: hasAttach, History: history}
}

// effortMessage maps one wire message onto an llm.Message for recentContextHeavy,
// which reads only block TYPE (tool_use/tool_result mark a working thread) and
// assistant text length. Anthropic content is already a block array
// (ContentToBlocks parses it natively) and a plain string becomes a text block —
// both pass through verbatim. OpenAI keeps tool activity OUTSIDE content, so an
// assistant `tool_calls` array and a `role:"tool"` result are appended as
// payload-free activity markers (only their type is read).
func effortMessage(role string, content, toolCalls json.RawMessage) llm.Message {
	var extra []llm.ContentBlock
	if hasJSONValue(toolCalls) {
		extra = append(extra, llm.ContentBlock{Type: "tool_use"})
	}
	if role == "tool" {
		extra = append(extra, llm.ContentBlock{Type: "tool_result"})
	}
	if len(extra) > 0 {
		if enc, err := json.Marshal(append(llm.ContentToBlocks(content), extra...)); err == nil {
			return llm.Message{Role: role, Content: enc}
		}
	}
	return llm.Message{Role: role, Content: content}
}

// hasJSONValue reports whether raw is a present, non-null JSON value (an OpenAI
// assistant message without tool calls carries `null` or omits the field).
func hasJSONValue(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && !bytes.Equal(t, []byte("null"))
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
