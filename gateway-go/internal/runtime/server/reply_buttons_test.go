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
		t.Fatalf("got %d, want 2 rows", len(kb.InlineKeyboard))
	}
	if len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("got %d, want 2 buttons in row 1", len(kb.InlineKeyboard[0]))
	}
	if kb.InlineKeyboard[0][0].Text != "Yes" {
		t.Fatalf("got %q, want 'Yes'", kb.InlineKeyboard[0][0].Text)
	}
	if kb.InlineKeyboard[0][0].CallbackData != "yes" {
		t.Fatalf("got %q, want 'yes'", kb.InlineKeyboard[0][0].CallbackData)
	}
	if kb.InlineKeyboard[1][0].Text != "Cancel" {
		t.Fatalf("got %q, want 'Cancel'", kb.InlineKeyboard[1][0].Text)
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
		t.Fatalf("got %q, want callback_data = label", kb.InlineKeyboard[0][0].CallbackData)
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
