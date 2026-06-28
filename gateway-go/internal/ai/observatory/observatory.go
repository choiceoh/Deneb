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
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Report is the aggregated self-status snapshot.
type Report struct {
	GeneratedAt time.Time    `json:"generatedAt"`
	Liveness    []LoopStatus `json:"liveness"`
	Skill       SkillSummary `json:"skill"`
	Memory      MemoryStatus `json:"memory"`
	Models      ModelSummary `json:"models"`
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

// MemoryStatus reports how far behind the dreamer is and recent spill pressure.
type MemoryStatus struct {
	DreamerConsumedThrough string `json:"dreamerConsumedThrough,omitempty"`
	LatestDiary            string `json:"latestDiary,omitempty"`
	BacklogDays            int    `json:"backlogDays,omitempty"`
	SpilloverToday         int    `json:"spilloverToday"`
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
	r.Skill = skillSummary(filepath.Join(stateDir, "data", "skill_genesis_log.jsonl"))
	r.Memory = memoryStatus(stateDir, now)
	r.Models = modelSummary(stateDir)
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

func skillSummary(path string) SkillSummary {
	var s SkillSummary
	f, err := os.Open(path)
	if err != nil {
		return s
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var rec struct {
			Route string `json:"route"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(rec.Route)) {
		case "no-op", "noop":
			s.NoOp++
		case "evolve":
			s.Evolve++
		case "genesis":
			s.Genesis++
		}
	}
	s.Total = s.NoOp + s.Evolve + s.Genesis
	return s
}

func memoryStatus(stateDir string, now time.Time) MemoryStatus {
	var m MemoryStatus
	// Dreamer processing bookmark.
	if data, err := os.ReadFile(filepath.Join(stateDir, "wiki", ".diary-process-state.json")); err == nil {
		var st struct {
			MemoryConsumedThrough string `json:"memoryConsumedThrough"`
		}
		if json.Unmarshal(data, &st) == nil {
			m.DreamerConsumedThrough = strings.TrimSpace(st.MemoryConsumedThrough)
		}
	}
	// Latest diary date and how far the dreamer trails it.
	if name, _ := newestDiary(filepath.Join(stateDir, "memory", "diary")); name != "" {
		m.LatestDiary = diaryDate(name)
		m.BacklogDays = backlogDays(m.DreamerConsumedThrough, m.LatestDiary)
	}
	// Spill pressure today.
	if entries, err := os.ReadDir(filepath.Join(stateDir, "spillover")); err == nil {
		y, mo, d := now.Date()
		for _, e := range entries {
			if info, err := e.Info(); err == nil {
				ey, emo, ed := info.ModTime().Date()
				if ey == y && emo == mo && ed == d {
					m.SpilloverToday++
				}
			}
		}
	}
	return m
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
