package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestModelAcceptsImages_ReturnsCapabilityByModelName(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		// GLM text models — exact match, including provider prefixes and case.
		{"glm-5.2", false},
		{"zai/glm-5.2", false},
		{"GLM-5.2", false},
		{"glm-4.6-air", false},
		// GLM vision variants share a prefix with the text ones — must pass.
		{"glm-4.6v", true},
		{"glm-4v-plus", true},
		// DeepSeek: family prefix, no vision chat models…
		{"deepseek-v4-flash", false},
		{"org/deepseek-chat", false},
		// …except the VL family.
		{"deepseek-vl-7b", true},
		// Unknown models default to image-capable.
		{"gemini-3.5-flash", true},
		{"claude-opus-4-8", true},
	}
	for _, c := range cases {
		if got := modelAcceptsImages(c.model); got != c.want {
			t.Errorf("modelAcceptsImages(%q) = %v, want %v", c.model, got, c.want)
		}
	}
}

func TestEntryAcceptsImages_OverrideWithBuiltinFallback(t *testing.T) {
	tr, fa := true, false
	// Override wins over the builtin table in both directions.
	if !entryAcceptsImages(modelEntry{UpstreamModel: "glm-5.2", Vision: &tr}) {
		t.Error("vision:true override should pass images through a builtin text-only model")
	}
	if entryAcceptsImages(modelEntry{UpstreamModel: "gemini-3.5-flash", Vision: &fa}) {
		t.Error("vision:false override should strip images from a builtin-unknown model")
	}
	// Nil override falls back to the builtin table keyed by upstream id.
	if entryAcceptsImages(modelEntry{Name: "main", UpstreamModel: "glm-5.2"}) {
		t.Error("glm-5.2 upstream should be text-only via the builtin table")
	}
}

func TestStripImageParts_OpenAI_PreservesTextAndFields(t *testing.T) {
	body := []byte(`{"model":"glm-5.2","temperature":0.30,"messages":[` +
		`{"role":"user","content":[{"type":"text","text":"이 사진 뭐야"},` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},` +
		`{"role":"assistant","content":"본 적 없는 사진입니다"}]}`)
	out, n := stripImageParts(body, protocolOpenAI)
	if n != 1 {
		t.Fatalf("stripped %d parts, want 1", n)
	}
	s := string(out)
	if strings.Contains(s, "image_url") {
		t.Errorf("image part survived: %s", s)
	}
	if !strings.Contains(s, strippedImageStub) {
		t.Errorf("stub missing: %s", s)
	}
	if !strings.Contains(s, "이 사진 뭐야") || !strings.Contains(s, "본 적 없는 사진입니다") {
		t.Errorf("text content lost: %s", s)
	}
	// Untouched raw values survive re-marshaling (RawMessage passthrough).
	if !strings.Contains(s, "0.30") {
		t.Errorf("sibling field bytes reformatted: %s", s)
	}
}

func TestStripImageParts_Anthropic_PreservesTextBlocks(t *testing.T) {
	body := []byte(`{"model":"kimi","messages":[{"role":"user","content":[` +
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}},` +
		`{"type":"text","text":"설명해줘"}]}]}`)
	out, n := stripImageParts(body, protocolAnthropic)
	if n != 1 {
		t.Fatalf("stripped %d parts, want 1", n)
	}
	if strings.Contains(string(out), `"source"`) {
		t.Errorf("image block survived: %s", out)
	}
	if !strings.Contains(string(out), "설명해줘") {
		t.Errorf("text block lost: %s", out)
	}
}

// The APC contract: a request without image parts must come back byte-identical
// — including the fast-path miss and the parse-then-no-op path.
func TestStripImageParts_PreservesBytesWithoutImageParts(t *testing.T) {
	noMarker := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"안녕"}]}`)
	if out, n := stripImageParts(noMarker, protocolOpenAI); n != 0 || !bytes.Equal(out, noMarker) {
		t.Errorf("image-free body mutated (n=%d): %s", n, out)
	}
	// Contains the marker substring in a TEXT value but no actual image part:
	// the parse runs, finds nothing, and must still return the original bytes.
	markerInText := []byte(`{"model":"deepseek-v4-flash","messages":[` +
		`{"role":"user","content":[{"type":"text","text":"the \"image_url\" field explained"}]}]}`)
	if out, n := stripImageParts(markerInText, protocolOpenAI); n != 0 || !bytes.Equal(out, markerInText) {
		t.Errorf("marker-in-text body mutated (n=%d): %s", n, out)
	}
	// Unparseable body fails open, untouched.
	garbage := []byte(`not json "image_url"`)
	if out, n := stripImageParts(garbage, protocolOpenAI); n != 0 || !bytes.Equal(out, garbage) {
		t.Errorf("garbage body mutated (n=%d)", n)
	}
}

func TestApplyVisionGate_StripsOrPreservesByModelCapability(t *testing.T) {
	rt := quietRouter(config{})
	withImage := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`)

	// Text-only upstream → stripped.
	out := rt.applyVisionGate(modelEntry{Name: "main", UpstreamModel: "glm-5.2"}, withImage, protocolOpenAI)
	if strings.Contains(string(out), "image_url") {
		t.Errorf("gate did not strip for glm-5.2: %s", out)
	}
	// Image-capable upstream → byte-identical.
	out = rt.applyVisionGate(modelEntry{Name: "vis", UpstreamModel: "gemini-3.5-flash"}, withImage, protocolOpenAI)
	if !bytes.Equal(out, withImage) {
		t.Errorf("gate mutated an image-capable model's request")
	}
}

// TestModelAcceptsImages_GLM53FamilySplit pins the measured split inside one GLM
// generation: the base model rejects image parts with a hard 400 (which then
// poisons every later turn of that chat), while the flash tier reads them. The
// table must therefore gate glm-5.3 and leave glm-5.3-flash alone — the reverse
// of the glm-4.7 generation, where the flash tier was the text-only one.
// Measured against the coding plan endpoint on 2026-09-02.
func TestModelAcceptsImages_GLM53FamilySplit(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  bool
	}{
		{"glm-5.3", false},
		{"glm5.3", false},
		{"glm-5.3-flash", true},
		{"glm-4.7", false},
		{"glm-4.7-flash", false},
	} {
		if got := modelAcceptsImages(tc.model); got != tc.want {
			t.Errorf("modelAcceptsImages(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}
