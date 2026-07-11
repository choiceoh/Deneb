package main

import "testing"

func TestParseSince(t *testing.T) {
	zero, err := parseSince("")
	if err != nil || !zero.IsZero() {
		t.Fatalf("empty parse = %v, %v", zero, err)
	}
	got, err := parseSince("2026-07-11")
	if err != nil || got.Format("2006-01-02") != "2026-07-11" {
		t.Fatalf("valid parse = %v, %v", got, err)
	}
	if _, err := parseSince("11-07-2026"); err == nil {
		t.Fatal("invalid date accepted")
	}
}

func TestValidateSource(t *testing.T) {
	for _, source := range []string{"", "imap", "gmail"} {
		if err := validateSource(source); err != nil {
			t.Errorf("validateSource(%q) = %v", source, err)
		}
	}
	if err := validateSource("exchange"); err == nil {
		t.Fatal("unknown source accepted")
	}
}
