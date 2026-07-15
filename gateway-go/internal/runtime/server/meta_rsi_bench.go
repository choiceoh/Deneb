package server

// metaRSIBenchEvidence — advisory RSI Bench block for MetaEvolutionTask (P5-5).
// Reads checked-in rsi-bench-baseline.json (+ optional snapshot). Leaf boundary:
// no in-process Python. ADVISORY ONLY — no gate reads it.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type rsiBenchBaseline struct {
	Overall      float64            `json:"overall"`
	Domains      map[string]float64 `json:"domains"`
	Pillars      map[string]float64 `json:"pillars"`
	HighFindings map[string]string  `json:"high_findings"`
}

type rsiBenchSnapshot struct {
	Score struct {
		Overall float64            `json:"overall"`
		Domains map[string]float64 `json:"domains"`
	} `json:"score"`
}

// metaRSIBenchEvidence formats the RSI Bench baseline as compact advisory prose.
func (s *Server) metaRSIBenchEvidence(_ context.Context) string {
	srcDir := prodSourceDir()
	if srcDir == "" {
		return ""
	}
	path := filepath.Join(srcDir, "scripts", "audit", "rsi-bench-baseline.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var b rsiBenchBaseline
	if err := json.Unmarshal(raw, &b); err != nil || b.Overall <= 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "- RSI Bench 종합 %.1f/100 (과정+효용), high/critical %d건",
		b.Overall, len(b.HighFindings))
	if len(b.Domains) > 0 {
		parts := make([]string, 0, 2)
		for _, name := range []string{"process", "utility"} {
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
	if snapRaw, err := os.ReadFile(filepath.Join(srcDir, "scripts", "audit", "rsi-bench-snapshot.json")); err == nil {
		var snap rsiBenchSnapshot
		if json.Unmarshal(snapRaw, &snap) == nil && snap.Score.Overall > 0 {
			delta := snap.Score.Overall - b.Overall
			fmt.Fprintf(&sb, ", 라이브 델타 %+.1f (스냅샷 %.1f)", delta, snap.Score.Overall)
		}
	}
	sb.WriteString("\n- 이는 수용된 RSI 과정·효용 자문 — 약한 축이 메타 진화 우선순위 참고가 되나, 게이트 통과 여부와 무관.")
	return sb.String()
}
