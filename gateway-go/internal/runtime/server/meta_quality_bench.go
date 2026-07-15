package server

// metaQualityBenchEvidence — the injected closure for MetaEvolutionTask's
// advisory codebase-health block (RSI P5-5). Prefers Health Bench 3.0 baseline
// (+ optional live snapshot delta), then falls back to health-v2.
//
// ADVISORY ONLY: grounds the producer on structural/runtime/fitness quality.
// No gate reads it. Live delta comes from an externally written snapshot JSON
// (leaf-package boundary: no in-process Python bench).

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

// healthV3Baseline is scripts/audit/health-v3-baseline.json.
type healthV3Baseline struct {
	Overall      float64            `json:"overall"`
	Domains      map[string]float64 `json:"domains"`
	Pillars      map[string]float64 `json:"pillars"`
	HighFindings map[string]string  `json:"high_findings"`
}

// healthV3Snapshot is scripts/audit/health-v3-snapshot.json (external writer).
type healthV3Snapshot struct {
	Score struct {
		Overall float64            `json:"overall"`
		Domains map[string]float64 `json:"domains"`
	} `json:"score"`
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
	if text := metaQualityBenchV3(srcDir); text != "" {
		return text
	}
	return metaQualityBenchV2(srcDir)
}

func metaQualityBenchV3(srcDir string) string {
	path := filepath.Join(srcDir, "scripts", "audit", "health-v3-baseline.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var b healthV3Baseline
	if err := json.Unmarshal(raw, &b); err != nil || b.Overall <= 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "- Health Bench 3.0 종합 %.1f/100, high/critical 결함 %d건 (수용된 베이스라인)",
		b.Overall, len(b.HighFindings))
	if len(b.Domains) > 0 {
		parts := make([]string, 0, len(b.Domains))
		for _, name := range []string{"structure", "runtime", "fitness"} {
			if score, ok := b.Domains[name]; ok {
				parts = append(parts, fmt.Sprintf("%s %.0f", name, score))
			}
		}
		if len(parts) > 0 {
			fmt.Fprintf(&sb, ", 도메인: %s", strings.Join(parts, " · "))
		}
	}
	weakest := weakestPillars(b.Pillars, 3)
	if len(weakest) > 0 {
		parts := make([]string, 0, len(weakest))
		for _, p := range weakest {
			parts = append(parts, fmt.Sprintf("%s %.0f", p.name, p.score))
		}
		fmt.Fprintf(&sb, ", 약한 축: %s", strings.Join(parts, " · "))
	}
	if snapRaw, err := os.ReadFile(filepath.Join(srcDir, "scripts", "audit", "health-v3-snapshot.json")); err == nil {
		var snap healthV3Snapshot
		if json.Unmarshal(snapRaw, &snap) == nil && snap.Score.Overall > 0 {
			delta := snap.Score.Overall - b.Overall
			fmt.Fprintf(&sb, ", 라이브 델타 %+.1f (스냅샷 %.1f)", delta, snap.Score.Overall)
		}
	}
	sb.WriteString("\n- 이는 수용된 코드베이스·런타임·피트니스 자문 — 약한 축이 진화 우선순위 참고가 되나, 게이트 통과 여부와 무관.")
	return sb.String()
}

func metaQualityBenchV2(srcDir string) string {
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
