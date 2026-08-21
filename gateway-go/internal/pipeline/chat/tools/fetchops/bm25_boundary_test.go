package fetchops

import (
	"reflect"
	"sort"
	"testing"
)

func TestBoundaryTokenizeUnicodeAndSeparators(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "empty", text: "", want: []string{}},
		{name: "spaces", text: "   ", want: []string{}},
		{name: "lowercase", text: "Fetch Tools", want: []string{"fetch", "tools"}},
		{name: "underscore splits", text: "fetch_tools", want: []string{"fetch", "tools"}},
		{name: "hyphen splits", text: "mail-archive", want: []string{"mail", "archive"}},
		{name: "slash splits", text: "wiki/read", want: []string{"wiki", "read"}},
		{name: "digits retained", text: "model 2026 q3", want: []string{"model", "2026", "q3"}},
		{name: "Korean retained", text: "메일 보관함 검색", want: []string{"메일", "보관함", "검색"}},
		{name: "mixed scripts", text: "Gmail 메일-search", want: []string{"gmail", "메일", "search"}},
		{name: "emoji splits", text: "mail📎archive", want: []string{"mail", "archive"}},
		{name: "punctuation run", text: "one...two,,,three", want: []string{"one", "two", "three"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tokenize(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tokenize(%q) = %#v, want %#v", tt.text, got, tt.want)
			}
		})
	}
}

func TestBoundaryExtractParamNamesSchemaShapes(t *testing.T) {
	tests := []struct {
		name   string
		schema map[string]any
		want   []string
	}{
		{name: "nil", schema: nil, want: nil},
		{name: "empty", schema: map[string]any{}, want: nil},
		{name: "missing properties", schema: map[string]any{"type": "object"}, want: nil},
		{name: "nil properties", schema: map[string]any{"properties": nil}, want: nil},
		{name: "wrong properties type", schema: map[string]any{"properties": []any{}}, want: nil},
		{name: "empty properties", schema: map[string]any{"properties": map[string]any{}}, want: []string{}},
		{name: "several properties", schema: map[string]any{"properties": map[string]any{"query": map[string]any{}, "limit": map[string]any{}, "include_body": map[string]any{}}}, want: []string{"include_body", "limit", "query"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractParamNames(tt.schema)
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("extractParamNames = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBoundaryBM25RankRelevanceAndDegenerateCases(t *testing.T) {
	docs := []searchDoc{
		{name: "mail_archive", tokens: tokenize("mail archive search mailbox thread")},
		{name: "wiki", tokens: tokenize("wiki page search read write")},
		{name: "calendar", tokens: tokenize("calendar event schedule search")},
		{name: "generic", tokens: tokenize("search search search")},
	}
	tests := []struct {
		name      string
		query     string
		wantFirst string
		wantNil   bool
	}{
		{name: "empty query", query: "", wantNil: true},
		{name: "punctuation query", query: "!!!", wantNil: true},
		{name: "unknown query", query: "spaceship", wantNil: true},
		{name: "mail exact", query: "mail archive", wantFirst: "mail_archive"},
		{name: "wiki exact", query: "wiki page", wantFirst: "wiki"},
		{name: "calendar exact", query: "calendar event", wantFirst: "calendar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bm25Rank(tt.query, docs)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("bm25Rank = %v, want nil", got)
				}
				return
			}
			if len(got) == 0 || got[0] != tt.wantFirst {
				t.Fatalf("bm25Rank = %v, want first %q", got, tt.wantFirst)
			}
		})
	}
	if got := bm25Rank("mail", nil); got != nil {
		t.Fatalf("nil docs = %v", got)
	}
}
