// search_params.go — Tunable search scoring parameters loaded from JSON config.
// Used by the benchmark test for autoresearch parameter optimization.
// Production code falls back to hardcoded constants when params is nil on Store.
package memory

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// Default scoring weights and thresholds.
// Autoresearch reads these constants directly from this file — keep the
// simple `name = value` format so the regex extractor can parse them.
const (
	weightHybrid       = 0.40
	weightImportance   = 0.25
	weightRecency      = 0.25
	weightVerification = 0.10

	defaultSteepnessDays  = 7.0
	ftsAndMinResults      = 3
	dedupJaccardThreshold = 0.60
)

// SearchParams holds all tunable search scoring parameters.
// When set on Store via SetSearchParams, these override the hardcoded constants.
type SearchParams struct {
	// Scoring weights (should sum to ~1.0).
	WeightHybrid       float64 `json:"weight_hybrid"`
	WeightImportance   float64 `json:"weight_importance"`
	WeightRecency      float64 `json:"weight_recency"`
	WeightVerification float64 `json:"weight_verification"`

	// Category importance multipliers.
	CategoryImportanceMultiplier map[string]float64 `json:"category_importance_multiplier"`

	// Category steepness days (recency decay half-life per category).
	CategorySteepnessDays map[string]float64 `json:"category_steepness_days"`
	DefaultSteepnessDays  float64            `json:"default_steepness_days"`

	// FTS thresholds.
	FTSAndMinResults int     `json:"fts_and_min_results"`
	ORPenalty        float64 `json:"or_penalty"`
	TrigramPenalty   float64 `json:"trigram_penalty"`

	// Vector search.
	VectorMinThreshold float64 `json:"vector_min_threshold"`
	HybridFTSWeight    float64 `json:"hybrid_fts_weight"`
	HybridVecWeight    float64 `json:"hybrid_vec_weight"`

	// Entity search.
	EntityMatchBaseline float64 `json:"entity_match_baseline"`

	// Reranker blend.
	RerankBlendReranker float64 `json:"rerank_blend_reranker"`
	RerankBlendHybrid   float64 `json:"rerank_blend_hybrid"`

	// Dedup.
	DedupJaccardThreshold float64 `json:"dedup_jaccard_threshold"`

	// Verification scoring.
	VerificationUnverified float64 `json:"verification_unverified"`
	VerificationVerified   float64 `json:"verification_verified"`
}

// DefaultSearchParams returns a SearchParams populated with the current
// hardcoded constant values. Used as fallback when no config file is loaded.
func DefaultSearchParams() SearchParams {
	return SearchParams{
		WeightHybrid:       weightHybrid,
		WeightImportance:   weightImportance,
		WeightRecency:      weightRecency,
		WeightVerification: weightVerification,

		CategoryImportanceMultiplier: map[string]float64{
			CategoryDecision:   1.20,
			CategoryPreference: 1.05,
			CategorySolution:   1.10,
			CategoryContext:    0.95,
			CategoryUserModel:  1.00,
			CategoryMutual:     0.85,
		},

		CategorySteepnessDays: map[string]float64{
			CategoryDecision:   14.0,
			CategoryPreference: 10.0,
			CategorySolution:   10.0,
			CategoryContext:    5.0,
			CategoryUserModel:  14.0,
			CategoryMutual:     7.0,
		},
		DefaultSteepnessDays: defaultSteepnessDays,

		FTSAndMinResults: ftsAndMinResults,
		ORPenalty:        0.85,
		TrigramPenalty:   0.80,

		VectorMinThreshold: 0.35,
		HybridFTSWeight:    0.40,
		HybridVecWeight:    0.60,

		EntityMatchBaseline: 0.60,

		RerankBlendReranker: 0.70,
		RerankBlendHybrid:   0.30,

		DedupJaccardThreshold: dedupJaccardThreshold,

		VerificationUnverified: 0.30,
		VerificationVerified:   1.00,
	}
}

// LoadSearchParams reads search parameters from a JSON file.
func LoadSearchParams(path string) (*SearchParams, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read search params: %w", err)
	}
	// Start from defaults so missing fields keep their default values.
	p := DefaultSearchParams()
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse search params: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid search params: %w", err)
	}
	return &p, nil
}

// Validate checks parameter constraints.
func (p *SearchParams) Validate() error {
	// Scoring weights must sum to ~1.0.
	weightSum := p.WeightHybrid + p.WeightImportance + p.WeightRecency + p.WeightVerification
	if math.Abs(weightSum-1.0) > 0.02 {
		return fmt.Errorf("scoring weights must sum to ~1.0, got %.4f", weightSum)
	}

	// All steepness values must be positive.
	if p.DefaultSteepnessDays <= 0 {
		return fmt.Errorf("default_steepness_days must be > 0, got %.2f", p.DefaultSteepnessDays)
	}
	for cat, days := range p.CategorySteepnessDays {
		if days <= 0 {
			return fmt.Errorf("category_steepness_days[%s] must be > 0, got %.2f", cat, days)
		}
	}

	// Importance multipliers must be positive.
	for cat, mult := range p.CategoryImportanceMultiplier {
		if mult <= 0 {
			return fmt.Errorf("category_importance_multiplier[%s] must be > 0, got %.2f", cat, mult)
		}
	}

	// Penalties must be in (0, 1].
	if p.ORPenalty <= 0 || p.ORPenalty > 1 {
		return fmt.Errorf("or_penalty must be in (0, 1], got %.2f", p.ORPenalty)
	}
	if p.TrigramPenalty <= 0 || p.TrigramPenalty > 1 {
		return fmt.Errorf("trigram_penalty must be in (0, 1], got %.2f", p.TrigramPenalty)
	}

	// Thresholds must be in [0, 1].
	if p.VectorMinThreshold < 0 || p.VectorMinThreshold > 1 {
		return fmt.Errorf("vector_min_threshold must be in [0, 1], got %.2f", p.VectorMinThreshold)
	}
	if p.DedupJaccardThreshold < 0 || p.DedupJaccardThreshold > 1 {
		return fmt.Errorf("dedup_jaccard_threshold must be in [0, 1], got %.2f", p.DedupJaccardThreshold)
	}

	return nil
}
