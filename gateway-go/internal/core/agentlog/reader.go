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

// statFor returns the ToolStat for name, creating it on first sight. Shared
// by the turn.tool and run.end folds so name handling cannot drift between
// the two aggregation sites.
func statFor(m map[string]*ToolStat, name string) *ToolStat {
	ts := m[name]
	if ts == nil {
		ts = &ToolStat{Name: name}
		m[name] = ts
	}
	return ts
}

// ToolStat aggregates one tool's usage across every recorded run.
//
// The anomaly counters close the measurement loop the tool code asks for:
// Repaired (tool_argrepair.go gates schema-aware repairs on this rate),
// Unknown (hallucinated/typoed tool names — deferred-description quality
// signal), Blocked (loop-detector/hook vetoes), CacheHits (run-cache hits —
// whether the cacheable-tool set earns its keep), Truncated (output
// truncations). TotalOutputChars/MaxOutputChars and Truncated feed per-tool
// MaxOutput cap tuning (tool_schemas.json).
type ToolStat struct {
	Name    string `json:"name"`
	Calls   int    `json:"calls"`
	Errors  int    `json:"errors"`
	TotalMs int64  `json:"totalMs"`
	AvgMs   int64  `json:"avgMs"`

	Repaired         int   `json:"repaired,omitempty"`
	UnknownArgs      int   `json:"unknownArgs,omitempty"`
	Unknown          int   `json:"unknown,omitempty"`
	Blocked          int   `json:"blocked,omitempty"`
	CacheHits        int   `json:"cacheHits,omitempty"`
	Truncated        int   `json:"truncated,omitempty"`
	TotalOutputChars int64 `json:"totalOutputChars,omitempty"`
	MaxOutputChars   int   `json:"maxOutputChars,omitempty"`
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

	accumulator := newAggregateAccumulator()
	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "*.jsonl"))
	for _, path := range paths {
		for _, entry := range readAllEntries(path) {
			if sinceMs > 0 && entry.Ts < sinceMs {
				continue
			}
			accumulator.fold(entry)
		}
	}
	return accumulator.finish()
}

// aggregateAccumulator owns the event folds separately from file traversal and
// final sorting. Each fold is best-effort: malformed payloads affect only their
// own event, matching the append-only log reader's partial-data contract.
type aggregateAccumulator struct {
	result AggregateResult
	tools  map[string]*ToolStat
}

func newAggregateAccumulator() *aggregateAccumulator {
	return &aggregateAccumulator{
		result: AggregateResult{
			ProactiveDecisions: map[string]int{},
			BackgroundJobs:     map[string]int{},
			BackgroundErrors:   map[string]int{},
		},
		tools: map[string]*ToolStat{},
	}
}

func (a *aggregateAccumulator) fold(entry LogEntry) {
	switch entry.Type {
	case TypeTurnTool:
		a.foldTurnTool(entry.Data)
	case TypeRunEnd:
		a.foldRunEnd(entry.Data)
	case TypeProactiveRelay:
		a.foldProactiveRelay(entry.Data)
	case TypeBackgroundJob:
		a.foldBackgroundJob(entry.Data)
	}
}

func (a *aggregateAccumulator) foldTurnTool(raw json.RawMessage) {
	var data TurnToolData
	if json.Unmarshal(raw, &data) != nil {
		return
	}
	stat := statFor(a.tools, data.Name)
	stat.Calls++
	stat.TotalMs += data.DurationMs
	if data.IsError {
		stat.Errors++
	}
	if data.UnknownTool {
		stat.Unknown++
	}
	if data.Blocked != "" {
		stat.Blocked++
	}
	stat.TotalOutputChars += int64(data.OutputLen)
	if data.OutputLen > stat.MaxOutputChars {
		stat.MaxOutputChars = data.OutputLen
	}
}

func (a *aggregateAccumulator) foldRunEnd(raw json.RawMessage) {
	var data RunEndData
	if json.Unmarshal(raw, &data) != nil {
		return
	}
	for name, count := range data.RepairedToolCalls {
		statFor(a.tools, name).Repaired += count
	}
	for name, count := range data.UnknownArgToolCalls {
		statFor(a.tools, name).UnknownArgs += count
	}
	for name, count := range data.CacheHitToolCalls {
		statFor(a.tools, name).CacheHits += count
	}
	for name, count := range data.TruncatedToolCalls {
		statFor(a.tools, name).Truncated += count
	}
	a.result.Runs++
	a.result.TotalInputTokens += int64(data.InputTokens)
	a.result.TotalOutputTokens += int64(data.OutputTokens)
	a.result.CacheReadTokens += int64(data.CacheReadTokens)
	if data.Proactive {
		a.result.ProactiveRuns++
	}
	if data.Compacted {
		a.result.CompactedRuns++
	}
}

func (a *aggregateAccumulator) foldProactiveRelay(raw json.RawMessage) {
	var data ProactiveRelayData
	if json.Unmarshal(raw, &data) != nil {
		return
	}
	key := data.Decision
	if data.Reason != "" {
		key = data.Decision + ":" + data.Reason
	}
	a.result.ProactiveDecisions[key]++
}

func (a *aggregateAccumulator) foldBackgroundJob(raw json.RawMessage) {
	var data BackgroundJobData
	if json.Unmarshal(raw, &data) != nil {
		return
	}
	a.result.BackgroundJobs[data.Name]++
	if data.Outcome == "error" {
		a.result.BackgroundErrors[data.Name]++
	}
}

func (a *aggregateAccumulator) finish() AggregateResult {
	a.result.Tools = make([]ToolStat, 0, len(a.tools))
	for _, stat := range a.tools {
		if stat.Calls > 0 {
			stat.AvgMs = stat.TotalMs / int64(stat.Calls)
		}
		a.result.Tools = append(a.result.Tools, *stat)
	}
	// Sort by calls desc, then name asc for a stable order on ties.
	sort.Slice(a.result.Tools, func(i, j int) bool {
		if a.result.Tools[i].Calls != a.result.Tools[j].Calls {
			return a.result.Tools[i].Calls > a.result.Tools[j].Calls
		}
		return a.result.Tools[i].Name < a.result.Tools[j].Name
	})
	return a.result
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
