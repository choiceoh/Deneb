package observatory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshot(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 28, 15, 0, 0, 0, time.UTC)

	write := func(rel, content string, ageHours float64) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(-time.Duration(ageHours * float64(time.Hour)))
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}

	write("data/skill_genesis_log.jsonl",
		`{"route":"no-op"}`+"\n"+`{"route":"no-op"}`+"\n"+`{"route":"evolve"}`+"\n"+`{"route":"no-op"}`+"\n", 1)
	write("regression-baseline.json", `{}`, 1)
	write("model-stats.json", `{"windowHours":24,"models":{"glm-5.2":{},"dsv4":{}}}`, 1)
	write("wiki/.diary-process-state.json", `{"memoryConsumedThrough":"2026-06-20 10:43"}`, 30) // stale: >24h
	write("memory/diary/diary-2026-06-27.md", "x", 26)
	write("memory/diary/diary-2026-06-28.md", "y", 2) // fresh + lexically latest
	write("logs/sparkfleet.log", "time=t level=WARN msg=\"backends down\" down=vllm-nex url=http://x\n", 1)
	write("spillover/a.txt", "x", 1)
	write("spillover/b.txt", "x", 2)

	r := Snapshot(dir, now)

	if r.Skill.NoOp != 3 || r.Skill.Evolve != 1 || r.Skill.Genesis != 0 || r.Skill.Total != 4 {
		t.Errorf("skill = %+v, want noop3/evolve1/genesis0/total4", r.Skill)
	}
	if r.Memory.LatestDiary != "2026-06-28" {
		t.Errorf("latestDiary = %q, want 2026-06-28", r.Memory.LatestDiary)
	}
	if r.Memory.BacklogDays != 8 {
		t.Errorf("backlogDays = %d, want 8", r.Memory.BacklogDays)
	}
	if r.Memory.SpilloverToday != 2 {
		t.Errorf("spilloverToday = %d, want 2", r.Memory.SpilloverToday)
	}
	if len(r.Models.Models) != 2 {
		t.Errorf("models = %v, want 2", r.Models.Models)
	}
	if len(r.Models.Down) != 1 || r.Models.Down[0] != "vllm-nex" {
		t.Errorf("down = %v, want [vllm-nex]", r.Models.Down)
	}

	live := map[string]LoopStatus{}
	for _, l := range r.Liveness {
		live[l.Name] = l
	}
	if live["dreamer"].Fresh || live["dreamer"].Missing {
		t.Errorf("dreamer should be present-but-STALE (30h > 24h): %+v", live["dreamer"])
	}
	if !live["skill-review"].Fresh {
		t.Errorf("skill-review should be fresh: %+v", live["skill-review"])
	}
	if !live["diary"].Fresh {
		t.Errorf("diary should be fresh: %+v", live["diary"])
	}

	md := r.Markdown()
	for _, want := range []string{"Deneb self-status", "LIVENESS", "dreamer STALE", "skill-review ok", "no-op 3", "backlog 8d", "DOWN: vllm-nex"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
}

// A missing state dir must yield a degraded-but-valid report (every loop MISSING),
// never a panic — the digest's job is to make absence visible.
func TestSnapshot_Empty(t *testing.T) {
	r := Snapshot(filepath.Join(t.TempDir(), "nope"), time.Now())
	if len(r.Liveness) == 0 {
		t.Fatal("expected liveness rows even when empty")
	}
	for _, l := range r.Liveness {
		if !l.Missing {
			t.Errorf("%s should be MISSING in an empty state dir", l.Name)
		}
	}
	if md := r.Markdown(); !strings.Contains(md, "MISSING") {
		t.Errorf("markdown should surface MISSING:\n%s", md)
	}
}
