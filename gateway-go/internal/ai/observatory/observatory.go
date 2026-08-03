// Package observatory aggregates Deneb's scattered self-improvement telemetry
// into one compact, machine-readable self-status digest.
//
// The point is AI consumption, not a human dashboard: an agent (or an external
// puppeteer) reads the digest in a single call instead of spelunking a dozen
// files under ~/.deneb. It also closes a blind spot this telemetry itself
// revealed — several improvement loops (dreamer, skill-curator, config-audit)
// went silent for weeks with no signal, because nothing watched their liveness.
// The digest makes "is my own improvement machinery alive?" a one-glance answer.
//
// Everything is read on demand from state files; the package holds no state and
// never fails the whole digest on a missing/unreadable source — a dead loop is
// the signal, not an error.
package observatory

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Report is the aggregated self-status snapshot.
type Report struct {
	GeneratedAt time.Time      `json:"generatedAt"`
	Liveness    []LoopStatus   `json:"liveness"`
	Skill       SkillSummary   `json:"skill"`
	Frontier    []FrontierItem `json:"frontier,omitempty"`
	Memory      MemoryStatus   `json:"memory"`
	Models      ModelSummary   `json:"models"`
	Failures    []FailureCount `json:"failures,omitempty"`
}

// FrontierItem is a recurring no-op skill — a workflow the agent keeps seeing
// and confirming an existing skill covers. The no-op stream is otherwise a
// one-bit decision; aggregated, the top entries are a free map of "what the
// user actually does on repeat" and where the next-skill frontier sits.
type FrontierItem struct {
	Skill string `json:"skill"`
	NoOps int    `json:"noOps"`
}

// FailureCount is how many times a silent-failure pattern appeared in the recent
// agent logs. These fail by evaporating (a dropped tool call, an unrecorded
// decision) rather than erroring loudly, so surfacing the count is the point.
type FailureCount struct {
	Pattern string `json:"pattern"`
	Count   int    `json:"count"`
}

// LoopStatus is the freshness of one improvement loop, derived from when its
// state file last changed versus the cadence we expect of it.
type LoopStatus struct {
	Name     string  `json:"name"`
	AgeHours float64 `json:"ageHours"`
	Fresh    bool    `json:"fresh"`
	Missing  bool    `json:"missing,omitempty"`
}

// SkillSummary is the Propus self-improvement decision mix from the genesis log.
type SkillSummary struct {
	NoOp    int `json:"noOp"`
	Evolve  int `json:"evolve"`
	Genesis int `json:"genesis"`
	Total   int `json:"total"`
}

// MemoryStatus reports how far the dreamer trails the diary and recent spill
// pressure. Consumption is measured from the dreamer's own per-file offsets
// (.diary-process-state.json "files") — NOT from memoryConsumedThrough, which
// is the workspace-MEMORY.md distillation stamp, a different source whose
// April date once masqueraded here as a 91-day diary "backlog" while the
// dreamer was in fact current (live false alarm, 2026-07-07).
type MemoryStatus struct {
	// DreamerConsumedThrough is the newest diary date the dreamer has fully
	// consumed (offset == size). The live diary's growing tail keeps today
	// pending most of the day, so this trailing by one date is normal.
	DreamerConsumedThrough string `json:"dreamerConsumedThrough,omitempty"`
	LatestDiary            string `json:"latestDiary,omitempty"`
	// BacklogDays is latest diary date minus the OLDEST date with unconsumed
	// bytes — 0 while only today's tail is pending, growing only when the
	// dreamer actually stops draining.
	BacklogDays int `json:"backlogDays,omitempty"`
	// PendingBytes is the total diary bytes not yet dreamed.
	PendingBytes int64 `json:"pendingBytes,omitempty"`
	// MemoryMDStamp is the workspace MEMORY.md distilled-through stamp
	// (memoryConsumedThrough), surfaced under its real meaning.
	MemoryMDStamp  string `json:"memoryMdStamp,omitempty"`
	SpilloverToday int    `json:"spilloverToday"`
}

// ModelSummary lists the models seen in the stats window and any backends the
// fleet currently reports down.
type ModelSummary struct {
	WindowHours int      `json:"windowHours,omitempty"`
	Models      []string `json:"models,omitempty"`
	Down        []string `json:"down,omitempty"`
}

type loopSpec struct {
	name   string
	rel    string  // path relative to stateDir; the "diary" name globs diary-*.md
	thresh float64 // hours before the loop is considered stale
}

// Snapshot reads the telemetry under stateDir and returns the aggregated report.
// now is injected so callers (and tests) control "fresh vs stale".
func Snapshot(stateDir string, now time.Time) Report {
	r := Report{GeneratedAt: now}
	for _, sp := range []loopSpec{
		{"dreamer", "wiki/.diary-process-state.json", 24},
		{"skill-review", "data/skill_genesis_log.jsonl", 48},
		{"regression-baseline", "regression-baseline.json", 72},
		{"model-stats", "model-stats.json", 24},
		{"diary", "memory/diary", 48},
	} {
		r.Liveness = append(r.Liveness, loopStatus(stateDir, sp, now))
	}
	r.Skill, r.Frontier = skillAndFrontier(filepath.Join(stateDir, "data", "skill_genesis_log.jsonl"))
	r.Memory = memoryStatus(stateDir, now)
	r.Models = modelSummary(stateDir)
	r.Failures = recentFailures(filepath.Join(stateDir, "agent-logs"), now)
	return r
}

func loopStatus(stateDir string, sp loopSpec, now time.Time) LoopStatus {
	var mt time.Time
	if sp.name == "diary" {
		_, mt = newestDiary(filepath.Join(stateDir, sp.rel))
	} else if fi, err := os.Stat(filepath.Join(stateDir, sp.rel)); err == nil {
		mt = fi.ModTime()
	}
	if mt.IsZero() {
		return LoopStatus{Name: sp.name, Missing: true}
	}
	age := now.Sub(mt).Hours()
	return LoopStatus{Name: sp.name, AgeHours: age, Fresh: age <= sp.thresh}
}

// skillAndFrontier reads the genesis log once: the decision mix plus the
// no-op-by-skill tally that surfaces the recurring-workflow frontier.
func skillAndFrontier(path string) (SkillSummary, []FrontierItem) {
	var s SkillSummary
	noopBySkill := map[string]int{}
	f, err := os.Open(path)
	if err != nil {
		return s, nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var rec struct {
			Route     string `json:"route"`
			SkillName string `json:"skillName"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(rec.Route)) {
		case "no-op", "noop":
			s.NoOp++
			name := strings.TrimSpace(rec.SkillName)
			if name == "" {
				name = "(unmatched)"
			}
			noopBySkill[name]++
		case "evolve":
			s.Evolve++
		case "genesis":
			s.Genesis++
		}
	}
	s.Total = s.NoOp + s.Evolve + s.Genesis
	return s, topFrontier(noopBySkill, 5)
}

// topFrontier returns the n most-confirmed skills by no-op count, descending.
func topFrontier(m map[string]int, n int) []FrontierItem {
	items := make([]FrontierItem, 0, len(m))
	for k, v := range m {
		items = append(items, FrontierItem{Skill: k, NoOps: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].NoOps != items[j].NoOps {
			return items[i].NoOps > items[j].NoOps
		}
		return items[i].Skill < items[j].Skill
	})
	if len(items) > n {
		items = items[:n]
	}
	return items
}

// recentFailures scans agent logs for silent-failure signatures — failures
// that evaporate rather than erroring loudly. Bounded (file count + bytes per
// file) so the on-demand digest stays cheap against thousands of log files.
//
// Counts only entries whose OWN timestamp falls in the last 24h. The previous
// implementation filtered files by mtime but counted the whole head-capped
// content — a long-lived session file (appended daily, June history intact at
// its head) kept re-reporting three-week-old failures as "recent", and the
// watchdog cried wolf about an extinct pattern (live 2026-07-06/07,
// "type-coercion drop 급증" on events last seen 06-17). Files are read
// TAIL-capped for the same reason: a growing file's newest entries live at
// the end.
func recentFailures(dir string, now time.Time) []FailureCount {
	patterns := []struct{ label, needle string }{
		{"type-coercion drop", "cannot unmarshal string into"},
		{"empty-args parse fail", "unexpected end of JSON input"},
		{"unknown tool", "unknown tool"},
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	cutoff := now.Add(-24 * time.Hour)
	cutoffMs := cutoff.UnixMilli()
	counts := make([]int, len(patterns))
	const maxFiles, maxBytes = 500, 256 * 1024
	scanned := 0
	for _, e := range entries {
		if scanned >= maxFiles {
			break
		}
		info, err := e.Info()
		if err != nil || e.IsDir() || info.ModTime().Before(cutoff) {
			continue
		}
		scanned++
		for _, line := range strings.Split(string(readTailCapped(filepath.Join(dir, e.Name()), maxBytes)), "\n") {
			hits := hitPatterns(line, patterns)
			if len(hits) == 0 {
				continue
			}
			// Parse just the stamp; a tail-cut partial first line fails to
			// parse and is skipped.
			var meta struct {
				Ts int64 `json:"ts"`
			}
			if json.Unmarshal([]byte(line), &meta) != nil || meta.Ts < cutoffMs {
				continue
			}
			for _, i := range hits {
				counts[i]++
			}
		}
	}
	var out []FailureCount
	for i, p := range patterns {
		if counts[i] > 0 {
			out = append(out, FailureCount{Pattern: p.label, Count: counts[i]})
		}
	}
	return out
}

// hitPatterns returns the indexes of the patterns whose needle appears in line.
func hitPatterns(line string, patterns []struct{ label, needle string }) []int {
	var hits []int
	for i, p := range patterns {
		if strings.Contains(line, p.needle) {
			hits = append(hits, i)
		}
	}
	return hits
}

// readTailCapped reads at most max bytes from the END of path (or nil on
// error) — the right cap direction for append-only logs, where the newest
// entries are the ones a "recent" scan wants.
func readTailCapped(path string, max int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	if size := info.Size(); size > max {
		if _, err := f.Seek(size-max, io.SeekStart); err != nil {
			return nil
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, max))
	if err != nil {
		return nil
	}
	return data
}

type diaryProcessState struct {
	MemoryConsumedThrough string `json:"memoryConsumedThrough"`
	Files                 map[string]struct {
		Offset int64 `json:"offset"`
	} `json:"files"`
}

func memoryStatus(stateDir string, now time.Time) MemoryStatus {
	memoryStamp, offsets := loadDiaryProcessState(stateDir)
	m := scanDiaryStatus(stateDir, offsets)
	m.MemoryMDStamp = memoryStamp
	m.SpilloverToday = countSpilloverToday(stateDir, now)
	return m
}

// loadDiaryProcessState reads the dreamer's two independent checkpoints: the
// MEMORY.md distillation stamp and the consumed byte offset for each diary.
// A missing or malformed ledger is equivalent to no checkpoint.
func loadDiaryProcessState(stateDir string) (string, map[string]int64) {
	data, err := os.ReadFile(filepath.Join(stateDir, "wiki", ".diary-process-state.json"))
	if err != nil {
		return "", nil
	}
	var state diaryProcessState
	if json.Unmarshal(data, &state) != nil {
		return "", nil
	}
	offsets := make(map[string]int64, len(state.Files))
	for name, file := range state.Files {
		offsets[name] = file.Offset
	}
	return strings.TrimSpace(state.MemoryConsumedThrough), offsets
}

// scanDiaryStatus joins the durable offsets with diary files. It deliberately
// distinguishes legacy files (older than the newest tracked file) from diaries
// that arrived after the ledger was last updated.
func scanDiaryStatus(stateDir string, offsets map[string]int64) MemoryStatus {
	var status MemoryStatus
	// Join the ledger against the actual diary files. Untracked files OLDER
	// than the newest tracked one are the dreamer's legacy-cutoff skips —
	// neither consumed nor pending; untracked files NEWER than it simply
	// haven't been scanned yet and count as fully pending. With no ledger at
	// all we report only LatestDiary — the dreamer's absence already shows up
	// as a liveness gap, not a fake byte backlog.
	entries, err := os.ReadDir(filepath.Join(stateDir, "memory", "diary"))
	if err != nil {
		return status
	}
	newestTracked := ""
	for name := range offsets {
		if name > newestTracked {
			newestTracked = name
		}
	}
	oldestPending := ""
	for _, entry := range entries {
		date, pending, consumed := diaryEntryStatus(entry, offsets, newestTracked)
		if date == "" {
			continue
		}
		if date > status.LatestDiary {
			status.LatestDiary = date
		}
		if consumed {
			if date > status.DreamerConsumedThrough {
				status.DreamerConsumedThrough = date
			}
			continue
		}
		if pending <= 0 {
			continue
		}
		status.PendingBytes += pending
		if oldestPending == "" || date < oldestPending {
			oldestPending = date
		}
	}
	if oldestPending != "" {
		status.BacklogDays = backlogDays(oldestPending, status.LatestDiary)
	}
	return status
}

func diaryEntryStatus(
	entry os.DirEntry,
	offsets map[string]int64,
	newestTracked string,
) (date string, pending int64, consumed bool) {
	name := entry.Name()
	if entry.IsDir() || !strings.HasPrefix(name, "diary-") || !strings.HasSuffix(name, ".md") {
		return "", 0, false
	}
	date = diaryDate(name)
	info, err := entry.Info()
	if err != nil {
		return date, 0, false
	}
	if offset, tracked := offsets[name]; tracked {
		pending = info.Size() - offset
		return date, pending, pending <= 0
	}
	if newestTracked != "" && name > newestTracked {
		return date, info.Size(), false
	}
	return date, 0, false // legacy skip (or no ledger at all)
}

func countSpilloverToday(stateDir string, now time.Time) int {
	entries, err := os.ReadDir(filepath.Join(stateDir, "spillover"))
	if err != nil {
		return 0
	}
	year, month, day := now.Date()
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		entryYear, entryMonth, entryDay := info.ModTime().Date()
		if entryYear == year && entryMonth == month && entryDay == day {
			count++
		}
	}
	return count
}

func modelSummary(stateDir string) ModelSummary {
	var ms ModelSummary
	if data, err := os.ReadFile(filepath.Join(stateDir, "model-stats.json")); err == nil {
		var st struct {
			WindowHours int                        `json:"windowHours"`
			Models      map[string]json.RawMessage `json:"models"`
		}
		if json.Unmarshal(data, &st) == nil {
			ms.WindowHours = st.WindowHours
			for name := range st.Models {
				ms.Models = append(ms.Models, name)
			}
			sort.Strings(ms.Models)
		}
	}
	ms.Down = fleetDownBackends(filepath.Join(stateDir, "logs", "sparkfleet.log"))
	return ms
}

// fleetDownBackends parses the most recent "down=<name>" the fleet logged. The
// log re-emits the standing condition every check, so the last line is current.
func fleetDownBackends(path string) []string {
	line := lastLineContaining(path, "down=")
	if line == "" {
		return nil
	}
	var down []string
	for _, tok := range strings.Fields(line) {
		if v, ok := strings.CutPrefix(tok, "down="); ok && v != "" {
			for _, name := range strings.Split(v, ",") {
				if name = strings.TrimSpace(name); name != "" {
					down = append(down, name)
				}
			}
		}
	}
	return down
}

// --- small file helpers ---

// newestDiary returns the lexically-greatest diary-*.md (dates sort as names)
// and its mtime.
func newestDiary(dir string) (string, time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}
	}
	best := ""
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, "diary-") && strings.HasSuffix(n, ".md") && n > best {
			best = n
		}
	}
	if best == "" {
		return "", time.Time{}
	}
	var mt time.Time
	if fi, err := os.Stat(filepath.Join(dir, best)); err == nil {
		mt = fi.ModTime()
	}
	return best, mt
}

func diaryDate(name string) string {
	s := strings.TrimSuffix(strings.TrimPrefix(name, "diary-"), ".md")
	if len(s) == 10 { // YYYY-MM-DD
		return s
	}
	return ""
}

// backlogDays is the whole-day gap between two YYYY-MM-DD strings (consumed → latest).
func backlogDays(consumed, latest string) int {
	const layout = "2006-01-02"
	c, err1 := time.Parse(layout, firstTenDate(consumed))
	l, err2 := time.Parse(layout, latest)
	if err1 != nil || err2 != nil {
		return 0
	}
	d := int(l.Sub(c).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

// firstTenDate pulls "YYYY-MM-DD" off the front of a possibly-longer timestamp.
func firstTenDate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// lastLineContaining returns the last line in the file's tail (last 8KB) that
// contains needle, or "" — bounded so the handler stays cheap on a large log.
func lastLineContaining(path, needle string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	const window = 8 * 1024
	start := int64(0)
	if fi.Size() > window {
		start = fi.Size() - window
	}
	if _, err := f.Seek(start, 0); err != nil {
		return ""
	}
	buf := make([]byte, fi.Size()-start)
	if _, err := f.Read(buf); err != nil {
		return ""
	}
	found := ""
	for _, ln := range strings.Split(string(buf), "\n") {
		if strings.Contains(ln, needle) {
			found = ln
		}
	}
	return found
}
