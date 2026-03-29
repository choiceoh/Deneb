package ffi

import (
	"context"
	"encoding/json"
	"testing"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

var callHandler = rpctest.Call

// ---------------------------------------------------------------------------
// ProtocolMethods
// ---------------------------------------------------------------------------

func TestProtocolMethods_returnsHandlers(t *testing.T) {
	m := ProtocolMethods()
	for _, name := range []string{"protocol.validate", "protocol.validate_params"} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

func TestProtocolValidate_missingFrame(t *testing.T) {
	m := ProtocolMethods()
	resp := callHandler(m, "protocol.validate", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error response for missing frame")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("got error code %v, want ErrMissingParam", resp.Error.Code)
	}
}

func TestProtocolValidate_invalidFramePayload(t *testing.T) {
	m := ProtocolMethods()
	resp := callHandler(m, "protocol.validate", map[string]any{"frame": `{"type":"wat"}`})
	if resp == nil || resp.Error != nil {
		t.Fatalf("expected OK validation payload, got %+v", resp)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["valid"] != false {
		t.Fatalf("expected valid=false, got %v", payload["valid"])
	}
	if _, ok := payload["error"]; !ok {
		t.Fatalf("expected error detail in payload, got %#v", payload)
	}
}

func TestProtocolValidateParams_schemaValidationPath(t *testing.T) {
	m := ProtocolMethods()
	resp := callHandler(m, "protocol.validate_params", map[string]any{
		"method": "sessions.resolve",
		"params": `{"key":"abc"}`,
	})
	if ffipkg.Available {
		if resp == nil || resp.Error != nil {
			t.Fatalf("expected success with FFI available, got %+v", resp)
		}
		var payload map[string]any
		if err := json.Unmarshal(resp.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if _, ok := payload["valid"]; !ok {
			t.Fatalf("expected valid flag, got %#v", payload)
		}
		return
	}

	if resp == nil || resp.Error == nil {
		t.Fatal("expected dependency error without FFI")
	}
	if resp.Error.Code != protocol.ErrDependencyFailed {
		t.Fatalf("got error code %q, want %q", resp.Error.Code, protocol.ErrDependencyFailed)
	}
}

func TestProtocolValidateParams_missingMethod(t *testing.T) {
	m := ProtocolMethods()
	resp := callHandler(m, "protocol.validate_params", map[string]any{"params": "{}"})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error response for missing method")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("got error code %v, want ErrMissingParam", resp.Error.Code)
	}
}

func TestProtocolValidateParams_missingParams(t *testing.T) {
	m := ProtocolMethods()
	resp := callHandler(m, "protocol.validate_params", map[string]any{"method": "sessions.send"})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error response for missing params")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("got error code %v, want ErrMissingParam", resp.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// SecurityMethods
// ---------------------------------------------------------------------------

func TestSecurityMethods_returnsHandlers(t *testing.T) {
	m := SecurityMethods()
	for _, name := range []string{
		"security.validate_session_key",
		"security.sanitize_html",
		"security.is_safe_url",
		"security.validate_error_code",
	} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

func TestSecurityValidateSessionKey_validKey(t *testing.T) {
	m := SecurityMethods()
	resp := callHandler(m, "security.validate_session_key", map[string]any{"key": "my-session-123"})
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp)
	}
}

func TestSecuritySanitizeHTML_invalidParams(t *testing.T) {
	raw := json.RawMessage(`not-json`)
	req := &protocol.RequestFrame{ID: "1", Method: "security.sanitize_html", Params: raw}
	m := SecurityMethods()
	resp := m["security.sanitize_html"](context.Background(), req)
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for invalid JSON params")
	}
}

func TestSecurityValidateErrorCode_unknownCode(t *testing.T) {
	m := SecurityMethods()
	resp := callHandler(m, "security.validate_error_code", map[string]any{"code": "NOT_A_REAL_CODE"})
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["valid"] != false {
		t.Fatalf("expected valid=false for unknown code, got %v", payload["valid"])
	}
}

func TestSecurityValidateErrorCode_invalidParams(t *testing.T) {
	raw := json.RawMessage(`not-json`)
	req := &protocol.RequestFrame{ID: "1", Method: "security.validate_error_code", Params: raw}
	m := SecurityMethods()
	resp := m["security.validate_error_code"](context.Background(), req)
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for invalid JSON params")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("got error code %q, want %q", resp.Error.Code, protocol.ErrInvalidRequest)
	}
}

// ---------------------------------------------------------------------------
// MediaMethods
// ---------------------------------------------------------------------------

func TestMediaMethods_returnsHandlers(t *testing.T) {
	m := MediaMethods()
	if _, ok := m["media.detect_mime"]; !ok {
		t.Error("missing handler media.detect_mime")
	}
}

func TestMediaDetectMIME_invalidParams(t *testing.T) {
	raw := json.RawMessage(`not-json`)
	req := &protocol.RequestFrame{ID: "1", Method: "media.detect_mime", Params: raw}
	m := MediaMethods()
	resp := m["media.detect_mime"](context.Background(), req)
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for invalid JSON params")
	}
}

// ---------------------------------------------------------------------------
// ParsingMethods
// ---------------------------------------------------------------------------

func TestParsingMethods_returnsHandlers(t *testing.T) {
	m := ParsingMethods()
	for _, name := range []string{
		"parsing.extract_links",
		"parsing.html_to_markdown",
		"parsing.base64_estimate",
		"parsing.base64_canonicalize",
		"parsing.media_tokens",
	} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

func TestParsingExtractLinks_invalidParams(t *testing.T) {
	raw := json.RawMessage(`not-json`)
	req := &protocol.RequestFrame{ID: "1", Method: "parsing.extract_links", Params: raw}
	m := ParsingMethods()
	resp := m["parsing.extract_links"](context.Background(), req)
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for invalid JSON params")
	}
}

// ---------------------------------------------------------------------------
// MemoryMethods
// ---------------------------------------------------------------------------

func TestMemoryMethods_returnsHandlers(t *testing.T) {
	m := MemoryMethods()
	for _, name := range []string{
		"memory.cosine_similarity",
		"memory.bm25_rank_to_score",
		"memory.build_fts_query",
		"memory.merge_hybrid_results",
		"memory.extract_keywords",
	} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

func TestMemoryCosineSimilarity_invalidParams(t *testing.T) {
	raw := json.RawMessage(`not-json`)
	req := &protocol.RequestFrame{ID: "1", Method: "memory.cosine_similarity", Params: raw}
	m := MemoryMethods()
	resp := m["memory.cosine_similarity"](context.Background(), req)
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for invalid JSON params")
	}
}

// ---------------------------------------------------------------------------
// MarkdownMethods
// ---------------------------------------------------------------------------

func TestMarkdownMethods_returnsHandlers(t *testing.T) {
	m := MarkdownMethods()
	for _, name := range []string{"markdown.to_ir", "markdown.detect_fences"} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}
