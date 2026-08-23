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

// Thinking-routing directions for modelEntry.ThinkingMode (validated in
// validate.go; "" is the historical judge behavior).
const (
	thinkingModeJudge         = ""                // off only when obviously simple (thinking-on bias)
	thinkingModeOff           = "off"             // always off — the entry is a no-thinking variant
	thinkingModeOffUnlessHard = "off-unless-hard" // off unless clearly hard (thinking-off bias)
)

// applyThinking runs the effort router for one resolved model and logs when it
// turns thinking off (the actionable event; the no-op pass-through stays quiet).
// noEffort is the caller's X-Wormhole-No-Effort opt-out: it suppresses the
// CLASSIFIER modes (judge / off-unless-hard) so a smart client that runs its own
// Ares isn't double-routed — but a static "off" entry applies regardless, because
// the caller picked that entry BY NAME and no-thinking is its contract.
func (rt *router) applyThinking(entry modelEntry, body []byte, noEffort bool) []byte {
	if noEffort && entry.ThinkingMode != thinkingModeOff {
		return body
	}
	out, reason, off := thinkingRoute(body, entry)
	if off {
		rt.log.Info("thinking routed off", "model", entry.Name, "mode", entry.ThinkingMode, "reason", reason)
	}
	return out
}

// Native cloud reasoning dialects (see modelEntry.Reasoning).
const (
	reasoningStyleGLM = "glm" // z.ai / GLM-5.x
	// reasoningStyleDeepseek is the api.deepseek.com dialect. Both API entries
	// have declared it since they were added, but nothing implemented it — the
	// style fell through reasoningRoute's unknown-style branch, so the caller's
	// effort reached DeepSeek untranslated. Measured 2026-08-23 on
	// deepseek-v4-flash: no effort field → 1,072 reasoning chars, and
	// reasoning_effort="low" — the Deneb gateway's OpenAI-path spelling of
	// "thinking disabled" — → 1,932 reasoning chars and finish_reason=length
	// with EMPTY content. Only "none" actually zeroes the channel.
	reasoningStyleDeepseek = "deepseek"
)

// applyReasoning runs the cloud-dialect reasoning router for one resolved model.
// Unlike applyThinking (vLLM chat_template_kwargs — owned by the Deneb gateway and
// suppressed by X-Wormhole-No-Effort), this normalizes a cloud model's NATIVE
// reasoning params: the gateway can't express them, so wormhole is the only place
// the translation can happen and there is no double-routing to suppress. No-op
// unless the entry declares a reasoning style.
func (rt *router) applyReasoning(entry modelEntry, body []byte) []byte {
	if entry.Reasoning == "" {
		return body
	}
	out, reason, off := reasoningRoute(body, entry)
	if off {
		rt.log.Info("reasoning routed off", "model", entry.Name, "style", entry.Reasoning, "reason", reason)
	}
	return out
}

// reasoningRoute classifies the request's effort (same Ares Decide() as the
// thinking toggle) and rewrites a cloud model's reasoning params in its own
// dialect. Currently one style:
//
//	"glm": an obviously-simple turn → thinking:{"type":"disabled"} (and drop any
//	reasoning_effort); otherwise reasoning_effort:"high" + thinking:{"type":"enabled"}.
//	GLM honors only reasoning_effort high|max and treats anything but an explicit
//	"high" as MAX, so the on-path must pin "high" — forwarding the gateway's "low"
//	would silently run GLM at its deepest (max) reasoning.
func reasoningRoute(body []byte, entry modelEntry) (out []byte, reason string, thinkingOff bool) {
	switch entry.Reasoning {
	case reasoningStyleGLM:
	case reasoningStyleDeepseek:
		return deepseekReasoningRoute(body)
	default:
		return body, "", false // unknown/empty style → leave the body untouched
	}
	// Explicit caller intent wins over Ares. The Deneb gateway translates its
	// thinking config to reasoning_effort on the OpenAI path ("low" = thinking
	// disabled, its documented minimal-reasoning floor). Ares used to override
	// that to "high" on every non-simple turn — for the skill evolver's
	// structured-JSON rewrites GLM then burned most of the 12K completion
	// budget in the reasoning channel and the JSON arrived truncated
	// (finish=length, live 2026-07-04). GLM's dialect has no true "low": only
	// high|max are honored and anything else silently means MAX, so a non-high
	// explicit effort maps to thinking OFF (closest to the caller's intent),
	// and an explicit high/max stays high with thinking enabled.
	if eff := getBodyStringField(body, "reasoning_effort"); eff != "" {
		switch strings.ToLower(eff) {
		case "high", "max":
			b := setBodyField(body, "thinking", map[string]string{"type": "enabled"})
			b = setBodyField(b, "reasoning_effort", "high")
			return b, "explicit-" + eff, false
		default:
			b := setBodyField(body, "thinking", map[string]string{"type": "disabled"})
			b = deleteBodyField(b, "reasoning_effort")
			return b, "explicit-" + eff, true
		}
	}
	d := ares.Decide(ares.DefaultProfile(), effortRequest(body))
	if d.ThinkingOff {
		b := setBodyField(body, "thinking", map[string]string{"type": "disabled"})
		b = deleteBodyField(b, "reasoning_effort")
		return b, d.Reason, true
	}
	b := setBodyField(body, "thinking", map[string]string{"type": "enabled"})
	b = setBodyField(b, "reasoning_effort", "high")
	return b, d.Reason, false
}

// deepseekReasoningRoute rewrites the effort for api.deepseek.com.
//
// DeepSeek's scale is none|medium|high (and it accepts "max"), but it does NOT
// read "low" as minimal — that request came back with MORE reasoning than the
// default and no content at all on a small budget. Callers that mean "don't
// think" therefore have to be translated, not forwarded: the Deneb gateway
// spells thinking-disabled as reasoning_effort="low" on the OpenAI path, and
// the vLLM-flavored chat_template_kwargs a caller aimed at a local Qwen/DeepSeek
// serving means nothing here either.
//
// This matters most on failover. A request for a local model that is down lands
// on this entry with the local model's thinking directive attached, on the small
// output budget its caller sized for a non-thinking helper — which is exactly
// how the wiki query expander returned empty content on 46% of its calls while
// the local qwen serving was down.
func deepseekReasoningRoute(body []byte) (out []byte, reason string, thinkingOff bool) {
	// A caller's vLLM toggle is meaningless to the cloud API and would only
	// risk a strict-schema rejection; drop it and speak the native dialect.
	body = deleteBodyField(body, "chat_template_kwargs")
	if eff := strings.ToLower(strings.TrimSpace(getBodyStringField(body, "reasoning_effort"))); eff != "" {
		switch eff {
		case "high", "max", "medium":
			return body, "explicit-" + eff, false
		default:
			// "low"/"none"/anything unrecognized = the caller wants minimal
			// reasoning. Only "none" delivers that here.
			return setBodyField(body, "reasoning_effort", "none"), "explicit-" + eff, true
		}
	}
	if d := ares.Decide(ares.DefaultProfile(), effortRequest(body)); d.ThinkingOff {
		return setBodyField(body, "reasoning_effort", "none"), d.Reason, true
	}
	// Thinking stays on by omission — the API's default — so a heavy turn is
	// unchanged from today.
	return body, "", false
}

// thinkingRoute classifies the request's effort and, for a model with a thinking
// toggle, injects chat_template_kwargs to skip the thinking phase — in the
// direction the entry's ThinkingMode selects. Returns the (possibly modified)
// body and a short reason tag for the log (empty reason = the model has no
// toggle, so nothing was classified).
func thinkingRoute(body []byte, entry modelEntry) (out []byte, reason string, thinkingOff bool) {
	if entry.ToggleKwarg == "" {
		return body, "", false // model has no per-request thinking switch
	}
	switch entry.ThinkingMode {
	case thinkingModeOff:
		// Static no-thinking variant — no classification, always off.
		return injectKwarg(body, entry.ToggleKwarg, false), "mode-off", true
	case thinkingModeOffUnlessHard:
		// Inverted bias: default off; only a CLEAR hardness signal keeps the
		// model's thinking. The ambiguous middle (long, context-heavy) routes
		// off — measured on dsv4: no-think matched think quality on long
		// bounded synthesis at 5× speed, so "long" alone is not evidence of
		// needing the thinking phase.
		d := ares.Decide(ares.DefaultProfile(), effortRequest(body))
		if hardReason(d.Reason) {
			return body, d.Reason, false
		}
		return injectKwarg(body, entry.ToggleKwarg, false), d.Reason, true
	default:
		// Judge with thinking-on bias (historical behavior).
		d := ares.Decide(ares.DefaultProfile(), effortRequest(body))
		if !d.ThinkingOff {
			return body, d.Reason, false // hard/long/structured → keep thinking on
		}
		return injectKwarg(body, entry.ToggleKwarg, false), d.Reason, true
	}
}

// hardReason reports whether an Ares reason tag marks CLEAR hardness — the only
// signals that keep thinking on under the inverted (off-unless-hard) mode:
// an explicit hard cue in the message, an attachment-bearing turn, or
// structured (code/multiline) input.
func hardReason(reason string) bool {
	return reason == "attachments" || reason == "structured" || strings.HasPrefix(reason, "hard-signal:")
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

// setBodyField sets a top-level field to value, preserving every other field's raw
// bytes. Returns the body unchanged if it isn't a JSON object or value won't marshal.
// getBodyStringField returns the string value of a top-level body field, or
// "" when absent/non-string/unparseable.
func getBodyStringField(body []byte, key string) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return ""
	}
	var v string
	if json.Unmarshal(fields[key], &v) != nil {
		return ""
	}
	return v
}

func setBodyField(body []byte, key string, value any) []byte {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return body
	}
	enc, err := json.Marshal(value)
	if err != nil {
		return body
	}
	fields[key] = enc
	out, err := json.Marshal(fields)
	if err != nil {
		return body
	}
	return out
}

// deleteBodyField removes a top-level field, preserving the rest. No-op if absent
// or the body isn't a JSON object.
func deleteBodyField(body []byte, key string) []byte {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return body
	}
	if _, ok := fields[key]; !ok {
		return body
	}
	delete(fields, key)
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
		if enc, err := json.Marshal(append(llm.ContentToBlocks(llm.FlexibleFromRaw(content)), extra...)); err == nil {
			return llm.Message{Role: role, Content: llm.FlexibleFromRaw(enc)}
		}
	}
	return llm.Message{Role: role, Content: llm.FlexibleFromRaw(content)}
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
