package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Vision gate — strip image content parts from requests bound for text-only
// models, at the single choke point every model consumer (the Deneb gateway,
// Claude Code, scripts) passes through.
//
// Field lesson vendored from Kai upstream (SimonSchubert/Kai@5d57ea69,
// Apache-2.0): Z.AI serves text-only GLM models next to the multimodal GLM-V
// variants, and sending image content-parts to a text model returns a hard 400
// that then poisons every later turn of the chat — the image stays in the
// client's transcript, so each following request fails the same way. The Deneb
// gateway strips images only when deneb.json marks the model vision:false
// (modelcaps.NoVision has no builtin defaults), so without this gate a missing
// override leaves the poisoning path open for glm-backed roles.
//
// ★ APC 불가침 (docs/agent-rules/sidecar-models.md): the gate only ever rewrites a
// request that actually CONTAINS image parts. A text-only request is forwarded
// with its bytes untouched — a fast substring scan short-circuits first, and
// even after a full parse the original bytes are returned unless a part was
// actually stripped. Image-bearing requests to a text-only model previously
// 400'd, so mutating them cannot split a working prefix-cache family.

// textOnlyImageModels lists model ids (normalized: any "provider/" prefix
// stripped, lower-cased) known to reject image input. Exact match only: the
// GLM vision variants (glm-4.6v, glm-4v-plus, …) share a prefix with the text
// ones, so prefix matching would wrongly strip images from a multimodal model.
// DeepSeek — whose chat API has no vision models at all — is matched by family
// prefix in modelAcceptsImages instead.
var textOnlyImageModels = map[string]bool{
	// GLM / Zhipu text models. The -v / v-plus variants are multimodal and are
	// deliberately absent so images still reach them.
	"glm-4.6":          true,
	"glm-4.6-air":      true,
	"glm-4.5":          true,
	"glm-4.5-air":      true,
	"glm-4.5-air:free": true,
	"glm-4.5-x":        true,
	"glm-4-plus":       true,
	"glm-4-plus-0111":  true,
	"glm-4-air":        true,
	"glm-4-airx":       true,
	"glm-4-long":       true,
	"glm-4-flash":      true,
	"glm-4-32b":        true,
	"glm-z1-airx":      true,
	"glm-z1-air":       true,
	"glm-z1-flash":     true,
	"glm-5":            true,
	"glm5":             true,
	"glm-5.1":          true,
	"glm-5.2":          true,
	"glm5.2":           true,
	"glm-5.2-max":      true,
	"glm-5-turbo":      true,
	"glm-5.3":          true,
	"glm5.3":           true,
	"glm-4.7":          true,
	"glm4.7":           true,
	"zai-glm-4.7":      true,
	"glm-4.7-flash":    true,
	// ★ glm-5.3-flash is deliberately ABSENT: unlike glm-4.7-flash (text-only)
	// and its own base model glm-5.3, the 5.3 flash tier is multimodal. Measured
	// on the coding plan endpoint 2026-09-02: a 1x1 PNG content-part answered
	// "Red" on glm-5.3-flash, while glm-5.3 / glm-5.2 / glm-5.1 / glm-4.7 all
	// returned 400 "messages.content.type is invalid, allowed values: ['text']".
	// Adding it here would strip images from the one GLM entry that can read them.
	"chatglm3-6b": true,
}

// modelAcceptsImages reports whether a model id accepts image input. Defaults
// to true for unknown models — most modern flagship models are multimodal, and
// stripping images from a model that supports them is a worse failure than
// letting the rare unknown text-only model reject them (the same default the
// gateway's modelcaps uses).
func modelAcceptsImages(modelID string) bool {
	key := modelID
	if i := strings.LastIndex(key, "/"); i >= 0 {
		key = key[i+1:]
	}
	key = strings.ToLower(strings.TrimSpace(key))
	// DeepSeek's chat models are all text-only; the lone vision family
	// (DeepSeek-VL) carries "vl" in the id. Prefix matching covers DeepSeek
	// reached through an aggregator and future ids.
	if strings.HasPrefix(key, "deepseek") && !strings.Contains(key, "vl") {
		return false
	}
	return !textOnlyImageModels[key]
}

// entryAcceptsImages resolves an entry's image capability: an explicit config
// override ("vision": true/false) wins; otherwise the builtin table, keyed by
// the UPSTREAM model id (what the backend actually serves).
func entryAcceptsImages(e modelEntry) bool {
	if e.Vision != nil {
		return *e.Vision
	}
	return modelAcceptsImages(e.UpstreamModel)
}

// strippedImageStub replaces a removed image part so the model still sees that
// an attachment existed (mirrors the gateway's image-block stub behavior).
const strippedImageStub = "[이미지 첨부 생략 — 텍스트 전용 모델]"

// applyVisionGate strips image content parts when the entry's upstream model is
// known to reject them. Returns the body unchanged (same bytes) for
// image-capable entries, unparseable bodies, and bodies without image parts.
func (rt *router) applyVisionGate(entry modelEntry, body []byte, proto string) []byte {
	if entryAcceptsImages(entry) {
		return body
	}
	stripped, n := stripImageParts(body, proto)
	if n == 0 {
		return body
	}
	rt.log.Info("vision gate: stripped image parts for text-only model",
		"model", entry.Name, "upstream", entry.UpstreamModel, "parts", n)
	return stripped
}

// stripImageParts replaces every image content part in messages[].content with
// a text stub and returns the rewritten body plus the number of parts replaced.
// n == 0 means the original body was returned byte-identical (no image parts,
// or a shape this parser doesn't recognize — fail open, the upstream will
// surface its own error).
func stripImageParts(body []byte, proto string) ([]byte, int) {
	imgType := "image_url" // OpenAI multi-part content
	if proto == protocolAnthropic {
		imgType = "image" // Anthropic content block
	}
	// Fast path: no image marker anywhere in the body → forward untouched.
	if !bytes.Contains(body, []byte(`"`+imgType+`"`)) {
		return body, 0
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return body, 0
	}
	msgsRaw, ok := fields["messages"]
	if !ok {
		return body, 0
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(msgsRaw, &msgs); err != nil {
		return body, 0
	}
	total := 0
	for i, m := range msgs {
		rewritten, n := stripMessageImages(m, imgType)
		if n > 0 {
			msgs[i] = rewritten
			total += n
		}
	}
	if total == 0 {
		return body, 0
	}
	encMsgs, err := json.Marshal(msgs)
	if err != nil {
		return body, 0
	}
	fields["messages"] = encMsgs
	out, err := json.Marshal(fields)
	if err != nil {
		return body, 0
	}
	return out, total
}

// stripMessageImages rewrites one message, replacing image parts in an
// array-shaped content with text stubs. Plain string content can't carry an
// image part and is left untouched.
func stripMessageImages(msg json.RawMessage, imgType string) (json.RawMessage, int) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(msg, &m); err != nil {
		return msg, 0
	}
	contentRaw, ok := m["content"]
	if !ok {
		return msg, 0
	}
	trimmed := bytes.TrimSpace(contentRaw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return msg, 0
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(contentRaw, &parts); err != nil {
		return msg, 0
	}
	n := 0
	out := make([]json.RawMessage, 0, len(parts))
	for _, p := range parts {
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(p, &probe) == nil && probe.Type == imgType {
			stub, err := json.Marshal(map[string]string{"type": "text", "text": strippedImageStub})
			if err != nil {
				return msg, 0
			}
			out = append(out, stub)
			n++
			continue
		}
		out = append(out, p)
	}
	if n == 0 {
		return msg, 0
	}
	encParts, err := json.Marshal(out)
	if err != nil {
		return msg, 0
	}
	m["content"] = encParts
	rewritten, err := json.Marshal(m)
	if err != nil {
		return msg, 0
	}
	return rewritten, n
}
