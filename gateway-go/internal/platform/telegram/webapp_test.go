package telegram

import (
	"strings"
	"testing"
)

func TestWebAppKeyboard_Valid(t *testing.T) {
	kb, err := WebAppKeyboard([][]WebAppButton{
		{
			{Text: "Inbox", URL: "https://deneb.example.com/app/#inbox"},
			{Text: "Threads", URL: "https://deneb.example.com/app/#threads"},
		},
		{
			{Text: "Skills", URL: "https://deneb.example.com/app/#skills"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(kb) != 2 || len(kb[0]) != 2 || len(kb[1]) != 1 {
		t.Fatalf("unexpected shape: %v", kb)
	}
	first := kb[0][0]
	if first["text"] != "Inbox" {
		t.Fatalf("text wrong: %v", first)
	}
	wa, ok := first["web_app"].(map[string]any)
	if !ok || wa["url"] != "https://deneb.example.com/app/#inbox" {
		t.Fatalf("web_app payload wrong: %v", first)
	}
}

func TestWebAppKeyboard_Empty(t *testing.T) {
	kb, err := WebAppKeyboard(nil)
	if err != nil {
		t.Fatal(err)
	}
	if kb != nil {
		t.Fatalf("expected nil for empty rows, got %v", kb)
	}
}

func TestWebAppKeyboard_RejectsHTTP(t *testing.T) {
	_, err := WebAppKeyboard([][]WebAppButton{
		{{Text: "App", URL: "http://insecure.example.com/"}},
	})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected https rejection, got %v", err)
	}
}

func TestWebAppKeyboard_RejectsMissingText(t *testing.T) {
	_, err := WebAppKeyboard([][]WebAppButton{
		{{Text: "", URL: "https://example.com/"}},
	})
	if err == nil || !strings.Contains(err.Error(), "missing text") {
		t.Fatalf("expected missing text error, got %v", err)
	}
}

func TestValidateWebAppURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"empty", "", true},
		{"http", "http://example.com/", true},
		{"https", "https://example.com/", false},
		{"https-with-path-and-fragment", "https://example.com/app/#inbox", false},
		{"missing-host", "https:///path", true},
		{"malformed", "://", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWebAppURL(tc.url)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateWebAppURL(%q) err=%v wantErr=%v", tc.url, err, tc.wantErr)
			}
		})
	}
}
