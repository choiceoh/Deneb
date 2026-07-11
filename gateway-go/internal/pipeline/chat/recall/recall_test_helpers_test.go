package recall

import (
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

type testTranscriptStore struct {
	mu       sync.Mutex
	sessions map[string][]toolctx.ChatMessage
}

func newTestTranscriptStore() *testTranscriptStore {
	return &testTranscriptStore{sessions: make(map[string][]toolctx.ChatMessage)}
}

func (s *testTranscriptStore) Load(sessionKey string, limit int) ([]toolctx.ChatMessage, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.sessions[sessionKey]
	total := len(msgs)
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return append([]toolctx.ChatMessage(nil), msgs...), total, nil
}

func (s *testTranscriptStore) Append(sessionKey string, msg toolctx.ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionKey] = append(s.sessions[sessionKey], msg)
	return nil
}

func (s *testTranscriptStore) Delete(sessionKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionKey)
	return nil
}

func (s *testTranscriptStore) ListKeys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.sessions))
	for key := range s.sessions {
		keys = append(keys, key)
	}
	return keys, nil
}

func (s *testTranscriptStore) Search(query string, maxResults int) ([]toolctx.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	query = strings.ToLower(query)
	var results []toolctx.SearchResult
	remaining := maxResults
	for key, msgs := range s.sessions {
		var matches []toolctx.MatchedMsg
		for i := range msgs {
			if remaining <= 0 {
				break
			}
			if !strings.Contains(strings.ToLower(msgs[i].TextContent()), query) {
				continue
			}
			from, to := i-1, i+2
			if from < 0 {
				from = 0
			}
			if to > len(msgs) {
				to = len(msgs)
			}
			matches = append(matches, toolctx.MatchedMsg{
				Index:   i,
				Message: msgs[i],
				Context: append([]toolctx.ChatMessage(nil), msgs[from:to]...),
			})
			remaining--
		}
		if len(matches) > 0 {
			results = append(results, toolctx.SearchResult{SessionKey: key, Matches: matches})
		}
		if remaining <= 0 {
			break
		}
	}
	return results, nil
}

func (s *testTranscriptStore) CloneRecent(srcKey, dstKey string, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.sessions[srcKey]
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	s.sessions[dstKey] = append([]toolctx.ChatMessage(nil), msgs...)
	return nil
}
