package agentlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ReadOpts configures log reading.
type ReadOpts struct {
	SessionKey string
	RunID      string // optional: filter by run ID
	Type       string // optional: filter by entry type
	Limit      int    // max entries to return (default 50)
}

// ReadResult holds a page of log entries.
type ReadResult struct {
	Entries []LogEntry `json:"entries"`
	Total   int        `json:"total"`
}

// Read returns log entries for a session, filtered by opts.
// Entries are returned in reverse chronological order (newest first).
func (w *Writer) Read(opts ReadOpts) ReadResult {
	w.mu.Lock()
	defer w.mu.Unlock()

	path := w.logPath(opts.SessionKey)
	entries := readAllEntries(path)

	// Apply filters.
	if opts.RunID != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if e.RunID == opts.RunID {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	if opts.Type != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if e.Type == opts.Type {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	total := len(entries)

	// Reverse for newest-first.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	// Apply limit.
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > len(entries) {
		limit = len(entries)
	}

	return ReadResult{
		Entries: entries[:limit],
		Total:   total,
	}
}

// ReadRun finds every entry for runID across all session logs and returns them
// in chronological order along with the sessionKey they belong to. Unlike Read
// (which needs the sessionKey up front) this is the entry point for the observe
// plane, where a caller often knows only the runId. It glob-scans every *.jsonl
// like Aggregate — cheap on the single-user host where a run lands in exactly
// one session file. nil-safe: a nil Writer or empty runID yields (nil, "").
func (w *Writer) ReadRun(runID string) (entries []LogEntry, session string) {
	if w == nil || runID == "" {
		return nil, ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "*.jsonl"))
	for _, path := range paths {
		for _, e := range readAllEntries(path) {
			if e.RunID == runID {
				entries = append(entries, e)
				if session == "" {
					session = e.Session
				}
			}
		}
	}
	return entries, session
}

// ToolProvenanceQuery filters turn.tool provenance events. Empty fields are
// ignored. Target is a case-insensitive substring match against sanitized
// TurnToolData.Targets.
type ToolProvenanceQuery struct {
	Target  string
	Tool    string
	RunID   string
	SinceMs int64
	Limit   int
}

// ToolProvenanceEvent is the read-optimized shape for one turn.tool entry,
// joined with the enclosing log metadata. It backs the observe provenance view:
// "which recent agent/tool/run touched this path-like thing?"
type ToolProvenanceEvent struct {
	Ts          int64            `json:"ts"`
	RunID       string           `json:"runId,omitempty"`
	Session     string           `json:"session,omitempty"`
	Turn        int              `json:"turn,omitempty"`
	Name        string           `json:"name"`
	ToolUseID   string           `json:"toolUseId,omitempty"`
	DurationMs  int64            `json:"durationMs,omitempty"`
	InputBytes  int              `json:"inputBytes,omitempty"`
	InputHash   string           `json:"inputHash,omitempty"`
	OutputLen   int              `json:"outputLen,omitempty"`
	OutputHash  string           `json:"outputHash,omitempty"`
	Targets     []string         `json:"targets,omitempty"`
	FileEffects []ToolFileEffect `json:"fileEffects,omitempty"`
	IsError     bool             `json:"isError,omitempty"`
	Error       string           `json:"error,omitempty"`
}

// ToolProvenance scans retained turn.tool JSONL entries newest-first. It is a
// derived query over the existing agentlog store; if this becomes hot, the same
// shape can be backed by an index without changing callers.
func (w *Writer) ToolProvenance(q ToolProvenanceQuery) []ToolProvenanceEvent {
	if w == nil {
		return nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	var out []ToolProvenanceEvent
	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "*.jsonl"))
	for _, path := range paths {
		for _, e := range readAllEntries(path) {
			if e.Type != TypeTurnTool {
				continue
			}
			if q.SinceMs > 0 && e.Ts < q.SinceMs {
				continue
			}
			if q.RunID != "" && e.RunID != q.RunID {
				continue
			}
			var d TurnToolData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			if q.Tool != "" && d.Name != q.Tool {
				continue
			}
			if q.Target != "" && !targetMatches(d.Targets, d.FileEffects, q.Target) {
				continue
			}
			out = append(out, ToolProvenanceEvent{
				Ts:          e.Ts,
				RunID:       e.RunID,
				Session:     e.Session,
				Turn:        d.Turn,
				Name:        d.Name,
				ToolUseID:   d.ToolUseID,
				DurationMs:  d.DurationMs,
				InputBytes:  d.InputBytes,
				InputHash:   d.InputHash,
				OutputLen:   d.OutputLen,
				OutputHash:  d.OutputHash,
				Targets:     append([]string(nil), d.Targets...),
				FileEffects: append([]ToolFileEffect(nil), d.FileEffects...),
				IsError:     d.IsError,
				Error:       d.Error,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts > out[j].Ts })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func targetMatches(targets []string, effects []ToolFileEffect, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	for _, target := range targets {
		if strings.Contains(strings.ToLower(target), query) {
			return true
		}
	}
	for _, effect := range effects {
		if strings.Contains(strings.ToLower(effect.Path), query) {
			return true
		}
	}
	return false
}

// ToolStat aggregates one tool's usage across every recorded run.
type ToolStat struct {
	Name    string `json:"name"`
	Calls   int    `json:"calls"`
	Errors  int    `json:"errors"`
	TotalMs int64  `json:"totalMs"`
	AvgMs   int64  `json:"avgMs"`
}

// AggregateResult is a cross-session behavioral roll-up: what the agent and its
// autonomous layer actually did, summed over every session JSONL. It is the
// data backing the "what is this agent doing / what's being used / what's
// silently failing" questions that motivate the behavioral logging.
type AggregateResult struct {
	Runs              int   `json:"runs"`             // run.end count
	ProactiveRuns     int   `json:"proactiveRuns"`    // runs that were autonomous/auto-delivered
	CompactedRuns     int   `json:"compactedRuns"`    // runs where compaction fired
	TotalInputTokens  int64 `json:"totalInputTokens"` // summed run input tokens
	TotalOutputTokens int64 `json:"totalOutputTokens"`
	CacheReadTokens   int64 `json:"cacheReadTokens"` // prompt-cache reuse total

	// Tools is the per-tool usage histogram, sorted by call count descending —
	// the top of the list is what the agent leans on; a tool with high Errors
	// or absent entirely is a candidate for fixing or removal.
	Tools []ToolStat `json:"tools"`

	// ProactiveDecisions counts relay outcomes keyed by "decision[:reason]"
	// (e.g. "delivered", "suppressed:contentless") — the proactive funnel.
	ProactiveDecisions map[string]int `json:"proactiveDecisions"`

	// BackgroundJobs / BackgroundErrors count cycles per background worker name
	// (gmail poll, evolution, …). A name with 0 cycles over a window it should
	// have run in is the silent-death signal.
	BackgroundJobs   map[string]int `json:"backgroundJobs"`
	BackgroundErrors map[string]int `json:"backgroundErrors"`
}

// Aggregate scans every session JSONL under baseDir and rolls up behavioral
// stats from turn.tool (tool usage), run.end (run totals + cache + proactive +
// compaction), proactive.relay (delivery funnel), and background.job (worker
// cycles). When sinceMs > 0, only entries with Ts >= sinceMs are counted (e.g.
// "last 7 days"); 0 counts everything retained in the logs.
func (w *Writer) Aggregate(sinceMs int64) AggregateResult {
	w.mu.Lock()
	defer w.mu.Unlock()

	res := AggregateResult{
		ProactiveDecisions: map[string]int{},
		BackgroundJobs:     map[string]int{},
		BackgroundErrors:   map[string]int{},
	}
	toolMap := map[string]*ToolStat{}

	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "*.jsonl"))
	for _, path := range paths {
		for _, e := range readAllEntries(path) {
			if sinceMs > 0 && e.Ts < sinceMs {
				continue
			}
			switch e.Type {
			case TypeTurnTool:
				var d TurnToolData
				if json.Unmarshal(e.Data, &d) != nil {
					continue
				}
				ts := toolMap[d.Name]
				if ts == nil {
					ts = &ToolStat{Name: d.Name}
					toolMap[d.Name] = ts
				}
				ts.Calls++
				ts.TotalMs += d.DurationMs
				if d.IsError {
					ts.Errors++
				}
			case TypeRunEnd:
				var d RunEndData
				if json.Unmarshal(e.Data, &d) != nil {
					continue
				}
				res.Runs++
				res.TotalInputTokens += int64(d.InputTokens)
				res.TotalOutputTokens += int64(d.OutputTokens)
				res.CacheReadTokens += int64(d.CacheReadTokens)
				if d.Proactive {
					res.ProactiveRuns++
				}
				if d.Compacted {
					res.CompactedRuns++
				}
			case TypeProactiveRelay:
				var d ProactiveRelayData
				if json.Unmarshal(e.Data, &d) != nil {
					continue
				}
				key := d.Decision
				if d.Reason != "" {
					key = d.Decision + ":" + d.Reason
				}
				res.ProactiveDecisions[key]++
			case TypeBackgroundJob:
				var d BackgroundJobData
				if json.Unmarshal(e.Data, &d) != nil {
					continue
				}
				res.BackgroundJobs[d.Name]++
				if d.Outcome == "error" {
					res.BackgroundErrors[d.Name]++
				}
			}
		}
	}

	res.Tools = make([]ToolStat, 0, len(toolMap))
	for _, ts := range toolMap {
		if ts.Calls > 0 {
			ts.AvgMs = ts.TotalMs / int64(ts.Calls)
		}
		res.Tools = append(res.Tools, *ts)
	}
	// Sort by calls desc, then name asc for a stable order on ties.
	sort.Slice(res.Tools, func(i, j int) bool {
		if res.Tools[i].Calls != res.Tools[j].Calls {
			return res.Tools[i].Calls > res.Tools[j].Calls
		}
		return res.Tools[i].Name < res.Tools[j].Name
	})

	return res
}

// readAllEntries reads all JSONL entries from a file.
func readAllEntries(path string) []LogEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(line))
		for {
			var entry LogEntry
			if err := dec.Decode(&entry); err != nil {
				break // skip malformed tail (or EOF)
			}
			entries = append(entries, entry)
		}
	}
	return entries
}
