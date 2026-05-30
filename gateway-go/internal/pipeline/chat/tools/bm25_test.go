package tools

import (
	"reflect"
	"testing"
)

func TestTokenize(t *testing.T) {
	got := tokenize("Send_and read Email-99!")
	want := []string{"send", "and", "read", "email", "99"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokenize = %v, want %v", got, want)
	}
}

func TestExtractParamNames(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"bucket": map[string]any{"type": "string"},
			"key":    map[string]any{"type": "string"},
		},
	}
	got := extractParamNames(schema)
	set := map[string]bool{}
	for _, n := range got {
		set[n] = true
	}
	if !set["bucket"] || !set["key"] {
		t.Fatalf("extractParamNames = %v, want bucket & key", got)
	}
	if n := extractParamNames(nil); n != nil {
		t.Fatalf("extractParamNames(nil) = %v, want nil", n)
	}
	if n := extractParamNames(map[string]any{"type": "object"}); n != nil {
		t.Fatalf("extractParamNames(no props) = %v, want nil", n)
	}
}

func TestBM25Rank_OrdersByRelevance(t *testing.T) {
	docs := []searchDoc{
		{name: "gmail", tokens: tokenize("gmail email email inbox")},
		{name: "calendar", tokens: tokenize("calendar email events")},
		{name: "process", tokens: tokenize("manage background exec sessions")},
	}
	got := bm25Rank("email", docs)
	want := []string{"gmail", "calendar"} // gmail has higher term frequency
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bm25Rank = %v, want %v", got, want)
	}
}

func TestBM25Rank_NoTokenMatch_ReturnsNil(t *testing.T) {
	docs := []searchDoc{
		{name: "gmail", tokens: tokenize("send and read email")},
	}
	if got := bm25Rank("nonexistent", docs); got != nil {
		t.Fatalf("bm25Rank = %v, want nil (no token match)", got)
	}
}

func TestBM25Rank_EmptyInputs(t *testing.T) {
	if got := bm25Rank("", []searchDoc{{name: "x", tokens: tokenize("y")}}); got != nil {
		t.Fatalf("empty query: got %v, want nil", got)
	}
	if got := bm25Rank("q", nil); got != nil {
		t.Fatalf("empty corpus: got %v, want nil", got)
	}
}
