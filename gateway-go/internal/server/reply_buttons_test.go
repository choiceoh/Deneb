package server

import (
	"testing"
)

func TestParseReplyButtons_NoDirective(t *testing.T) {
	text, kb := parseReplyButtons("Hello world")
	if text != "Hello world" {
		t.Fatalf("expected original text, got: %q", text)
	}
	if kb != nil {
		t.Fatal("expected nil keyboard")
	}
}

func TestParseReplyButtons_ValidDirective(t *testing.T) {
	input := `Choose an option:
<!-- buttons: [["Yes|yes","No|no"],["Cancel|cancel"]] -->`

	text, kb := parseReplyButtons(input)

	if text != "Choose an option:" {
		t.Fatalf("unexpected cleaned text: %q", text)
	}
	if kb == nil {
		t.Fatal("expected keyboard")
	}
	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(kb.InlineKeyboard))
	}
	if len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("expected 2 buttons in row 1, got %d", len(kb.InlineKeyboard[0]))
	}
	if kb.InlineKeyboard[0][0].Text != "Yes" {
		t.Fatalf("expected 'Yes', got %q", kb.InlineKeyboard[0][0].Text)
	}
	if kb.InlineKeyboard[0][0].CallbackData != "yes" {
		t.Fatalf("expected 'yes', got %q", kb.InlineKeyboard[0][0].CallbackData)
	}
	if kb.InlineKeyboard[1][0].Text != "Cancel" {
		t.Fatalf("expected 'Cancel', got %q", kb.InlineKeyboard[1][0].Text)
	}
}

func TestParseReplyButtons_LabelOnlyButtons(t *testing.T) {
	input := `Pick one <!-- buttons: [["Option A","Option B"]] -->`

	text, kb := parseReplyButtons(input)

	if text != "Pick one" {
		t.Fatalf("unexpected cleaned text: %q", text)
	}
	if kb == nil {
		t.Fatal("expected keyboard")
	}
	// When no pipe separator, callback_data defaults to label.
	if kb.InlineKeyboard[0][0].CallbackData != "Option A" {
		t.Fatalf("expected callback_data = label, got %q", kb.InlineKeyboard[0][0].CallbackData)
	}
}

func TestParseReplyButtons_InvalidJSON(t *testing.T) {
	input := `text <!-- buttons: not-json -->`

	text, kb := parseReplyButtons(input)

	if text != input {
		t.Fatalf("expected original text on parse failure, got: %q", text)
	}
	if kb != nil {
		t.Fatal("expected nil keyboard on parse failure")
	}
}

func TestParseReplyButtons_EmptyRows(t *testing.T) {
	input := `text <!-- buttons: [[]] -->`

	text, kb := parseReplyButtons(input)

	if text != input {
		t.Fatalf("expected original text when keyboard empty, got: %q", text)
	}
	if kb != nil {
		t.Fatal("expected nil keyboard for empty rows")
	}
}
