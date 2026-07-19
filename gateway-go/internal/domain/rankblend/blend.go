// Package rankblend combines retrieval evidence with a cross-encoder's ordinal
// order without assuming that the reranker's raw score scale is calibrated.
package rankblend

import (
	"math"
	"sort"
)

// Config controls how strongly the original retrieval order is preserved.
// Head applies to the first N retrieval candidates; Tail applies afterward.
type Config struct {
	HeadCandidates int
	HeadWeight     float64
	TailWeight     float64
}

var DefaultConfig = Config{
	HeadCandidates: 3,
	HeadWeight:     0.75,
	TailWeight:     0.60,
}

// OrderOnlyConfig gives the reranker a deciding vote when the retrieval
// backend exposes only candidate order, not meaningful score gaps. The initial
// order still contributes enough signal to resist small reranker perturbations.
var OrderOnlyConfig = Config{
	HeadCandidates: 3,
	HeadWeight:     0.45,
	TailWeight:     0.35,
}

// Result is aligned with the original candidate slice. Scores contains blended
// scores by original index; Order contains original indices in final order.
type Result struct {
	Scores      []float64
	Order       []int
	Weights     []float64
	ChangedTop1 bool
}

// Blend combines normalized retrieval scores with reranker ordinal scores. It
// rejects malformed/non-finite inputs so every caller can fail open to its
// existing retrieval order.
func Blend(retrievalScores, rerankerScores []float64, cfg Config) (Result, bool) {
	if len(retrievalScores) < 2 || len(retrievalScores) != len(rerankerScores) {
		return Result{}, false
	}
	if !validConfig(cfg) {
		cfg = DefaultConfig
	}
	for i := range retrievalScores {
		if !finite(retrievalScores[i]) || !finite(rerankerScores[i]) {
			return Result{}, false
		}
	}

	rerankOrder := make([]int, len(rerankerScores))
	for i := range rerankOrder {
		rerankOrder[i] = i
	}
	sort.SliceStable(rerankOrder, func(i, j int) bool {
		return rerankerScores[rerankOrder[i]] > rerankerScores[rerankOrder[j]]
	})
	rerankRank := make([]int, len(rerankOrder))
	for rank, index := range rerankOrder {
		rerankRank[index] = rank
	}

	retrieval := normalizedRetrievalScores(retrievalScores)
	blended := make([]float64, len(retrievalScores))
	weights := make([]float64, len(retrievalScores))
	for i := range blended {
		weight := cfg.TailWeight
		if i < cfg.HeadCandidates {
			weight = cfg.HeadWeight
		}
		weights[i] = weight
		rerankOrdinal := ordinalScore(rerankRank[i], len(rerankRank))
		blended[i] = weight*retrieval[i] + (1-weight)*rerankOrdinal
	}

	order := make([]int, len(blended))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return blended[order[i]] > blended[order[j]]
	})
	return Result{Scores: blended, Order: order, Weights: weights, ChangedTop1: order[0] != 0}, true
}

// OrdinalScores projects an already-sorted retrieval list onto [1,0]. It is
// useful for callers that retain order but not the backend's raw scores.
func OrdinalScores(count int) []float64 {
	if count <= 0 {
		return nil
	}
	out := make([]float64, count)
	for i := range out {
		out[i] = ordinalScore(i, count)
	}
	return out
}

func normalizedRetrievalScores(scores []float64) []float64 {
	maxScore := 0.0
	for _, score := range scores {
		if score > maxScore {
			maxScore = score
		}
	}
	if maxScore <= 0 {
		return OrdinalScores(len(scores))
	}
	out := make([]float64, len(scores))
	for i, score := range scores {
		out[i] = math.Max(0, score/maxScore)
	}
	return out
}

func ordinalScore(rank, count int) float64 {
	if count <= 1 {
		return 1
	}
	return 1 - float64(rank)/float64(count-1)
}

func validConfig(cfg Config) bool {
	return cfg.HeadCandidates >= 0 && finite(cfg.HeadWeight) && finite(cfg.TailWeight) &&
		cfg.HeadWeight >= 0 && cfg.HeadWeight <= 1 && cfg.TailWeight >= 0 && cfg.TailWeight <= 1
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
