// Package llm provides an HTTP client for OpenAI-compatible LLM provider APIs
// with SSE streaming.
package llm

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// ChatRequest represents a streaming chat completion request.
type ChatRequest struct {
	Model            string       `json:"model"`
	Messages         []Message    `json:"messages"`
	System           FlexibleJSON `json:"system,omitempty"` // string or []ContentBlock
	MaxTokens        int          `json:"max_tokens"`
	Tools            []Tool       `json:"tools,omitempty"`
	Stream           bool         `json:"stream"`
	Temperature      *float64     `json:"temperature,omitempty"`
	TopP             *float64     `json:"top_p,omitempty"`
	TopK             *int         `json:"top_k,omitempty"`
	StopSequences    []string     `json:"stop_sequences,omitempty"`
	FrequencyPenalty *float64     `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64     `json:"presence_penalty,omitempty"`
	// Seed requests deterministic sampling from OpenAI-compatible backends that
	// implement the seed extension. Anthropic wire mode deliberately omits it.
	Seed *int64 `json:"seed,omitempty"`

	// ResponseFormat requests structured output.
	// Use &ResponseFormat{Type: "json_object"} for JSON mode.
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`

	// ToolChoice controls tool selection behavior.
	// Values: "auto", "none", "required", or {"type":"function","function":{"name":"..."}}.
	ToolChoice FlexibleJSON `json:"tool_choice,omitempty"`

	// Thinking configures extended thinking (mapped to reasoning_effort for OpenAI).
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// ExtraBody holds additional top-level fields merged into the OpenAI-format
	// request body. Used for provider-specific parameters (e.g., timeout,
	// logit_bias for CJK blocking). Values are pre-serialized JSON.
	ExtraBody map[string]FlexibleJSON `json:"-"`

	// BetaHeaders are values to send via the `anthropic-beta` HTTP header
	// (comma-joined). Used by Anthropic-direct and OpenRouter-proxied
	// providers to opt in to beta features such as interleaved thinking.
	// Other providers ignore the header.
	BetaHeaders []string `json:"-"`
}

// ResponseFormat controls the output format for OpenAI-compatible endpoints.
type ResponseFormat struct {
	Type       string       `json:"type"`                  // "json_object", "json_schema", or "text"
	JSONSchema FlexibleJSON `json:"json_schema,omitempty"` // schema definition when Type="json_schema"
}

// hexChars is used by appendJSONString to encode control characters as \uXXXX.
const hexChars = "0123456789abcdef"

// appendJSONString encodes s as a JSON string and appends it to dst.
// It is equivalent to json.Marshal(s) but avoids the reflection path and
// html-safe escaping of <, >, & that json.Marshal performs by default.
// Valid UTF-8 multi-byte sequences are passed through unchanged (JSON allows
// UTF-8); invalid bytes are replaced with U+FFFD, matching encoding/json, so the
// request body is always valid UTF-8 and a strict provider can't reject it.
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			// ASCII fast path: escape control chars and " / \, pass the rest.
			switch {
			case c == '"':
				dst = append(dst, '\\', '"')
			case c == '\\':
				dst = append(dst, '\\', '\\')
			case c < 0x20:
				// Control character — short form where available, \uXXXX otherwise.
				switch c {
				case '\t':
					dst = append(dst, '\\', 't')
				case '\n':
					dst = append(dst, '\\', 'n')
				case '\r':
					dst = append(dst, '\\', 'r')
				case '\b':
					dst = append(dst, '\\', 'b')
				case '\f':
					dst = append(dst, '\\', 'f')
				default:
					dst = append(dst, '\\', 'u', '0', '0', hexChars[c>>4], hexChars[c&0x0f])
				}
			default:
				dst = append(dst, c)
			}
			i++
			continue
		}
		// Multi-byte: keep a valid UTF-8 rune verbatim; replace an invalid byte
		// with U+FFFD (as encoding/json does) instead of emitting it raw, which
		// would put invalid UTF-8 on the wire.
		_, size := utf8.DecodeRuneInString(s[i:])
		if size == 1 {
			// Invalid encoding (DecodeRuneInString returns RuneError, size 1).
			dst = append(dst, "�"...)
			i++
			continue
		}
		dst = append(dst, s[i:i+size]...)
		i += size
	}
	dst = append(dst, '"')
	return dst
}

// SystemString is a convenience for setting a plain string system prompt.
func SystemString(s string) FlexibleJSON {
	if s == "" {
		return FlexibleJSON{}
	}
	return FlexibleFromValue(s)
}

// SystemBlocks is a convenience for setting an array-of-blocks system prompt.
func SystemBlocks(blocks []ContentBlock) FlexibleJSON {
	if len(blocks) == 0 {
		return FlexibleJSON{}
	}
	return FlexibleFromValue(blocks)
}

// ExtractSystemText extracts a plain text string from the System field,
// whether it's stored as a JSON string or an array of content blocks.
func ExtractSystemText(system FlexibleJSON) string {
	if system.IsZero() {
		return ""
	}
	raw := system.Bytes()
	// Try plain string first.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Try array of content blocks.
	var blocks []ContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	return ""
}

// AppendSystemText appends additional text to the system prompt.
// Handles both JSON string and []ContentBlock formats.
func AppendSystemText(system FlexibleJSON, addition string) FlexibleJSON {
	if addition == "" {
		return system
	}
	if system.IsZero() {
		return SystemString(addition)
	}
	raw := system.Bytes()
	// Try plain string first.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return SystemString(s + "\n\n" + addition)
	}
	// Try array of content blocks — append as new text block.
	var blocks []ContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		blocks = append(blocks, ContentBlock{Type: "text", Text: "\n\n" + addition})
		return SystemBlocks(blocks)
	}
	return system
}

// AppendSystemTexts appends multiple text additions to the system prompt in a single
// unmarshal/marshal cycle. Empty additions are ignored. This is more efficient than
// calling AppendSystemText repeatedly when multiple additions are known upfront.
func AppendSystemTexts(system FlexibleJSON, additions ...string) FlexibleJSON {
	// Collect non-empty additions.
	filtered := additions[:0:0]
	for _, a := range additions {
		if a != "" {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) == 0 {
		return system
	}
	if system.IsZero() {
		var sb strings.Builder
		for i, a := range filtered {
			if i > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(a)
		}
		return SystemString(sb.String())
	}
	raw := system.Bytes()
	// Try plain string — unmarshal once, build combined string, marshal once.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		var sb strings.Builder
		sb.WriteString(s)
		for _, a := range filtered {
			sb.WriteString("\n\n")
			sb.WriteString(a)
		}
		return SystemString(sb.String())
	}
	// Try array of content blocks — unmarshal once, append blocks, marshal once.
	var blocks []ContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		for _, a := range filtered {
			blocks = append(blocks, ContentBlock{Type: "text", Text: "\n\n" + a})
		}
		return SystemBlocks(blocks)
	}
	return system
}

// ThinkingConfig controls extended thinking (mapped to reasoning_effort for OpenAI-compatible APIs).
type ThinkingConfig struct {
	Type         string `json:"type"`          // "enabled" or "disabled"
	BudgetTokens int    `json:"budget_tokens"` // max tokens for thinking

	// Interleaved enables Anthropic's interleaved-thinking-2025-05-14 beta:
	// the model may emit thinking blocks BETWEEN tool calls in the same
	// turn, and prior thinking blocks are echoed back to the API on
	// subsequent turns so reasoning context persists across tool boundaries.
	// On non-Anthropic providers the beta header is a no-op; the message
	// preservation still allows OpenRouter-proxied reasoning to round-trip
	// via the `reasoning_content` field on assistant messages.
	Interleaved bool `json:"interleaved,omitempty"`

	// TemplateKwarg, for Type "disabled" on dual-mode vLLM models, names the
	// chat_template_kwargs boolean that truly disables the thinking phase
	// (e.g. "thinking" for DeepSeek V4 — see modelcaps.ThinkingToggleKwarg).
	// Set by the chat effort router from the model's capability. Empty means
	// no template toggle: "disabled" then falls back to the per-model
	// reasoning_effort floor in applySamplingParams. Steers request
	// construction only; never serialized to the wire itself.
	TemplateKwarg string `json:"-"`
}

// Message represents a single message in a conversation.
type Message struct {
	Role    string       `json:"role"`
	Content FlexibleJSON `json:"content"` // string or []ContentBlock
}

// NewTextMessage creates a message with a plain text content string.
// Uses appendJSONString to avoid json.Marshal's reflection path and
// html-safe escaping, and to pre-size the allocation from string length.
func NewTextMessage(role, text string) Message {
	// Cap hint avoids len(text)+2 overflowing on absurd inputs; append grows if needed.
	capHint := len(text)
	if capHint <= int(^uint(0)>>1)-2 {
		capHint += 2
	}
	return Message{Role: role, Content: FlexibleFromRaw(appendJSONString(make([]byte, 0, capHint), text))}
}

// NewBlockMessage creates a message with structured content blocks.
// Content is guaranteed to be valid JSON — see marshalBlocks.
func NewBlockMessage(role string, blocks []ContentBlock) Message {
	return Message{Role: role, Content: marshalBlocks(blocks)}
}

// marshalBlocks serializes content blocks, guaranteeing a valid JSON result.
//
// A streamed tool_use block can carry a non-JSON Input fragment — e.g. a model
// emitting whitespace-only or max_tokens-truncated tool arguments. FlexibleJSON
// marshaling fails on such bytes, and a silently ignored error here used to
// leave the whole message with empty (0-byte) Content. Every wire converter
// then dropped that message on every later API call: history loss, per-call
// log spam, and the model repeating the failed call because it never saw it.
// Instead, sanitize the offending Input and retry so the turn survives.
func marshalBlocks(blocks []ContentBlock) FlexibleJSON {
	raw, err := json.Marshal(blocks)
	if err == nil {
		return FlexibleFromRaw(raw)
	}
	raw, err = json.Marshal(sanitizeBlockInputs(blocks))
	if err == nil {
		return FlexibleFromRaw(raw)
	}
	// Unreachable in practice (Input is the only opaque JSON field on
	// ContentBlock), but never return invalid Content.
	return FlexibleFromRaw([]byte(`[{"type":"text","text":"[unserializable content omitted]"}]`))
}

// sanitizeBlockInputs returns a copy of blocks where every non-empty Input
// that is not valid JSON is wrapped into a valid JSON object preserving the
// raw fragment, so the model can still see what it actually emitted.
func sanitizeBlockInputs(blocks []ContentBlock) []ContentBlock {
	out := make([]ContentBlock, len(blocks))
	copy(out, blocks)
	for i := range out {
		if !out[i].Input.IsZero() && !json.Valid(out[i].Input.Bytes()) {
			wrapped, _ := json.Marshal(map[string]string{
				"_malformed_arguments": out[i].Input.String(),
			})
			out[i].Input = FlexibleFromRaw(wrapped)
		}
	}
	return out
}

// ContentBlock represents a single content block in a message.
type ContentBlock struct {
	Type string `json:"type"` // "text", "tool_use", "tool_result", "image", "thinking"

	// text block
	Text string `json:"text,omitempty"`

	// tool_use block
	ID    string       `json:"id,omitempty"`
	Name  string       `json:"name,omitempty"`
	Input FlexibleJSON `json:"input,omitempty"`

	// tool_result block
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// Metadata is the code-only sideband attached by the executor from the
	// call's toolmeta collector (pkg/toolmeta). Transcript-persistent and
	// compaction-surviving (stubs replace only Content), but NEVER sent to a
	// provider: both wire paths project blocks through explicit structs
	// (marshalAnthropicBlocks' wireBlock, the OpenAI tool-message conversion)
	// that must not copy this field.
	Metadata FlexibleJSON `json:"metadata,omitempty"`

	// image block (base64 inline)
	Source *ImageSource `json:"source,omitempty"`

	// image_url block (URL reference)
	ImageURL *ImageURL `json:"image_url,omitempty"`

	// thinking block (extended thinking / reasoning content)
	Thinking string `json:"thinking,omitempty"`

	// Signature is the cryptographic signature attached to thinking blocks
	// by Anthropic-compatible endpoints. Required when echoing prior
	// thinking blocks back to the API on subsequent turns.
	Signature string `json:"signature,omitempty"`

	// Data carries the encrypted payload of a redacted_thinking block.
	// Round-tripped verbatim so a redacted block in an assistant turn can be
	// echoed back on later turns (Anthropic rejects one without its data).
	Data string `json:"data,omitempty"`

	// Cache control for prompt caching.
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ImageSource is an inline image (base64).
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", "image/jpeg", etc.
	Data      string `json:"data"`       // base64-encoded image data
}

// ImageURL is an image reference (URL or data URI).
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// CacheControl marks a content block or tool for prompt caching.
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// toolInputSchema is an unexported map alias so Tool's exported declaration
// does not carry map[string]any into Health Bench dynamic-contract counts.
type toolInputSchema map[string]any

// Tool describes a tool available to the model.
//
// RawInputSchema holds the pre-serialized JSON schema used during API request
// marshaling. Prefer setting it via FlexibleFromValue / FlexibleFromRaw.
// PreSerialize fills RawInputSchema from an internally stored programmatic
// schema (same package / tests only) when RawInputSchema is still zero.
type Tool struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	inputSchema    toolInputSchema `json:"-"`                       // programmatic; excluded from JSON
	RawInputSchema FlexibleJSON    `json:"input_schema"`            // pre-serialized; used in API requests
	CacheControl   *CacheControl   `json:"cache_control,omitempty"` // prompt caching
}

// PreSerialize computes RawInputSchema from the programmatic schema if not
// already set. Called automatically when building wire requests.
func (t *Tool) PreSerialize() {
	if t.inputSchema != nil && t.RawInputSchema.IsZero() {
		t.RawInputSchema = FlexibleFromValue(t.inputSchema) // best-effort: known-good schema
	}
}

// StreamEvent represents a single server-sent event from the LLM API.
type StreamEvent struct {
	Type    string       `json:"type"`
	Payload FlexibleJSON `json:"payload,omitempty"`
}

// --- Streaming event payload types ---

// MessageStart is the payload for "message_start" events. The cache fields
// are reported by Anthropic-compatible endpoints that participate in prompt
// caching; absent fields decode to zero so non-cache providers are safe.
type MessageStart struct {
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
		} `json:"usage"`
	} `json:"message"`
}

// ContentBlockStart is the payload for "content_block_start" events.
type ContentBlockStart struct {
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

// ContentBlockDelta is the payload for "content_block_delta" events.
type ContentBlockDelta struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"` // "text_delta", "input_json_delta", "thinking_delta", "signature_delta"
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
		// Anthropic-native fields. For thinking_delta the text is in
		// `thinking` (not `text`); for signature_delta it is in `signature`.
		Thinking  string `json:"thinking,omitempty"`
		Signature string `json:"signature,omitempty"`
	} `json:"delta"`
}

// ContentBlockStop is the payload for "content_block_stop" events.
type ContentBlockStop struct {
	Index int `json:"index"`
}

// MessageDelta is the payload for "message_delta" events. Some Anthropic
// endpoints report final cache totals here instead of (or in addition to)
// message_start; we accept them in either place and only overwrite when
// non-zero so an early message_start total is not clobbered by a missing
// message_delta value.
type MessageDelta struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	} `json:"usage"`
}

// TokenUsage tracks token consumption for a request. The cache fields are
// populated for Anthropic-compatible providers that report prompt-cache hits
// in their `usage` block (cache_read_input_tokens, cache_creation_input_tokens).
// Non-Anthropic providers leave them at zero. These are the single source of
// truth for proving the system_and_3 cache strategy works end-to-end — they
// surface in the per-turn and per-run logs so a tail of the gateway log
// directly answers "are we hitting the prompt cache?".
type TokenUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}
