package llm

// Multimodal wire-serialization coverage: the image content-block types
// (ImageSource base64, ImageURL reference) existed without any test proving
// they survive the per-protocol conversion. These tests pin the exact wire
// shapes so a refactor cannot silently drop image payloads — the failure mode
// would otherwise only surface as a provider-side "empty message" error in
// production (gmail attachment OCR, miniapp image capture).

import (
	"encoding/json"
	"strings"
	"testing"
)

const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func multimodalUserMessage() Message {
	return NewBlockMessage("user", []ContentBlock{
		{Type: "text", Text: "이 이미지를 설명해줘"},
		{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: tinyPNGBase64}},
		{Type: "image_url", ImageURL: &ImageURL{URL: "https://example.com/chart.png", Detail: "high"}},
	})
}

func TestConvertMessagesToOpenAI_ImageBlocks(t *testing.T) {
	client := NewClient("http://localhost", "")
	out := client.convertMessagesToOpenAI([]Message{multimodalUserMessage()}, false)
	if len(out) != 1 {
		t.Fatalf("got %d messages, want 1", len(out))
	}
	parts, ok := out[0].Content.([]openAIContentPart)
	if !ok {
		t.Fatalf("content type = %T, want []openAIContentPart (multipart vision format)", out[0].Content)
	}
	if len(parts) != 3 {
		t.Fatalf("got %d parts, want text + 2 images", len(parts))
	}
	// Text must come first so the prompt precedes its images.
	if parts[0].Type != "text" || parts[0].Text != "이 이미지를 설명해줘" {
		t.Errorf("parts[0] = %+v, want the text part", parts[0])
	}
	// Anthropic-style base64 block → OpenAI data-URI image_url.
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil {
		t.Fatalf("parts[1] = %+v, want image_url part", parts[1])
	}
	if want := "data:image/png;base64," + tinyPNGBase64; parts[1].ImageURL.URL != want {
		t.Errorf("data URI = %q, want %q", parts[1].ImageURL.URL, want)
	}
	// image_url block passes through with its detail hint.
	if parts[2].ImageURL == nil || parts[2].ImageURL.URL != "https://example.com/chart.png" || parts[2].ImageURL.Detail != "high" {
		t.Errorf("parts[2] = %+v, want URL passthrough with detail", parts[2])
	}
}

func TestConvertMessagesToOpenAI_EmptyImageSourceDropped(t *testing.T) {
	client := NewClient("http://localhost", "")
	msg := NewBlockMessage("user", []ContentBlock{
		{Type: "text", Text: "hi"},
		{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: ""}},
	})
	out := client.convertMessagesToOpenAI([]Message{msg}, false)
	if len(out) != 1 {
		t.Fatalf("got %d messages, want 1", len(out))
	}
	// An image block with no data must not produce a broken data URI; the
	// message degrades to plain text.
	if s, ok := out[0].Content.(string); !ok || s != "hi" {
		t.Errorf("content = %#v, want plain string %q", out[0].Content, "hi")
	}
}

func TestBuildAnthropicRequestBody_ImageBlocks(t *testing.T) {
	body, err := buildAnthropicRequestBody(ChatRequest{
		Model:     "claude-test",
		MaxTokens: 64,
		Messages:  []Message{multimodalUserMessage()},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type   string       `json:"type"`
				Source *ImageSource `json:"source"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(req.Messages))
	}
	content := req.Messages[0].Content
	if len(content) != 3 {
		t.Fatalf("got %d blocks, want 3 (text + image + image_url)", len(content))
	}
	// The base64 image source must survive sanitizeAnthropicContent verbatim.
	img := content[1]
	if img.Type != "image" || img.Source == nil {
		t.Fatalf("block[1] = %+v, want image block with source", img)
	}
	if img.Source.Type != "base64" || img.Source.MediaType != "image/png" || img.Source.Data != tinyPNGBase64 {
		t.Errorf("image source = %+v, want base64 png payload preserved", img.Source)
	}
}

func TestBuildAnthropicRequestBody_ImagePayloadOnWire(t *testing.T) {
	// Belt-and-braces: assert the raw bytes carry the payload, independent of
	// the decode-shape used above.
	body, err := buildAnthropicRequestBody(ChatRequest{
		Model:     "claude-test",
		MaxTokens: 64,
		Messages: []Message{NewBlockMessage("user", []ContentBlock{
			{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/jpeg", Data: tinyPNGBase64}},
		})},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, want := range []string{`"media_type":"image/jpeg"`, tinyPNGBase64} {
		if !strings.Contains(string(body), want) {
			t.Errorf("wire body missing %q", want)
		}
	}
}
