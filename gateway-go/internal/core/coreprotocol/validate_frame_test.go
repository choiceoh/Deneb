package coreprotocol

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestValidateFrame_ValidRequest(t *testing.T) {
	err := ValidateFrame(`{"type":"req","id":"abc","method":"chat.send","params":{"text":"hello"}}`)
	testutil.NoError(t, err)
}

func TestValidateFrame_ValidResponse(t *testing.T) {
	err := ValidateFrame(`{"type":"res","id":"abc","ok":true,"payload":{"data":1}}`)
	testutil.NoError(t, err)
}

func TestValidateFrame_ValidEvent(t *testing.T) {
	err := ValidateFrame(`{"type":"event","event":"health","seq":5}`)
	testutil.NoError(t, err)
}

func TestValidateFrame_ResponseWithError(t *testing.T) {
	err := ValidateFrame(`{"type":"res","id":"x","ok":false,"error":{"code":"NOT_FOUND","message":"session not found","retryable":false}}`)
	testutil.NoError(t, err)
}

func TestValidateFrame_MissingMethod(t *testing.T) {
	err := ValidateFrame(`{"type":"req","id":"abc"}`)
	if err == nil {
		t.Fatal("expected error for missing method")
	}
}

func TestValidateFrame_EmptyID(t *testing.T) {
	err := ValidateFrame(`{"type":"req","id":"","method":"test"}`)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestValidateFrame_InvalidJSON(t *testing.T) {
	err := ValidateFrame(`{not json}`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateFrame_NegativeSeq(t *testing.T) {
	err := ValidateFrame(`{"type":"event","event":"health","seq":-1}`)
	if err == nil {
		t.Fatal("expected error for negative seq")
	}
	if !strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("expected non-negative error, got %v", err)
	}
}

func TestValidateFrame_ZeroSeq(t *testing.T) {
	err := ValidateFrame(`{"type":"event","event":"health","seq":0}`)
	testutil.NoError(t, err)
}

func TestValidateFrame_ExtraFieldsIgnored(t *testing.T) {
	err := ValidateFrame(`{"type":"req","id":"1","method":"test","unknown_field":42}`)
	testutil.NoError(t, err)
}

func TestValidateFrame_OversizedID(t *testing.T) {
	longID := strings.Repeat("x", 300)
	err := ValidateFrame(`{"type":"req","id":"` + longID + `","method":"test"}`)
	if err == nil {
		t.Fatal("expected error for oversized id")
	}
	if !strings.Contains(err.Error(), "maximum length") {
		t.Fatalf("expected maximum length error, got %v", err)
	}
}

func TestValidateFrame_OversizedMethod(t *testing.T) {
	longMethod := strings.Repeat("m", 300)
	err := ValidateFrame(`{"type":"req","id":"1","method":"` + longMethod + `"}`)
	if err == nil {
		t.Fatal("expected error for oversized method")
	}
}

func TestValidateFrame_CaseSensitiveType(t *testing.T) {
	err := ValidateFrame(`{"type":"REQ","id":"1","method":"test"}`)
	if err == nil {
		t.Fatal("expected error for uppercase type")
	}
}

func TestValidateFrame_Empty(t *testing.T) {
	err := ValidateFrame("")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestValidateFrame_ResponseMissingOK(t *testing.T) {
	err := ValidateFrame(`{"type":"res","id":"abc"}`)
	if err == nil {
		t.Fatal("expected error for missing ok")
	}
}
