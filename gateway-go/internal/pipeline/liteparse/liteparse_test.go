package liteparse

import (
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
