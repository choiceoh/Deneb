package mailstore

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/rankblend"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
)

const (
	mailRerankCandidateLimit = 10
	mailRerankTimeout        = 800 * time.Millisecond
	mailRerankMaxRunes       = 4000
)

// MailReranker is an optional cross-encoder. It can only reorder candidates
// already admitted by BM25+dense retrieval and is fail-open on every error.
type MailReranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]float64, error)
}

func (s *Store) SetReranker(reranker MailReranker) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.reranker = reranker
	s.mu.Unlock()
}

func (s *Store) rerankerSnapshot() MailReranker {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.reranker
}

// rerankMessages reorders only the first bounded candidate window. The tail
// remains behind it, so unscored retrieval results cannot interleave with the
// cross-encoder order. Retrieval scores remain intact as provenance.
func (s *Store) rerankMessages(ctx context.Context, query string, messages []mailarchive.ContextMessage) []mailarchive.ContextMessage {
	reranker := s.rerankerSnapshot()
	query = strings.TrimSpace(query)
	if ctx == nil || reranker == nil || query == "" || len(messages) < 2 {
		return messages
	}
	count := min(len(messages), mailRerankCandidateLimit)
	documents := make([]string, count)
	for i := range documents {
		documents[i] = mailRerankDocument(messages[i])
	}

	rankCtx, cancel := context.WithTimeout(ctx, mailRerankTimeout)
	defer cancel()
	scores, err := reranker.Rerank(rankCtx, query, documents)
	if err != nil || len(scores) != len(documents) {
		return messages
	}
	retrievalScores := make([]float64, count)
	for i := range retrievalScores {
		retrievalScores[i] = messages[i].Score
	}
	blended, ok := rankblend.Blend(retrievalScores, scores, rankblend.DefaultConfig)
	if !ok {
		return messages
	}
	out := append([]mailarchive.ContextMessage(nil), messages...)
	for rank, index := range blended.Order {
		msg := messages[index]
		msg.RankReasons = append([]string(nil), msg.RankReasons...)
		msg.RankReasons = appendMailRankReason(msg.RankReasons, "rerank")
		out[rank] = msg
	}
	return out
}

func mailRerankDocument(message mailarchive.ContextMessage) string {
	text := strings.TrimSpace(strings.Join(mailarchive.ContextIndexFields(message), "\n"))
	runes := []rune(text)
	if len(runes) > mailRerankMaxRunes {
		text = string(runes[:mailRerankMaxRunes])
	}
	return text
}
