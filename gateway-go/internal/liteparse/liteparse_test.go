package liteparse

import (
	"context"
	"testing"
)

func TestSupportedMIME(t *testing.T) {
	tests := []struct {
		mime string
		want bool
	}{
		// Supported types.
		{"application/pdf", true},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", true},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true},
		{"application/vnd.openxmlformats-officedocument.presentationml.presentation", true},
		{"application/msword", true},
		{"application/vnd.ms-excel", true},
		{"application/vnd.ms-powerpoint", true},
		{"application/vnd.oasis.opendocument.text", true},
		{"application/vnd.oasis.opendocument.spreadsheet", true},
		{"text/csv", true},
		// Case insensitive.
		{"Application/PDF", true},
		{"APPLICATION/VND.OPENXMLFORMATS-OFFICEDOCUMENT.WORDPROCESSINGML.DOCUMENT", true},
		// Not supported.
		{"image/png", false},
		{"image/jpeg", false},
		{"text/html", false},
		{"application/json", false},
		{"text/plain", false},
		{"application/zip", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := SupportedMIME(tt.mime); got != tt.want {
			t.Errorf("SupportedMIME(%q) = %v, want %v", tt.mime, got, tt.want)
		}
	}
}

func TestParse_EmptyData(t *testing.T) {
	_, err := Parse(context.Background(), nil, "test.pdf")
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestParse_OversizedData(t *testing.T) {
	data := make([]byte, maxDocumentSize+1)
	_, err := Parse(context.Background(), data, "big.pdf")
	if err == nil {
		t.Fatal("expected error for oversized data")
	}
}
