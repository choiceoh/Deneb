package wiki

import (
	"context"
	"fmt"
	"time"
)

type SearchProbeStatus struct {
	Attempted  bool   `json:"attempted"`
	Healthy    bool   `json:"healthy"`
	Dimensions int    `json:"dimensions,omitempty"`
	LatencyMS  int64  `json:"latencyMs,omitempty"`
	Error      string `json:"error,omitempty"`
}

type RerankerStatus struct {
	Enabled   bool   `json:"enabled"`
	Healthy   bool   `json:"healthy"`
	Identity  string `json:"identity,omitempty"`
	LatencyMS int64  `json:"latencyMs,omitempty"`
	Error     string `json:"error,omitempty"`
}

type SearchDoctorReport struct {
	Healthy             bool                 `json:"healthy"`
	LexicalDocuments    int                  `json:"lexicalDocuments"`
	FactProjection      FactProjectionStatus `json:"factProjection"`
	Semantic            SemanticIndexStatus  `json:"semantic"`
	SemanticProbe       SearchProbeStatus    `json:"semanticProbe"`
	Reranker            RerankerStatus       `json:"reranker"`
	Chunking            string               `json:"chunking"`
	SupportedStructures []string             `json:"supportedStructures"`
	Recommendations     []string             `json:"recommendations,omitempty"`
}

// SearchDoctor verifies the live model contracts in addition to inspecting
// cache metadata. It is an explicit operator surface, never part of hot search.
func (s *Store) SearchDoctor(ctx context.Context) SearchDoctorReport {
	if s == nil {
		return SearchDoctorReport{Recommendations: []string{"configure_wiki_store"}}
	}
	report := SearchDoctorReport{
		FactProjection:      s.FactProjectionStatus(),
		Semantic:            s.SemanticStatus(),
		Chunking:            semanticPreprocessingVersion,
		SupportedStructures: []string{"markdown", "go", "kotlin", "paragraph-fallback"},
	}
	if s.fts != nil {
		report.LexicalDocuments = s.fts.docCount()
	}
	if s.sem != nil && s.sem.embedder != nil {
		report.SemanticProbe.Attempted = true
		started := time.Now()
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		vectors, err := s.sem.embedder.Embed(probeCtx, []string{"Deneb search doctor probe"})
		cancel()
		report.SemanticProbe.LatencyMS = time.Since(started).Milliseconds()
		if err != nil {
			report.SemanticProbe.Error = err.Error()
		} else if len(vectors) != 1 || len(vectors[0]) == 0 {
			report.SemanticProbe.Error = fmt.Sprintf("expected one non-empty vector, got %d", len(vectors))
		} else {
			report.SemanticProbe.Healthy = true
			report.SemanticProbe.Dimensions = len(vectors[0])
		}
	}
	if s.reranker != nil {
		report.Reranker.Enabled = true
		report.Reranker.Identity = s.reranker.Identity()
		started := time.Now()
		probeCtx, cancel := context.WithTimeout(ctx, rerankTimeout)
		scores, err := s.reranker.Rerank(probeCtx, "search doctor", []string{"search doctor", "unrelated document"})
		cancel()
		report.Reranker.LatencyMS = time.Since(started).Milliseconds()
		if err != nil {
			report.Reranker.Error = err.Error()
		} else if len(scores) != 2 {
			report.Reranker.Error = fmt.Sprintf("expected 2 scores, got %d", len(scores))
		} else {
			report.Reranker.Healthy = true
		}
	}
	switch {
	case report.LexicalDocuments == 0:
		report.Recommendations = append(report.Recommendations, "rebuild_lexical_index")
	case report.Semantic.Enabled && !report.Semantic.Healthy:
		report.Recommendations = append(report.Recommendations, "warm_semantic_index")
	}
	if report.SemanticProbe.Attempted && !report.SemanticProbe.Healthy {
		report.Recommendations = append(report.Recommendations, "check_embedding_service")
	}
	if report.Reranker.Enabled && !report.Reranker.Healthy {
		report.Recommendations = append(report.Recommendations, "check_reranker_service")
	}
	if report.FactProjection.Degraded {
		report.Recommendations = append(report.Recommendations, "repair_fact_projection")
	}
	report.Healthy = report.LexicalDocuments > 0 &&
		!report.FactProjection.Degraded &&
		(!report.Semantic.Enabled || (report.Semantic.Healthy && report.SemanticProbe.Healthy)) &&
		(!report.Reranker.Enabled || report.Reranker.Healthy)
	return report
}
