package fetchops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/pkg/vectorutil"
)

type fetchToolSemanticSearch struct {
	embedder embedindex.Embedder

	mu     sync.Mutex
	digest string
	vecs   map[string][]float32
}

func (s *fetchToolSemanticSearch) admissionFloor() float64 {
	if s == nil {
		return embedindex.CalibrationFor(nil, embedindex.SemanticSurfaceFetchTools).Floor
	}
	return embedindex.CalibrationFor(s.embedder, embedindex.SemanticSurfaceFetchTools).Floor
}

type semanticToolHit struct {
	name  string
	score float64
}

func newFetchToolSemanticSearch(embedder embedindex.Embedder) *fetchToolSemanticSearch {
	if embedder == nil {
		return nil
	}
	return &fetchToolSemanticSearch{embedder: embedder}
}

// rank returns semantic ranks and whether dense search completed. Passage
// vectors are cached by the visible, preset-filtered catalog digest; a newly
// discovered MCP tool automatically invalidates the snapshot.
func (s *fetchToolSemanticSearch) rank(ctx context.Context, query string, docs []searchDoc) ([]semanticToolHit, bool) {
	if s == nil || s.embedder == nil || !s.embedder.IsHealthy() || len(docs) == 0 {
		return nil, false
	}
	vecs, ok := s.passageVectors(ctx, docs)
	if !ok {
		return nil, false
	}
	queries, err := embedindex.EmbedQueries(ctx, s.embedder, []string{query})
	if err != nil || len(queries) != 1 {
		return nil, false
	}

	hits := make([]semanticToolHit, 0, len(docs))
	for _, doc := range docs {
		if score := vectorutil.Cosine(queries[0], vecs[doc.name]); score > 0 {
			hits = append(hits, semanticToolHit{name: doc.name, score: score})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].name < hits[j].name
	})
	// Keep the full semantic order through fusion. A lexical top-five result
	// may sit just below the semantic top five; retaining its dense rank lets
	// cross-backend agreement promote it before the final result cap is applied.
	return hits, true
}

func (s *fetchToolSemanticSearch) passageVectors(ctx context.Context, docs []searchDoc) (map[string][]float32, bool) {
	digest := fetchToolCatalogDigest(docs, embedindex.IdentityOf(s.embedder).Fingerprint)
	s.mu.Lock()
	if s.digest == digest && len(s.vecs) == len(docs) {
		vecs := s.vecs
		s.mu.Unlock()
		return vecs, true
	}
	s.mu.Unlock()

	texts := make([]string, len(docs))
	for i := range docs {
		texts[i] = docs[i].semantic
	}
	vectors, err := s.embedder.Embed(ctx, texts)
	if err != nil || len(vectors) != len(docs) {
		return nil, false
	}
	vecs := make(map[string][]float32, len(docs))
	for i, doc := range docs {
		vecs[doc.name] = vectors[i]
	}
	s.mu.Lock()
	s.digest = digest
	s.vecs = vecs
	s.mu.Unlock()
	return vecs, true
}

func fetchToolCatalogDigest(docs []searchDoc, identity string) string {
	var b strings.Builder
	b.WriteString(identity)
	b.WriteByte('\n')
	for _, doc := range docs {
		b.WriteString(doc.name)
		b.WriteByte('\x00')
		b.WriteString(doc.semantic)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16])
}

// fuseFetchToolRanks uses RRF so BM25 and cosine magnitudes never share an
// axis. Lexical hits are always admitted; semantic-only tools need the floor.
func fuseFetchToolRanks(lexical []string, semantic []semanticToolHit) []string {
	return fuseFetchToolRanksLimit(lexical, semantic, searchResultLimit)
}

func fuseFetchToolRanksLimit(lexical []string, semantic []semanticToolHit, limit int) []string {
	floor := embedindex.CalibrationFor(nil, embedindex.SemanticSurfaceFetchTools).Floor
	return fuseFetchToolRanksLimitCalibrated(lexical, semantic, limit, floor)
}

func fuseFetchToolRanksLimitCalibrated(lexical []string, semantic []semanticToolHit, limit int, semanticFloor float64) []string {
	const k = 60.0
	type fused struct {
		name         string
		score        float64
		lexicalRank  int
		semanticRank int
	}
	byName := make(map[string]*fused, len(lexical)+len(semantic))
	for rank, name := range lexical {
		entry := &fused{name: name, lexicalRank: rank + 1}
		entry.score += 1 / (k + float64(rank+1))
		byName[name] = entry
	}
	for rank, hit := range semantic {
		entry := byName[hit.name]
		if entry == nil {
			if hit.score < semanticFloor {
				continue
			}
			entry = &fused{name: hit.name}
			byName[hit.name] = entry
		}
		entry.semanticRank = rank + 1
		entry.score += 1 / (k + float64(rank+1))
	}
	results := make([]fused, 0, len(byName))
	for _, entry := range byName {
		results = append(results, *entry)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		if (results[i].lexicalRank > 0) != (results[j].lexicalRank > 0) {
			return results[i].lexicalRank > 0
		}
		if results[i].lexicalRank != results[j].lexicalRank {
			return semanticNonzeroRank(results[i].lexicalRank) < semanticNonzeroRank(results[j].lexicalRank)
		}
		if results[i].semanticRank != results[j].semanticRank {
			return semanticNonzeroRank(results[i].semanticRank) < semanticNonzeroRank(results[j].semanticRank)
		}
		return results[i].name < results[j].name
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	out := make([]string, len(results))
	for i := range results {
		out[i] = results[i].name
	}
	return out
}

func semanticNonzeroRank(rank int) int {
	if rank == 0 {
		return int(^uint(0) >> 1)
	}
	return rank
}
