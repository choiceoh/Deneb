// rsi-backtest replays the archived skill-usage history through BOTH
// post-evolve rollback deciders — the legacy absolute threshold and the
// baseline-aware e-process (PACE) — and reports their agreement, so the
// P3 precondition-#1 cutover decision runs on backtested evidence instead of
// waiting for live labels (가속 노화: real inputs, synthetic re-execution).
//
// Read-only over ~/.deneb/data JSONL; safe to run against production.
//
//	go run ./cmd/rsi-backtest [-data ~/.deneb/data] [-threshold 3] [-alpha 0.05]
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/eprocess"
)

type usageRec struct {
	SkillName string `json:"skillName"`
	Success   bool   `json:"success"`
	UsedAt    int64  `json:"usedAt"`
	Source    string `json:"source"`
}

type lifecycleRec struct {
	Type      string `json:"type"`
	SkillName string `json:"skillName"`
	CreatedAt int64  `json:"createdAt"`
}

type verdict struct {
	Skill        string
	EvolvedAt    int64
	BaselineUses int
	BaselineFail int
	PostUses     int
	PostFails    int
	LegacyFired  bool
	Resolved     bool
	EPReject     bool
	EValue       float64
}

func loadJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path) //nolint:gosec // operator-supplied data dir
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for sc.Scan() {
		var v T
		if err := json.Unmarshal(sc.Bytes(), &v); err == nil {
			out = append(out, v)
		}
	}
	return out, sc.Err()
}

func isReal(u usageRec) bool { return u.Source == "" || u.Source == "real" }

func backtest(usage []usageRec, evolves []lifecycleRec, threshold int, alpha float64) []verdict {
	window := threshold * 2
	sort.Slice(usage, func(i, j int) bool { return usage[i].UsedAt < usage[j].UsedAt })
	sort.Slice(evolves, func(i, j int) bool { return evolves[i].CreatedAt < evolves[j].CreatedAt })

	nextEvolve := func(idx int) int64 {
		for _, e := range evolves[idx+1:] {
			if e.SkillName == evolves[idx].SkillName {
				return e.CreatedAt
			}
		}
		return 0
	}

	var out []verdict
	for i, ev := range evolves {
		horizon := nextEvolve(i)
		v := verdict{Skill: ev.SkillName, EvolvedAt: ev.CreatedAt}
		for _, u := range usage {
			if u.SkillName != ev.SkillName || !isReal(u) || u.UsedAt >= ev.CreatedAt {
				continue
			}
			v.BaselineUses++
			if !u.Success {
				v.BaselineFail++
			}
		}
		baselineRate := 0.0
		if v.BaselineUses > 0 {
			baselineRate = float64(v.BaselineFail) / float64(v.BaselineUses)
		}
		ep := eprocess.NewEProcess(alpha, baselineRate)
		for _, u := range usage {
			if u.SkillName != ev.SkillName || !isReal(u) || u.UsedAt <= ev.CreatedAt {
				continue
			}
			if horizon > 0 && u.UsedAt >= horizon {
				break
			}
			v.PostUses++
			if !u.Success {
				v.PostFails++
			}
			ep.Observe(!u.Success)
			if v.PostFails >= threshold {
				v.LegacyFired, v.Resolved = true, true
				break
			}
			if v.PostUses >= window {
				v.Resolved = true
				break
			}
		}
		v.EPReject, v.EValue = ep.Reject(), ep.E
		if v.PostUses > 0 {
			out = append(out, v)
		}
	}
	return out
}

func main() {
	home, _ := os.UserHomeDir()
	dataDir := flag.String("data", filepath.Join(home, ".deneb", "data"), "genesis data dir")
	threshold := flag.Int("threshold", 3, "legacy rollback failure threshold")
	alpha := flag.Float64("alpha", eprocess.DefaultEProcessAlpha, "e-process alpha")
	flag.Parse()

	usage, err := loadJSONL[usageRec](filepath.Join(*dataDir, "skill_usage.jsonl"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "usage log:", err)
		os.Exit(1)
	}
	all, err := loadJSONL[lifecycleRec](filepath.Join(*dataDir, "skill_genesis_log.jsonl"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "lifecycle log:", err)
		os.Exit(1)
	}
	var evolves []lifecycleRec
	for _, e := range all {
		if e.Type == "evolved" {
			evolves = append(evolves, e)
		}
	}

	verdicts := backtest(usage, evolves, *threshold, *alpha)
	var fired, resolved, overfire, missed int
	for _, v := range verdicts {
		if v.LegacyFired {
			fired++
			if !v.EPReject {
				overfire++
			}
		}
		if v.Resolved && !v.LegacyFired && v.EPReject {
			missed++
		}
		if v.Resolved {
			resolved++
		}
	}
	fmt.Printf("evolve events with post-usage: %d (of %d total)\n", len(verdicts), len(evolves))
	fmt.Printf("resolved: %d · legacy fired: %d · suspected overfires (fired, ep quiet): %d · suspected misses (confirmed, ep reject): %d\n",
		resolved, fired, overfire, missed)
	for _, v := range verdicts {
		state := "unresolved"
		if v.LegacyFired {
			state = "FIRED"
		} else if v.Resolved {
			state = "confirmed"
		}
		fmt.Printf("  %-30s base=%d/%d post=%d/%d legacy=%-10s epReject=%v E=%.1f\n",
			v.Skill, v.BaselineFail, v.BaselineUses, v.PostFails, v.PostUses, state, v.EPReject, v.EValue)
	}
}
