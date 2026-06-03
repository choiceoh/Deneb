package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

type fakeSessionTranscript struct {
	searches []string
	results  map[string][]toolctx.SearchResult
}

func (f *fakeSessionTranscript) Load(string, int) ([]toolctx.ChatMessage, int, error) {
	return nil, 0, nil
}

func (f *fakeSessionTranscript) Append(string, toolctx.ChatMessage) error {
	return nil
}

func (f *fakeSessionTranscript) Delete(string) error {
	return nil
}

func (f *fakeSessionTranscript) ListKeys() ([]string, error) {
	return nil, nil
}

func (f *fakeSessionTranscript) Search(query string, _ int) ([]toolctx.SearchResult, error) {
	f.searches = append(f.searches, query)
	return f.results[query], nil
}

func (f *fakeSessionTranscript) CloneRecent(string, string, int) error {
	return nil
}

func sessionSearchJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestToolSessionsSearchExpandsNaturalLanguageQuery(t *testing.T) {
	match := toolctx.MatchedMsg{
		Index:   3,
		Message: toolctx.NewTextChatMessage("assistant", "PR 리뷰 후 체리픽 브랜치를 만들었다", 123),
	}
	transcript := &fakeSessionTranscript{
		results: map[string][]toolctx.SearchResult{
			"pr": {
				{SessionKey: "desktop:abc", Matches: []toolctx.MatchedMsg{match}},
			},
			"체리픽": {
				{SessionKey: "desktop:abc", Matches: []toolctx.MatchedMsg{match}},
			},
		},
	}

	out, err := toolSessionsSearch(transcript)(
		context.Background(),
		sessionSearchJSON(t, map[string]any{"query": "GitHub PR 리뷰하고 체리픽했던 작업 찾아줘"}),
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "via expanded terms") {
		t.Fatalf("expected expanded search output, got: %s", out)
	}
	if !strings.Contains(out, "Found 1 match(es)") {
		t.Fatalf("expected duplicate expanded hits to be merged, got: %s", out)
	}
	if !strings.Contains(out, "desktop:abc") || !strings.Contains(out, "체리픽 브랜치") {
		t.Fatalf("expected session and match text, got: %s", out)
	}
	if len(transcript.searches) < 2 || transcript.searches[0] != "GitHub PR 리뷰하고 체리픽했던 작업 찾아줘" {
		t.Fatalf("expected exact search before expansion, got: %v", transcript.searches)
	}
}
