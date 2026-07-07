package observatory

import (
	"fmt"
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
	// memoryConsumedThrough is the MEMORY.md distillation stamp (months old on
	// purpose — it must NOT read as diary backlog); diary consumption comes
	// from the files ledger: 06-27 fully consumed, 06-28's 1-byte tail pending.
	write("wiki/.diary-process-state.json",
		`{"memoryConsumedThrough":"2026-04-07 10:43","files":{"diary-2026-06-27.md":{"offset":1},"diary-2026-06-28.md":{"offset":0}}}`, 30) // stale: >24h
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
	if r.Memory.DreamerConsumedThrough != "2026-06-27" {
		t.Errorf("dreamerConsumedThrough = %q, want 2026-06-27 (newest fully-consumed)", r.Memory.DreamerConsumedThrough)
	}
	if r.Memory.BacklogDays != 0 {
		t.Errorf("backlogDays = %d, want 0 — only the latest diary's tail is pending, and the April MEMORY.md stamp must not count as diary backlog", r.Memory.BacklogDays)
	}
	if r.Memory.PendingBytes != 1 {
		t.Errorf("pendingBytes = %d, want 1", r.Memory.PendingBytes)
	}
	if r.Memory.MemoryMDStamp != "2026-04-07 10:43" {
		t.Errorf("memoryMdStamp = %q, want the raw distillation stamp", r.Memory.MemoryMDStamp)
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
	for _, want := range []string{"Deneb self-status", "LIVENESS", "dreamer STALE", "skill-review ok", "no-op 3", "dreamer→2026-06-27", "(pending 1B)", "memoryMD→2026-04-07", "DOWN: vllm-nex"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
	if strings.Contains(md, "backlog") {
		t.Errorf("no backlog expected when only the live tail is pending:\n%s", md)
	}
}

// TestMemoryStatus_RealBacklogAndLegacySkips: unconsumed bytes from an OLDER
// diary date surface as backlog days; untracked files older than the newest
// tracked one are the dreamer's legacy-cutoff skips and count as neither
// consumed nor pending; untracked NEWER files are fully pending.
func TestMemoryStatus_RealBacklogAndLegacySkips(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("wiki/.diary-process-state.json",
		`{"files":{"diary-2026-06-25.md":{"offset":2},"diary-2026-06-26.md":{"offset":1}}}`)
	write("memory/diary/diary-2026-06-01.md", "legacy") // untracked, older than newest tracked → skipped
	write("memory/diary/diary-2026-06-25.md", "ab")     // consumed (offset==size)
	write("memory/diary/diary-2026-06-26.md", "abc")    // 2B pending since 06-26
	write("memory/diary/diary-2026-06-28.md", "abcd")   // untracked, newer → 4B fully pending

	m := memoryStatus(dir, time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	if m.DreamerConsumedThrough != "2026-06-25" {
		t.Errorf("dreamerConsumedThrough = %q, want 2026-06-25", m.DreamerConsumedThrough)
	}
	if m.LatestDiary != "2026-06-28" {
		t.Errorf("latestDiary = %q, want 2026-06-28", m.LatestDiary)
	}
	if m.PendingBytes != 6 {
		t.Errorf("pendingBytes = %d, want 6 (2 tail + 4 unscanned; legacy skip excluded)", m.PendingBytes)
	}
	if m.BacklogDays != 2 {
		t.Errorf("backlogDays = %d, want 2 (oldest pending 06-26 → latest 06-28)", m.BacklogDays)
	}
}

// TestRecentFailures_CountsOnlyRecentEntries: a long-lived session file keeps
// its June failures at the head while being appended today — only entries
// STAMPED within 24h may count (live 2026-07-06 false alarm: the watchdog
// reported a "recent" spike of a pattern last seen three weeks earlier).
func TestRecentFailures_CountsOnlyRecentEntries(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	oldTs := now.Add(-20 * 24 * time.Hour).UnixMilli()
	recentTs := now.Add(-2 * time.Hour).UnixMilli()
	line := func(ts int64) string {
		return fmt.Sprintf(`{"ts":%d,"type":"turn.tool","data":{"error":"json: cannot unmarshal string into Go struct field .max of type int"}}`, ts)
	}
	content := line(oldTs) + "\n" + line(oldTs) + "\n" + line(recentTs) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "client:main.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := recentFailures(dir, now)
	if len(got) != 1 || got[0].Pattern != "type-coercion drop" || got[0].Count != 1 {
		t.Fatalf("want exactly the one recent hit, got %+v", got)
	}

	// A file with ONLY stale entries (but fresh mtime) must report nothing.
	if err := os.WriteFile(filepath.Join(dir, "client:main.jsonl"), []byte(line(oldTs)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := recentFailures(dir, now); len(got) != 0 {
		t.Fatalf("stale-only file must not alarm, got %+v", got)
	}
}

// TestReadTailCapped: the cap keeps the END of the file (append-only logs put
// the newest entries there).
func TestReadTailCapped(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(p, []byte("OLDOLDOLD-NEWNEW"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := string(readTailCapped(p, 6)); got != "NEWNEW" {
		t.Fatalf("tail cap = %q, want NEWNEW", got)
	}
	if got := string(readTailCapped(p, 100)); got != "OLDOLDOLD-NEWNEW" {
		t.Fatalf("under-cap read = %q", got)
	}
}

// TestMemoryStatus_NoLedgerNoFakeBacklog: with no files ledger at all (fresh
// box or pre-ledger state), nothing counts as pending — the dreamer's absence
// is a liveness signal, not a byte backlog.
func TestMemoryStatus_NoLedgerNoFakeBacklog(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "memory", "diary")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "diary-2026-06-28.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := memoryStatus(dir, time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	if m.PendingBytes != 0 || m.BacklogDays != 0 || m.DreamerConsumedThrough != "" {
		t.Errorf("no-ledger must not fake a backlog: %+v", m)
	}
	if m.LatestDiary != "2026-06-28" {
		t.Errorf("latestDiary = %q, want 2026-06-28", m.LatestDiary)
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
