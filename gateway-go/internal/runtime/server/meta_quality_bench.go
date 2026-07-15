package server

// metaQualityBenchEvidence — the injected closure for MetaEvolutionTask's
// advisory codebase-health block (RSI P5-5). Reads the accepted health-v2
// baseline (the checked-in score snapshot that `codebase-health-v2 --check`
// ratchets against) and renders the overall score + weakest pillars as
// standing context for the producer's prose.
//
// ADVISORY ONLY: grounds the producer on structural quality. No gate reads
// it. The baseline is the contract surface — it reflects the last accepted
// quality state. A live delta-vs-baseline (current run − baseline) would need
// a Python bench run in-process, which the leaf-package boundary forbids;
// that trend slice is a follow-up. For now the producer sees the accepted
// health shape: "codebase at 82.7, weakest: change-locality 55.0".

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// healthV2Baseline is the JSON shape of scripts/audit/health-v2-baseline.json
// (only the fields this advisory reader needs).
type healthV2Baseline struct {
	Overall      float64            `json:"overall"`
	Pillars      map[string]float64 `json:"pillars"`
	HighFindings map[string]string  `json:"high_findings"`
}

// prodSourceDir resolves the production source checkout (DENEB_PROD_DIR,
// default $HOME/deneb) — where scripts/audit/ lives on the gateway host.
func prodSourceDir() string {
	if v := strings.TrimSpace(os.Getenv("DENEB_PROD_DIR")); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "deneb")
	}
	return ""
}

// metaQualityBenchEvidence formats the codebase-health baseline as a compact
// advisory one-liner. Empty when the baseline is absent (no source tree / dev).
func (s *Server) metaQualityBenchEvidence(_ context.Context) string {
	srcDir := prodSourceDir()
	if srcDir == "" {
		return ""
	}
	path := filepath.Join(srcDir, "scripts", "audit", "health-v2-baseline.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "" // no source tree or baseline — quiet, not broken
	}
	var b healthV2Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return ""
	}
	if b.Overall <= 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "- 코드베이스 종합 %.1f/100, high/critical 구조 결함 %d건 (수용된 베이스라인)",
		b.Overall, len(b.HighFindings))
	// Weakest 3 pillars: lowest scores are where structural debt concentrates.
	weakest := weakestPillars(b.Pillars, 3)
	if len(weakest) > 0 {
		parts := make([]string, 0, len(weakest))
		for _, p := range weakest {
			parts = append(parts, fmt.Sprintf("%s %.0f", p.name, p.score))
		}
		fmt.Fprintf(&sb, ", 약한 기둥: %s", strings.Join(parts, " · "))
	}
	sb.WriteString("\n- 이는 수용된 코드베이스 품질 상태 자문 — 약한 기둥이 진화 우선순위 참고가 되나, 게이트 통과 여부와 무관.")
	return sb.String()
}

type pillarScore struct {
	name  string
	score float64
}

// weakestPillars returns the n lowest-scoring pillars, sorted ascending.
func weakestPillars(pillars map[string]float64, n int) []pillarScore {
	out := make([]pillarScore, 0, len(pillars))
	for name, score := range pillars {
		out = append(out, pillarScore{name, score})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].score < out[j].score })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
